# Stage 1 filtering

Stage 1 decides, without any model call, which models Decision-1 may choose from. A request
can narrow the choice in two ways:

- by asking: the optional `routing_constraints` field;
- by what it contains: tools, images, a JSON schema, streaming, or its length.

Operators can also cap the choice for every request. Code: `decision-router/decision_router/stage1.py`.

## Request fields

All four are optional and are removed before the request reaches a model, so a request
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
| `routing_mode` | `cost`, `balanced` or `quality`. Picks a price band of what is left after the other steps. |
| `compatibility` | `old` (Model Router pool) or `new` (latest models). See `MODEL_CATALOGS.md`. |
| `claude_translation` | `true` (default): the body is chat completions, and Claude is called through the translation layer. `false`: the body is an Anthropic Messages request, only Claude models are candidates, and the chosen one receives the body unchanged (only `model` is set) and its native response is returned. See below. |
| `routing_constraints.models` | Only these models: the developer's chosen subset. Names must exist in the selected catalog. |
| `routing_constraints.exclude_models` | Never these models. |
| `routing_constraints.providers` | Only models from these providers (case-insensitive): `OpenAI`, `Anthropic`, `xAI`, `DeepSeek`, `Meta`, `Fireworks`, `Microsoft`. |
| `routing_constraints.capabilities` | Models must support each one: `tools`, `parallel_tools`, `structured_output`, `json_object`, `streaming`, `reasoning`, `image_input`, `computer_use`. |
| `routing_constraints.region` | The model must be deployable in this Azure region (for example `swedencentral`; `"Sweden Central"` is accepted). Fireworks models deploy globally and pass any region. |
| `routing_constraints.deployment_type` | `global_standard`, `data_zone_standard` or `standard`. With `region`, the model must be available in that region for that deployment type. Fireworks models offer `global_standard` only. |
| `routing_constraints.inference_in_azure` | Excludes models whose inference runs outside Azure: Anthropic-hosted Claude (version 1, which includes every Claude model in `old`) and Fireworks models. |
| `routing_constraints.min_context_tokens` | The context window must be at least this large. |
| `routing_constraints.allow_preview` | Overrides `ROUTER_ALLOW_PREVIEW` for this request. |

Unknown keys, unknown model names and unknown capability names return 400
`invalid_routing_constraints`, so a typo never silently widens the choice.

## Requirements read from the request

| If the request has... | ...the model must have |
|---|---|
| `tools` | `tools: yes` |
| `parallel_tool_calls: true` (with tools) | `parallel_tools: yes` |
| `response_format: {"type": "json_schema"}` | `structured_output: yes` (Claude qualifies: the adapter sends the schema as `output_config.format`) |
| `response_format: {"type": "json_object"}` | `json_object: yes` (Claude does not: JSON mode has no Messages API equivalent) |
| `stream: true` | `streaming: yes` |
| an `image_url` part in `messages` | `image_input: yes` |
| estimated prompt tokens + `max_completion_tokens` | a big enough context window, an input limit at least as large as the prompt (GPT-5.x models accept 272K of their 400K context, or 922K of 1.05M, as input), and an output limit at least as large as requested |

The token estimate is characters ÷ 4, and deliberately rough. It exists to keep a 300,000-character prompt away from a 16K-context model, not to
cut close to a limit.

## Filter order

| Step | Removes a model when |
|---|---|
| 0. `api` | `claude_translation` is `false` and the model is not Claude (it cannot take a Messages body) |
| 1. `lifecycle` | it is deprecated, retired or legacy (always), or preview and previews are not allowed |
| 2. `allowlist` | `ROUTER_MODEL_ALLOWLIST` is set and the model is not on it |
| 3. `selection` | it is outside `models`, inside `exclude_models`, or not from a listed provider |
| 4. `capabilities` | a requested or implied capability is `no`, or `unknown` while `ROUTER_UNKNOWN_CAPABILITY=ineligible` |
| 5. `location` | inference runs outside Azure and `inference_in_azure` is set; or the region / deployment type is not available or not published |
| 6. `size` | the estimated tokens exceed the context window, the prompt exceeds the input limit, or the requested output exceeds the output limit |
| 7. `price_band` | it is outside the routing mode's band of what is left: `cost` = cheapest third, `balanced` = all, `quality` = most expensive third (at least one model always stays) |

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

## Unknown capabilities

The catalogs record `unknown` where Microsoft's documentation does not say. The default,
`ROUTER_UNKNOWN_CAPABILITY=ineligible`, treats unknown as unsupported: a request that needs
tools never goes to a model that might not support them. The cost is that some capable
models are skipped. For example, several Fireworks models have `parallel_tools: unknown`.
Setting `eligible` reverses the trade-off. The decision log shows each removal caused by an
unknown value (`"reason": "parallel_tools unknown (...)"`), so you can tell which catalog
entries are worth confirming.

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
