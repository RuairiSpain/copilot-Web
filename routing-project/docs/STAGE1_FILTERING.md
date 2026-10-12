# Stage 1 filtering

Stage 1 decides, without any model call, which models Decision-1 may choose from. A request
can narrow the choice in two ways:

- by asking: the optional `routing_constraints` field;
- by what it contains: tools, images, a JSON schema, streaming, or its length.

Operators can also cap the choice for every request. Code: `decision-router/decision_router/stage1.py`.
Worked examples, with the computed output of every step, including requests that leave no
models: `STAGE1_EXAMPLES.md`.

## Request fields

These fields are all optional and are removed before the request reaches a model, so a request
without them is a plain Model Router request.

```json
{
  "messages": [{"role": "user", "content": "Summarise this contract and flag risky clauses."}],
  "routing_mode": "balanced",
  "compatibility": "new",
  "routing_constraints": {
    "models": ["gpt-5.5", "claude-sonnet-5-5", "DeepSeek-V4-Pro", "gpt-5.4-mini"],
    "exclude_models": [],
    "providers": ["OpenAI", "Anthropic", "DeepSeek"],
    "capabilities": ["tools", "reasoning"],
    "region": "swedencentral",
    "deployment_type": "data_zone_standard",
    "inference_in_azure": true,
    "min_context_tokens": 200000,
    "allow_preview": false
  }
}
```

