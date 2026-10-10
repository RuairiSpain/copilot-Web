# Architecture

```
client ── POST /v1/chat/completions (Model Router body + optional routing_mode)
   │
   ▼
stage 1   catalog.candidates(mode)            deterministic, from config/model_catalog.json
   │      cost → nano, mini, deepseek  ·  balanced → all six  ·  quality → gpt-5.5, o4-mini, terra
   ▼
stage 2   Decision-1 "choice" question        state = rendered conversation
   │      criteria = candidates + descriptions + price rank
   │      ranking = probabilities, highest first (ties → cheaper model)
   ▼
select    execution order                     highest probability first; if that probability is
   │                                          below ROUTER_LOW_CONFIDENCE_THRESHOLD, the second-
   │                                          ranked model goes first and the top model second
   ▼
log       decision log line                   candidates, probabilities, ranking, execution order, usage, cost
   │
   ▼
generate  POST {endpoint}/openai/v1/chat/completions, model = first model in the execution order
          on failure, the next model in that order (stage-1 candidates only)
```

## Input and output

**Input** is an OpenAI chat-completions body, the same one Model Router accepts. `model` is
ignored (it names the router) and replaced with the chosen deployment. Every other field
(`messages`, `tools`, `response_format`, `temperature`, `max_completion_tokens`, `stream`,
and so on) is forwarded unchanged.

**Routing mode.** Model Router sets its mode on the deployment. This service does the same
with `default_mode` in the catalog or `ROUTER_DEFAULT_MODE`, and also accepts a
`routing_mode` field per request, which is removed before the request is forwarded.

**Output** is the chosen model's response, unchanged, including streamed responses. Its
`model` field names the model that answered.

## Decision-1 contract

```http
POST https://<resource>.services.ai.azure.com/providers/microsoft/v1/systemone
Content-Type: application/json
Accept: application/json
api-key: <key>            or   Authorization: Bearer <Entra token, scope cognitiveservices.azure.com>

{"model": "microsoft-decision-1",
 "state": "Request: 1 message(s); output limit: 400 tokens\nConversation, oldest first:\n\n[user]\n...",
 "questions": {"route": {
   "type": "choice",
   "instructions": "Choose the model that should answer ... Cost mode: ...",
   "criteria": {"gpt-5-nano": "Trivial, single-step work ... Price rank 1 of 6 in the pool (1 is cheapest).",
                "gpt-5-mini": "...", "deepseek-v4-flash": "..."}}}}

200 {"model": "microsoft-decision-1", "usage": {"prompt_tokens": 412, ...},
     "answers": {"route": {"type": "choice", "choice": "gpt-5-mini",
                           "probabilities": {"gpt-5-nano": 0.21, "gpt-5-mini": 0.64, "deepseek-v4-flash": 0.15},
                           "confidence": 0.81}}}
```

`model` in the request is the **deployment name**; `model` in the response is the underlying
model (for example `microsoft-decision-1`). Both are logged, with the response's token usage
and the Decision-1 cost (list price $0.042 per million input tokens; output is not billed).

How the answer is used:

- The ranking is the candidates in descending probability; ties go to the cheaper model. It
  does not depend on the order the options were sent in. Decision-1's `choice` is logged; with
  calibrated probabilities it is the top-ranked model.
- Probabilities that sum to 1 within 0.01 are renormalised. A larger gap, a missing or extra
  option, a `choice` that was not offered, or a `refusal` answer rejects the response.
- A `choice` question needs at least two options. When stage 1 leaves a single model,
  Decision-1 is not called.
- `confidence` is optional and only logged.

**Sources.** Route, request body, authentication, deployment-name semantics, error codes and
pricing are from Microsoft's documentation ("Deploy and use Microsoft-Decision-1 in Microsoft
Foundry", and the Foundry blog post of 9 October 2026). Microsoft publishes no sample response:
the docs say only that a choice answer contains "the selected option and a probability for
every option", plus the model name and token usage. The field names `probabilities` and
`confidence` come from the `ElBruno.AI.Decisions` Foundry client (commit bc94ed9), which was
smoke-tested against Foundry. Confirm them with one live call; they are read in
`decision_router/decision1.py:parse_answer`.

