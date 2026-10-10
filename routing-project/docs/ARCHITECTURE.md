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
log       decision log line                   candidates, probabilities, ranking, attempts, usage, cost
   │
   ▼
generate  POST {endpoint}/openai/v1/chat/completions, model = top-ranked deployment
          on failure, next model in the ranking (stage-1 candidates only)
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
api-key: <key>            or   Authorization: Bearer <Entra token, scope cognitiveservices.azure.com>

{"model": "microsoft-decision-1",
 "state": "Request: 1 message(s); output limit: 400 tokens\nConversation, oldest first:\n\n[user]\n...",
 "questions": {"route": {
   "type": "choice",
   "instructions": "Choose the model that should answer ... Cost mode: ...",
   "criteria": {"gpt-5-nano": "Trivial, single-step work ... Price rank 1 of 6 in the pool (1 is cheapest).",
                "gpt-5-mini": "...", "deepseek-v4-flash": "..."}}}}

200 {"answers": {"route": {"type": "choice", "choice": "gpt-5-mini",
                           "probabilities": {"gpt-5-nano": 0.21, "gpt-5-mini": 0.64, "deepseek-v4-flash": 0.15},
                           "confidence": 0.81}}}
```

A response is rejected if the answer type is `refusal`, if the probabilities do not cover
exactly the offered models or do not sum to 1, or if `choice` is not an offered model. A
`choice` question needs 2–255 options; when stage 1 leaves a single model, Decision-1 is not
called.

**Source of this contract.** Microsoft's announcement and catalog pages could not be
reached from the environment this was written in. The shape above comes from the
`ElBruno.AI.Decisions` Foundry client (github.com/elbruno/ElBruno.AI.Decisions, commit
bc94ed9, 9 October 2026), which reports a live smoke test against Foundry. Check it against
the catalog entry before production use; the request is built in
`decision_router/decision1.py:build_request` and parsed in `parse_answer`.

## Failure handling

| Event | Behaviour |
|---|---|
| Decision-1 error, timeout, refusal or malformed answer | 502 (504 on timeout), `code: decision_unavailable`. No model is called. |
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
`probabilities`, `choice`, `confidence`, `latency_ms`, `skipped`), `served_model`,
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
- `report.json` and `report.md`: agreement (top-1, and Model Router's choice within
  Decision-1's top 2), how often Model Router chose a model outside the mode's stage-1 set,
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
| `ROUTER_DECISION_TIMEOUT_SECONDS` | `10` | Decision-1 call timeout |
| `ROUTER_REQUEST_TIMEOUT_SECONDS` | `60` | Per model call |
| `ROUTER_TOTAL_TIMEOUT_SECONDS` | `90` | Whole request, across fallbacks |
| `ROUTER_ATTEMPTS_PER_MODEL` | `2` | Attempts per model before the next ranked one |
| `ROUTER_MAX_RETRY_AFTER_SECONDS` | `10` | Longest `retry-after` the router waits for |
| `ROUTER_DECISION_LOG_PATH` | unset | Also write the decision log to this file |
| `ROUTER_LOG_PROMPTS` | `false` | Include message text in the decision log |