| Field | Effect |
|---|---|
| `routing_mode` | `cost`, `balanced` or `quality`. Picks a price band of what is left after the other steps, and the instructions Decision-1 receives. |
| `routing_cost_band` | Replaces the mode's price band. A label in the `new` catalog (`low`, `medium`, `medium-high`, `high`, `top`) or a dollar range on either catalog: `"1-3"`, `"$10-$25"`, `"30-"` (no upper limit) or `{"min": 1, "max": 3}`. With `routing_constraints.actions`, a label means an even price group of that action's models instead of a dollar range. See [Cost bands](#cost-bands). |
| `compatibility` | `old` (Model Router pool) or `new` (latest models). See `MODEL_CATALOGS.md`. |
| `claude_translation` | `true` (default): the body is chat completions, and Claude is called through the translation layer. `false`: the body is an Anthropic Messages request, only Claude models are candidates, and the chosen one receives the body unchanged (only `model` is set) and its native response is returned. See below. |
| `routing_pin` | `true`: keep the conversation on the model that answered its first turn; stage 1 still runs on every turn. See `AGENT_SERVICE.md`. |
| `routing_constraints.models` | Only these models: the developer's chosen subset. Names must exist in the selected catalog. |
| `routing_constraints.exclude_models` | Never these models. |
| `routing_constraints.providers` | Only models from these providers (case-insensitive): `OpenAI`, `Anthropic`, `xAI`, `DeepSeek`, `Meta`, `Fireworks`, `Microsoft`. |
| `routing_constraints.capabilities` | Models must support each one: `tools`, `structured_output`, `streaming`, `reasoning`, `image_input`. |
| `routing_constraints.actions` | Models must be tagged with every listed action: `code`, `evaluate`, `extract`, `orchestrate`, `plan`, `reason`, `summarize`, `translate`, `write`. `new` catalog only. Changes what a `routing_cost_band` label means (see [Cost bands](#cost-bands)). |
| `routing_constraints.region` | The model must be deployable in this Azure region (for example `swedencentral`; `"Sweden Central"` is accepted). Fireworks models deploy globally and pass any region. |
| `routing_constraints.deployment_type` | `global_standard`, `data_zone_standard` or `standard`. With `region`, the model must be available in that region for that deployment type. Fireworks models offer `global_standard` only. |
| `routing_constraints.inference_in_azure` | Excludes models whose inference runs outside Azure: Anthropic-hosted Claude (version 1, which includes every Claude model in `old`) and Fireworks models. |
| `routing_constraints.min_context_tokens` | The context window must be at least this large. |
| `routing_constraints.allow_preview` | Overrides `ROUTER_ALLOW_PREVIEW` for this request. |

Unknown keys, unknown model names and unknown capability names return 400
`invalid_routing_constraints`, so a typo never silently widens the choice.

## Cost bands

`routing_cost_band` selects models by USD output price per million tokens, from
`config/model_pricing.json`, instead of by thirds. Both ends of a range are inclusive, so a model priced exactly on an
edge ($1, $3, $10 or $25) is in both bands. The `new` catalog defines five named bands (`cost_bands` in
`config/catalog_spec.json`), which together cover $0–$60 with no gaps:

| Band | Output price | Models in `new` |
|---|---|---|
| `low` | $0–$1 | Phi-4-mini-instruct, FW-GLM-5.3-Flash, claude-haiku-5-5, gpt-5.4-nano, Phi-4, gpt-oss-120b, Phi-4-reasoning, Llama-4-Maverick, grok-4-1-fast-reasoning, DeepSeek-V4-Flash |
| `medium` | $1–$3 | gpt-5.4-mini, gpt-6-luna, FW-GLM-5.3, FW-Kimi-K3 |
| `medium-high` | $3–$10 | gpt-5.4-mini, DeepSeek-V4-Pro, gpt-5.6-terra, MAI-Thinking-1, claude-sonnet-5-5, gpt-6.1-sol |
| `high` | $10–$25 | claude-sonnet-5-5, gpt-6.1-sol, gpt-5.5, grok-4.7, claude-opus-5-5 |
| `top` | $25–$60 | gpt-6-astra |

Every model is in at least one band. gpt-5.4-mini ($3) is in `medium` and `medium-high`;
claude-sonnet-5-5 and gpt-6.1-sol ($10) are in `medium-high` and `high`.

- The band is applied after the other steps, like the routing mode's band. Unlike a mode, a
  band can leave no models, for example `top` with `providers: ["Anthropic"]`, which returns 422.
- `routing_mode` still applies: it sets the instructions Decision-1 receives inside the band.
- The `old` catalog has no named bands and keeps the routing modes' thirds. A band name sent
  with `old` returns 400 `invalid_routing_cost_band`; a dollar range works on both catalogs.
- Callers that can't add the field use the profiles `decision-router-low`, `-medium`,
  `-medium-high`, `-high` and `-top` (`AGENT_SERVICE.md`).
- `/v1/route` and the decision log report the band as `cost_band`.

### Labels with an action: even price groups

When the request also has `routing_constraints.actions`, the labels stop being dollar ranges. Stage 1
takes the models left after every earlier step (so already filtered to the action, tools, region and
so on), sorts them by output price, and splits them into groups:

1. As many groups as possible, up to four, with at least two models in each.
2. Models with the same output price are always in the same group.
3. Of the splits that satisfy both, the one with the most even group sizes.

With four groups, `low`, `medium`, `high` and `top` are the cheapest to the most expensive group.
With fewer models, neighbouring labels share a group: with three groups `medium` and `high` share
the middle one, with two `low` = `medium` and `high` = `top`, and with one (two or three models)
every label returns all of them. `medium-high` returns the `medium` and `high` groups together
(the middle of the action's price range), or the shared group when they are the same. So a label with an action never leaves fewer than two models when
two or more remain, and never leaves none.

The groups for each action in `new`, with no other filters (output price per million tokens):

| Action | low | medium | high | top |
|---|---|---|---|---|
| code | claude-haiku-5-5, gpt-oss-120b, DeepSeek-V4-Flash ($0.50–$0.80) | FW-GLM-5.3, FW-Kimi-K3, gpt-5.4-mini, DeepSeek-V4-Pro ($2.50–$4) | MAI-Thinking-1, gpt-5.6-terra, claude-sonnet-5-5 ($7.50–$10) | gpt-5.5, grok-4.7, claude-opus-5-5, gpt-6-astra ($15–$50) |
| evaluate | gpt-oss-120b, DeepSeek-V4-Pro, gpt-5.6-terra ($0.60–$8) | claude-sonnet-5-5, gpt-6.1-sol ($10) | = medium | gpt-5.5, claude-opus-5-5 ($15–$20) |
| extract | Phi-4-mini-instruct, claude-haiku-5-5 ($0.30–$0.50) | = low | gpt-5.4-nano, gpt-5.4-mini ($0.80–$3) | = high |
| orchestrate | FW-GLM-5.3-Flash, claude-haiku-5-5, grok-4-1-fast-reasoning ($0.40–$0.50) | FW-GLM-5.3, FW-Kimi-K3 ($2.50) | claude-sonnet-5-5, gpt-6.1-sol ($10) | gpt-5.5, grok-4.7, gpt-6-astra ($15–$50) |
| plan | FW-GLM-5.3, DeepSeek-V4-Pro ($2.50–$4) | MAI-Thinking-1, gpt-5.6-terra ($7.50–$8) | gpt-6.1-sol, gpt-5.5 ($10–$15) | claude-opus-5-5, gpt-6-astra ($20–$50) |
| reason | claude-haiku-5-5, Phi-4, grok-4-1-fast-reasoning, gpt-oss-120b, Phi-4-reasoning ($0.50–$0.60) | DeepSeek-V4-Flash, gpt-6-luna, FW-GLM-5.3, FW-Kimi-K3 ($0.80–$2.50) | gpt-5.4-mini, DeepSeek-V4-Pro, MAI-Thinking-1, gpt-5.6-terra ($3–$8) | claude-sonnet-5-5, gpt-6.1-sol, claude-opus-5-5, gpt-6-astra ($10–$50) |
| summarize | Phi-4-mini-instruct, FW-GLM-5.3-Flash ($0.30–$0.40) | = low | gpt-5.4-nano, DeepSeek-V4-Flash, gpt-5.4-mini ($0.80–$3) | = high |
| translate | Llama-4-Maverick, gpt-6-luna ($0.85–$2.50) | = low | = low | = low |
| write | Phi-4-mini-instruct, FW-GLM-5.3-Flash ($0.30–$0.40) | Phi-4, gpt-5.4-nano ($0.50–$0.80) | Llama-4-Maverick, gpt-6-luna ($0.85–$2.50) | grok-4.7, claude-opus-5-5 ($15–$20) |

Because the groups are formed after the other steps, the same label can return different models
for different requests: with `tools` in the request, gpt-5.6-terra leaves the code models and the
13 that remain are split again. A dollar range with `actions` is still a dollar range, and a label
without `actions` is still the dollar band above. Several actions mean a model must have all of them.

## Requirements read from the request

| If the request has... | ...the model must have |
|---|---|
| `tools` | `tools: yes` |
| `response_format: {"type": "json_schema"}` | `structured_output: yes` (Claude qualifies: the adapter sends the schema as `output_config.format`) |
| `response_format: {"type": "json_object"}` | not a Claude model: the Messages API has no JSON mode, so Claude would answer in free text. Other models are not filtered |
| `stream: true` | `streaming: yes` |
| an `image_url` part in `messages` | `image_input: yes` |
| estimated prompt tokens + `max_completion_tokens` | a big enough context window, an input limit at least as large as the prompt (GPT-5.x models accept 272K of their 400K context, or 922K of 1.05M, as input), and an output limit at least as large as requested |

The token estimate is characters ÷ 4 for text, plus a fixed 1,600 tokens per image. An inline
base64 image is not counted by its encoded size: a 750 KB image would otherwise count as about
190,000 tokens and wrongly remove models for size. The estimate is deliberately rough. It exists
to keep a 300,000-character prompt away from a 16K-context model, not to cut close to a limit.
It is computed once per request and reused for the instructions Decision-1 receives.

## Filter order

| Step | Removes a model when |
|---|---|
| 0. `api` | `claude_translation` is `false` and the model is not Claude (it cannot take a Messages body) |
| 1. `lifecycle` | it is deprecated, retired or legacy (always), or preview and previews are not allowed |
| 2. `allowlist` | `ROUTER_MODEL_ALLOWLIST` is set and the model is not on it |
| 3. `selection` | it is outside `models`, inside `exclude_models`, or not from a listed provider |
| 4. `capabilities` | a requested or implied capability is `no`, or the model is not tagged with every action in `actions` |
| 5. `location` | inference runs outside Azure and `inference_in_azure` is set; or the region / deployment type is not available or not published |
| 6. `size` | the estimated tokens exceed the context window, the prompt exceeds the input limit, or the requested output exceeds the output limit |
| 7. `price_band` | with a `routing_cost_band` label and `actions`: it is outside that label's price group of what is left. With a label alone or a dollar range: its output price is outside the range (this can leave no models). Otherwise: it is outside the routing mode's band of what is left: `cost` = cheapest third, `balanced` = all, `quality` = most expensive third (at least one model always stays) |

What happens with the result:

- **Two or more models left:** they go to Decision-1.
- **One model left:** it is used without calling Decision-1.
- **No models left:** the request gets **422 `no_eligible_models`**, naming the step that removed the
  last model. The full step-by-step record is included in the error. No model is called.

```json
{"error": {"code": "no_eligible_models",
           "message": "no model in the 'new' catalog passes stage 1; the 'capabilities' step removed the last candidates",
           "stage1": {"empty_at": "capabilities", "requirements": {"tools": "request has tools"},
                      "steps": [{"step": "lifecycle", "removed": [], "remaining": 23},
                                {"step": "allowlist", "removed": [], "remaining": 23},
                                {"step": "selection", "removed": [...], "remaining": 1},
                                {"step": "capabilities", "removed": [{"model": "Phi-4", "reason": "tools no (needed: request has tools)"}],
                                 "remaining": 0}]}}}
```

The same record (`stage1`) is written to the decision log for every request, and `/v1/route`
returns it, so you can see why a model was or wasn't offered.

## `claude_translation: false` (native Claude passthrough)

For callers that already speak the Anthropic Messages API:

```json
{
  "claude_translation": false,
  "routing_mode": "quality",
  "model": "ignored",
  "max_tokens": 1024,
  "system": "You are a careful reviewer.",
  "messages": [{"role": "user", "content": [{"type": "text", "text": "Review this clause..."}]}]
}
```

- **Stage 1:** the `api` step keeps only Claude models. The other filters still apply.
  Requirements are read from the Messages body: `tools`, `stream: true`, and `image` content blocks.
- **Decision-1:** it sees the conversation, including the top-level `system` prompt, and chooses among the Claude candidates.
- **The model call:** the chosen Claude deployment receives the body exactly as sent, with
  `model` set to its deployment and this router's extension fields removed.
- **The response:** it comes back unchanged, in Messages format, and streams as native Anthropic
  events (`event: message_start`, ...).
- **Errors:** they are also returned in Anthropic's shape, not translated.
- **Logging:** the decision log normalises Claude's `input_tokens`/`output_tokens` into
  `prompt_tokens`/`completion_tokens`, so cost and metrics work either way.

If the constraints leave no Claude model, the request gets the usual 422 naming the step.

## Capability values

Each capability is `yes` or `no`. `no` covers both "a source says it is unsupported" and "no
source confirms it", so a request that needs a capability only reaches models a source
confirms. The cost is that a model which in fact supports something, but has no source yet, is
skipped. For example, seven models have `streaming: no` for lack of a source.

Not filtered on: `parallel_tool_calls` and `response_format: {"type": "json_object"}`. Requests
with them can reach any model. A model without JSON mode may answer in free text; Claude always
does, because the translation layer drops `json_object`. Send a JSON schema when the answer
must be JSON.

Leaving out a filter keeps its models in play. Phi models have no tool calling, so they drop
out of any request with `tools`, but remain candidates for plain text requests.

## Interaction with Model Router comparison

Model Router accepts no per-request constraints; it only knows its deployment's model subset
and mode. The comparison harness therefore sends no `routing_constraints` and uses
`compatibility=old`. Constraints and the `new` catalog are features of this router alone.

## Examples

Keep inference in the EU, any capable model:

```json
{"routing_constraints": {"region": "swedencentral", "deployment_type": "data_zone_standard", "inference_in_azure": true}}
```

Only two approved models, cheapest that will do:

```json
{"routing_mode": "cost", "routing_constraints": {"models": ["gpt-5.4-mini", "gpt-5.5"]}}
```

Agent step with tools on the newest models, previews excluded:

```json
{"compatibility": "new", "tools": [...], "routing_constraints": {"allow_preview": false}}
```