## Selecting the model: the low-confidence rule

The model called first is the one with the highest probability. When that probability is
below `ROUTER_LOW_CONFIDENCE_THRESHOLD` (default `0.5`), the second-ranked model is called
first instead, and the top model moves to second place, so it is still tried if the second
model fails. The rest of the order is unchanged.

| Probabilities | Threshold | Execution order | `low_confidence` |
|---|---|---|---|
| gpt-5.5 0.70, o4-mini 0.20, terra 0.10 | 0.5 | gpt-5.5, o4-mini, terra | false |
| gpt-5.5 0.45, o4-mini 0.35, terra 0.20 | 0.5 | o4-mini, gpt-5.5, terra | true |
| gpt-5.5 0.45, o4-mini 0.35, terra 0.20 | 0 (off) | gpt-5.5, o4-mini, terra | false |

Notes:

- The rule uses the top model's probability from `probabilities`, not the optional
  `confidence` field.
- It does not apply when stage 1 leaves a single model.
- `0.5` is a starting value, not a validated one. Microsoft's guidance is to choose thresholds
  per task on your own data. The comparison report includes a threshold sweep (below) for
  that purpose.
- With six candidates a top probability of 0.45 can still be a clear lead; with two it is
  nearly a coin toss. If one threshold does not suit every mode, that is visible in the
  report's per-mode figures.

The response carries `x-router-low-confidence: true|false`, and `/v1/route` returns both
`ranking` (by probability) and `execution_order`. The decision log records `ranking`,
`execution_order`, `top_probability`, `low_confidence` and the threshold in force.

## Failure handling

| Event | Behaviour |
|---|---|
| Decision-1 returns 408, 429, 5xx or a network error | Retried with exponential backoff, honouring `retry-after-ms` / `retry-after`, up to `ROUTER_DECISION_MAX_ATTEMPTS` attempts inside `ROUTER_DECISION_TIMEOUT_SECONDS` |
| Decision-1 still failing, or 400/401/403/404/422, refusal or malformed answer | `code: decision_unavailable`, status 503 for throttling, 504 for timeout, otherwise 502. No model is called. |
| Model returns 408, 429, 5xx or a network error | Retry the same model (`ROUTER_ATTEMPTS_PER_MODEL`, default 2), honouring `retry-after-ms` / `retry-after` up to `ROUTER_MAX_RETRY_AFTER_SECONDS`; then the next ranked model |
| Model returns 404 (deployment missing) | Next ranked model, no retry |
| Model returns another 4xx (bad request, content filter, auth) | Passed through to the client unchanged; no fallback, because another model would get the same request |
| All ranked models fail | 502, `code: all_models_failed`, with the attempt list |
| `ROUTER_TOTAL_TIMEOUT_SECONDS` used up | 504, `code: deadline_exceeded` |
| Streaming | Fallback is possible only before the first byte |

## Decision log

One JSON line per request, to the `decision_router.decisions` logger and, when
`ROUTER_DECISION_LOG_PATH` is set, to that file (written off the event loop):
`request_id`, `routing_mode`, `catalog_sha256`, `candidates`, `decision` (`ranking`,
`execution_order`, `top_probability`, `low_confidence`, `low_confidence_threshold`,
`probabilities`, `choice`, `confidence`, `latency_ms`, `skipped`, `attempts`,
`response_model`, `usage`, `cost`), `served_model`,
`provider_model`, `fallback_used`, `attempts`, `usage`, `cost`, `total_latency_ms`,
`outcome`. Message text is not logged unless `ROUTER_LOG_PROMPTS=true`; a SHA-256 of the
messages is logged instead.

## Comparing with Model Router

Model Router's mode is a deployment setting, so the comparison needs one Model Router
deployment per mode. Each must use **the same model subset** as `config/model_catalog.json`,
and the catalog's `deployment` names must be the deployments Model Router uses. Then both
arms choose from the same models and generate with the same deployments, and the only
difference is the routing decision.

```bash
export FOUNDRY_ENDPOINT=https://<resource>.services.ai.azure.com
export MODEL_ROUTER_DEPLOYMENTS='{"cost":"model-router-cost","balanced":"model-router","quality":"model-router-quality"}'
python scripts/compare_with_model_router.py --sample 400 --output-dir eval/run-001
```

Dataset `quality_preference` maps to modes: `cheap` → `cost`, `medium` and `high` →
`balanced`, `extra_high` → `quality`. The sample is stratified by task type × preference.

Outputs in `--output-dir`:

- `results.jsonl`: per prompt, the Decision-1 candidates, probabilities and ranking, the
  model each arm served, latency, usage, cost and the response text.
- `decision_log.jsonl`: the service's own decision log for the run.
- `report.json` and `report.md`: agreement (our first call, and Model Router's choice within
  Decision-1's top 2), a low-confidence section (how often the rule fired, and what fire
  rate and agreement thresholds from 0.3 to 0.8 would have given), how often Model Router chose a model outside the mode's stage-1 set,
  model mix per mode, latency percentiles, cost totals, and agreement with the dataset's
  policy labels.

What the report does and does not show:

- Agreement measures whether the two routers pick the same model, not which answer is
  better. To judge quality, score the stored outputs, for example with Microsoft's
  Model-Router-Auto-Evaluation harness or a pairwise judge that runs each pair in both orders.
- The dataset's `selected_model` labels come from a hand-written policy, so agreement with
  them is a reference point only.
- If Model Router returns models that are not in the catalog, the report lists them and
  leaves those rows out. Add them as `aliases`, or align the router's model subset.
- `--shuffle-options [N]` re-asks Decision-1 with the options reordered and reports how often
  the decision changes. See `docs/OPTION_ORDER_TESTING.md`.
- `--decide-only` stops our arm after Decision-1. Model Router has no route-only call, so it
  still generates.
- `--dry-run` uses an in-memory fake for both arms. It checks the harness, not either router.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `FOUNDRY_ENDPOINT` | (required) | `https://<resource>.services.ai.azure.com` |
| `FOUNDRY_API_KEY` | unset | API key; when unset, Entra ID via `DefaultAzureCredential` |
| `DECISION1_DEPLOYMENT` | `microsoft-decision-1` | Decision-1 deployment name |
| `DECISION1_ENDPOINT` / `DECISION1_API_KEY` | derived / `FOUNDRY_API_KEY` | Override when Decision-1 is on another resource |
| `FOUNDRY_CHAT_COMPLETIONS_URL` | `{endpoint}/openai/v1/chat/completions` | Override the chat route |
| `ROUTER_DEFAULT_MODE` | catalog `default_mode` | `cost`, `balanced` or `quality` |
| `ROUTER_DEPLOYMENT_MAP` | `{}` | JSON, model name → deployment name |
| `ROUTER_CATALOG_PATH` / `ROUTER_PRICING_PATH` | `config/...` | Catalog and price table |
| `ROUTER_STATE_MAX_CHARS` | `24000` | Size of the conversation text sent to Decision-1 |
| `ROUTER_DECISION_TIMEOUT_SECONDS` | `10` | Time budget for the Decision-1 call, retries included |
| `ROUTER_DECISION_MAX_ATTEMPTS` | `3` | Decision-1 attempts on 408, 429, 5xx and network errors |
| `ROUTER_LOW_CONFIDENCE_THRESHOLD` | `0.5` | Below this top probability, call the second-ranked model first; `0` turns the rule off |
| `ROUTER_REQUEST_TIMEOUT_SECONDS` | `60` | Per model call |
| `ROUTER_TOTAL_TIMEOUT_SECONDS` | `90` | Whole request, across fallbacks |
| `ROUTER_ATTEMPTS_PER_MODEL` | `2` | Attempts per model before the next ranked one |
| `ROUTER_MAX_RETRY_AFTER_SECONDS` | `10` | Longest `retry-after` the router waits for |
| `ROUTER_DECISION_LOG_PATH` | unset | Also write the decision log to this file |
| `ROUTER_LOG_PROMPTS` | `false` | Include message text in the decision log |
