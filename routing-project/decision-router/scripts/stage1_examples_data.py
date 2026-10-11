"""Example requests for docs/STAGE1_EXAMPLES.md (rendered by stage1_examples.py).

Each example: title, why (what it demonstrates), request (the body a client sends),
optional settings (router configuration overrides), explanation (what the stage 1 output
means). Outputs are computed, never written here.
"""

USER = "Summarise the attached contract clause and flag anything unusual."
TOOL = {"type": "function", "function": {"name": "read_file", "description": "Read a file from the repository",
                                         "parameters": {"type": "object", "properties": {"path": {"type": "string"}}}}}
IMAGE = {"type": "image_url", "image_url": {"url": "https://example.com/diagram.png"}}
SCHEMA = {"type": "json_schema", "json_schema": {"name": "risk", "strict": True, "schema": {
    "type": "object", "properties": {"risk": {"type": "string"}}, "required": ["risk"], "additionalProperties": False}}}


def msgs(text: str = USER, system: str | None = None) -> list:
    return ([{"role": "system", "content": system}] if system else []) + [{"role": "user", "content": text}]


INTRO = """
Stage 1 is the deterministic filter that decides which models Decision-1 may choose from
(rules: `STAGE1_FILTERING.md`). This page runs a set of example requests through it and shows,
for each one, what was removed at each step and the list that goes to Decision-1.

How to read the examples:

- **Computed, not hand-written.** Every result was computed by running the request through the
  router's request handling and stage 1 against the checked-in catalogs
  (`decision-router/scripts/stage1_examples.py`). A test fails if a catalog or rule change
  makes this page out of date.
- **Default settings** unless an example says otherwise:
  - catalog `old` (`ROUTER_COMPATIBILITY`);
  - `ROUTER_UNKNOWN_CAPABILITY=ineligible`, so a capability recorded as `unknown` counts as missing;
  - previews allowed;
  - no operator allow-list.
- **Token estimate:** characters ÷ 4.
- **What Decision-1 receives:** each remaining model becomes one option in the Decision-1
  request. The option key is the model name, and its text is the model's catalog description
  followed by its price rank. For example, the option for `gpt-5.4-mini` in the `new` catalog reads:
  "Compact GPT-5.4 reasoning model and a good default for everyday work: ... Price rank 11 of
  23 in the pool (1 is cheapest)." Models are listed cheapest first.
- **One model left:** it is called directly and Decision-1 is skipped.
- **No models left:** the request fails with 422 `no_eligible_models` (examples Z1 onwards).
"""

FINDINGS = """
Running the examples exposed these catalog gaps. Each one makes stage 1 remove models that
can probably serve the request:

1. **JSON mode:** every OpenAI model has `json_object: unknown`, because the capability
   tables don't mention JSON mode. A `response_format: json_object` request therefore reaches
   only DeepSeek V4 and grok-4.7 (example A8).
2. **Streaming:** ten `new` models have `streaming: unknown`, so every `stream: true` request
   loses them (A9): Phi, Llama 4 Maverick, grok, DeepSeek V4, FW-Kimi-K3 and MAI-Thinking-1.
3. **Parallel tool calls:** FW-GLM-5.3 and DeepSeek-V4-Pro have `parallel_tools: unknown`. The
   coding profile lists them, but they are never offered when an agent sends
   `parallel_tool_calls: true` (A15).
4. **Standard (regional) deployments:** no model has published Standard availability, so
   `deployment_type: standard` always returns no models (Z3).

Confirming these values, or setting `ROUTER_UNKNOWN_CAPABILITY=eligible` (A10), would widen
the choice. Each confirmed value goes in `catalog_spec.json` with its source.
"""

ZERO_INTRO = """
These requests leave no candidates. The router returns **422 `no_eligible_models`**, names the
step that removed the last models, and includes the full stage-1 record. No model is called.
The fallback for these cases is still to be decided; the examples cover the distinct ways a
request can end up here:

- **conflicting constraints:** the request contradicts itself, or contradicts the operator's
  allow-list;
- **requirements no chosen model meets:** tools, images, JSON, reasoning or streaming;
- **location rules:** region, deployment type, inference in Azure;
- **size limits:** context window, input or output limit.
"""

EXAMPLES: list[dict] = [
    # ------------------------------------------------------------------ reach Decision-1
    {"title": 'Plain question, default settings',
     "why": "The baseline. It shows what Decision-1 receives when a request asks for nothing special: the router's default catalog (`old`, Model Router's pool) in the default `balanced` mode. This is the case the Model Router comparison runs.",
     "explanation": "Nothing in the request narrows the choice and balanced mode keeps the whole price range, so all 26 models go to Decision-1. Decision-1 then weighs answer quality against cost using each model's description and price rank.",
     "request": {"messages": msgs()}},
    {"title": 'Cost mode',
     "why": 'Shows what the routing mode does on its own. Cost mode is the main setting a caller has for limiting cost.',
     "explanation": 'Cost mode keeps the cheapest third of whatever passes the earlier steps (9 of 26, by `tier`). Decision-1 is also told to prefer the cheapest model that can still answer well. A more expensive model can never be chosen, even if Decision-1 would rate it higher.',
     "request": {"messages": msgs(), "routing_mode": "cost"}},
    {"title": 'Quality mode',
     "why": 'The opposite of cost mode: the caller wants the best answer and accepts the cost.',
     "explanation": 'Quality mode keeps the most expensive third (9 of 26). Price is used as a stand-in for capability here, so the band includes grok-4, whose 8,192-token output limit makes it unsuitable for long answers. Decision-1, which reads the description, is expected to rank it low for those.',
     "request": {"messages": msgs(), "routing_mode": "quality"}},
    {"title": 'New catalog, balanced',
     "why": "The `new` catalog is the production pool of latest models. This shows its full list, including models Model Router doesn't offer (Phi, MAI, DeepSeek V4, gpt-6.1-sol).",
     "explanation": 'No filter applies, so all 23 models of the `new` catalog are candidates.',
     "request": {"messages": msgs(), "compatibility": "new"}},
    {"title": 'Tool calling (agent step)',
     "why": 'Agent steps carry a `tools` array. This is the most common requirement read from the request, and the one Foundry Agent Service requests always have.',
     "explanation": "Stage 1 removes the five models that can't call tools through Chat Completions: the three Phi models, Llama 4 Maverick, and gpt-6.1-sol, whose tool calling works only through the Responses API. Claude stays, because the translation layer converts tool calls. 18 models remain.",
     "request": {"messages": msgs("Find where the retry limit is set."), "compatibility": "new", "tools": [TOOL]}},
    {"title": 'Image input',
     "why": 'Requests with an `image_url` part need a vision-capable model; sending them to a text-only model would fail or ignore the image.',
     "explanation": 'Eight text-only models are removed (Phi, gpt-oss-120b, DeepSeek V4, FW-GLM-5.3, MAI-Thinking-1). 15 models that accept images remain.',
     "request": {"compatibility": "new", "messages": [{"role": "user", "content": [
         {"type": "text", "text": "What does this architecture diagram show?"}, IMAGE]}]}},
    {"title": 'JSON-schema output',
     "why": 'Callers that parse the answer programmatically send `response_format: json_schema`. Only models that enforce the schema should be offered.',
     "explanation": 'Models with `structured_output: yes` remain, including the three Claude models (the adapter sends the schema as `output_config.format`). The 11 removed models have `structured_output: unknown`: the catalog has no source saying they enforce a schema, and `ROUTER_UNKNOWN_CAPABILITY=ineligible` treats that as unsupported.',
     "request": {"messages": msgs(), "compatibility": "new", "response_format": SCHEMA}},
    {"title": 'JSON mode (json_object)',
     "why": 'JSON mode asks for any valid JSON without a schema. It shows how strongly an `unknown` capability value narrows the choice.',
     "explanation": 'Only three models remain: the two DeepSeek V4 models and grok-4.7, whose docs list JSON as a response format. Claude is removed because JSON mode has no Messages API equivalent (`no`). All OpenAI models are removed because the catalog records `json_object: unknown` for them; see the findings above.',
     "request": {"messages": msgs(), "compatibility": "new", "response_format": {"type": "json_object"}}},
    {"title": 'Streaming',
     "why": '`stream: true` is common in chat front ends. This shows that streaming support is a filter, not a formatting detail.',
     "explanation": 'Ten models are removed because their streaming support is `unknown` in the catalog: Phi, Llama 4 Maverick, grok, DeepSeek V4, FW-Kimi-K3 and MAI-Thinking-1. 13 models remain. Most of the removed models probably do stream; see the findings above.',
     "request": {"messages": msgs(), "compatibility": "new", "stream": True}},
    {"title": 'Streaming with unknown capabilities allowed',
     "why": 'The same request with `ROUTER_UNKNOWN_CAPABILITY=eligible`. It shows the trade-off that setting controls.',
     "explanation": "With unknown values treated as supported, nothing is removed and all 23 models are candidates. The risk is that a model which in fact can't stream is chosen and the call fails; the router then falls back to the next ranked model.",
     "settings": {"unknown_capability": "eligible"},
     "request": {"messages": msgs(), "compatibility": "new", "stream": True}},
    {"title": 'Long document (~300K tokens)',
     "why": "Long prompts must not go to models whose context window or input limit is smaller. The size step is the only one that depends on the prompt's length.",
     "explanation": 'The prompt is estimated at about 300,000 tokens (characters ÷ 4). Models with smaller context windows are removed, and so are gpt-5.4-nano and gpt-5.4-mini: their context is 400K, but they accept at most 272K tokens of input. 15 models with 500K to 1M-token contexts remain.',
     "request": {"messages": msgs("Summarise this report: " + "x" * 1_200_000), "compatibility": "new"}},
    {"title": 'EU data residency',
     "why": 'A typical compliance request: process in the EU (Sweden Central, Data Zone Standard) and only on Azure infrastructure.',
     "explanation": "Fireworks models are removed (inference outside Azure), models without a Data Zone deployment are removed, gpt-oss-120b is removed because its regions aren't published, and models not offered in Sweden Central's Data Zone are removed. Six OpenAI and DeepSeek models remain.",
     "request": {"messages": msgs(), "compatibility": "new", "routing_constraints": {
         "region": "swedencentral", "deployment_type": "data_zone_standard", "inference_in_azure": True}}},
    {"title": 'Developer-chosen subset, cost mode',
     "why": 'A developer who has approved five models and wants the cheapest suitable one. It shows that the price band applies to the subset, not to the whole catalog.',
     "explanation": 'The selection step keeps the five named models. Cost mode then keeps the cheapest third of those five, rounded up: DeepSeek-V4-Flash and gpt-5.4-mini.',
     "request": {"messages": msgs(), "compatibility": "new", "routing_mode": "cost", "routing_constraints": {
         "models": ["gpt-5.4-mini", "gpt-5.5", "claude-sonnet-5-5", "DeepSeek-V4-Flash", "gpt-6-astra"]}}},
    {"title": 'One provider only',
     "why": 'Restricting to one provider, for example for contractual reasons.',
     "explanation": 'Only the three Claude models in the `new` catalog remain: Haiku 5.5, Sonnet 5.5 and Opus 5.5.',
     "request": {"messages": msgs(), "compatibility": "new", "routing_constraints": {"providers": ["Anthropic"]}}},
    {"title": 'Coding agent profile',
     "why": 'The built-in `decision-router-coding` profile, as a Foundry agent would use it: `model` names the profile, and the request has tools with parallel tool calls enabled.',
     "explanation": 'The profile selects the `new` catalog, keeps its nine coding models and turns pinning on. The request asks for parallel tool calls, so FW-GLM-5.3 and DeepSeek-V4-Pro are removed: their `parallel_tools` value is `unknown`. Seven models remain. When an agent sends `parallel_tool_calls: true`, those two models are never offered.',
     "request": {"model": "decision-router-coding", "messages": msgs("Fix the failing test in utils.py",
                                                                     "You are a coding agent."),
                 "tools": [TOOL], "parallel_tool_calls": True}},
    {"title": 'Native Claude request (claude_translation: false)',
     "why": 'A caller that already speaks the Anthropic Messages API sends a native body. Only Claude can take it.',
     "explanation": 'The `api` step removes every model that is not Claude. The three Claude models in the `new` catalog remain, and the chosen one receives the body unchanged.',
     "request": {"claude_translation": False, "compatibility": "new", "max_tokens": 1024,
                 "system": "You are a careful reviewer.",
                 "messages": [{"role": "user", "content": [{"type": "text", "text": USER}]}]}},
    {"title": 'Operator allow-list',
     "why": '`ROUTER_MODEL_ALLOWLIST` caps every request, whatever it asks for. Operators use it to limit spending or to allow only approved models.',
     "explanation": 'Only the four allow-listed models remain. A request cannot add a model the operator has not allowed.',
     "settings": {"model_allowlist": ("gpt-5.4-mini", "gpt-5.5", "claude-opus-5", "grok-4")},
     "request": {"messages": msgs()}},
    {"title": 'Previews excluded',
     "why": 'Production callers often want generally available models only. `allow_preview: false` overrides `ROUTER_ALLOW_PREVIEW` for one request.',
     "explanation": 'grok-4.6 and claude-fable-5-1 are in preview and are removed. 24 models remain.',
     "request": {"messages": msgs(), "routing_constraints": {"allow_preview": False}}},
    {"title": 'Region where only global Fireworks models qualify',
     "why": 'A region outside the published availability lists. It shows that a region filter does not imply that processing stays in that region.',
     "explanation": "No Azure-hosted model lists West India, so they are all removed, and gpt-oss-120b and grok-4 are removed because their regions aren't published. The three Fireworks models remain, because they deploy globally from any region. Their inference runs on Fireworks infrastructure, so a caller who wanted data to stay in India must also set `inference_in_azure` (see the example 'Region with no Azure availability, inference in Azure' below).",
     "request": {"messages": msgs(), "routing_constraints": {"region": "westindia"}}},
    {"title": 'Only one model passes',
     "why": 'When stage 1 leaves one model there is nothing to choose, and the router skips Decision-1.',
     "explanation": 'The router calls gpt-5.5 directly. This saves the Decision-1 call (time and cost); the decision log records it as `skipped`.',
     "request": {"messages": msgs(), "compatibility": "new", "routing_constraints": {"models": ["gpt-5.5"]}}},
    # ------------------------------------------------------------------ no models left
    {"title": 'Tools requested from models without tool calling',
     "why": 'A developer subset of small, cheap models, then a request that needs tools. It is the most likely way to get an empty result in practice.',
     "explanation": 'All three chosen models have `tools: no`.',
     "request": {"messages": msgs(), "compatibility": "new", "tools": [TOOL],
                 "routing_constraints": {"models": ["Phi-4", "Phi-4-mini-instruct", "Llama-4-Maverick-17B-128E-Instruct-FP8"]}}},
    {"title": 'Tools on gpt-6.1-sol only',
     "why": "gpt-6.1-sol supports tool calling, but only through the Responses API, which this router doesn't serve.",
     "explanation": "Through Chat Completions, gpt-6.1-sol can't take tools (`tools: no`), so the only chosen model is removed.",
     "request": {"messages": msgs(), "compatibility": "new", "tools": [TOOL],
                 "routing_constraints": {"models": ["gpt-6.1-sol"]}}},
    {"title": 'Regional (Standard) deployment type',
     "why": '`deployment_type: standard` asks for regional deployments.',
     "explanation": 'No model in either catalog has published Standard (regional) availability in the docs snapshot, so every model is removed. This request can never succeed with the current catalogs.',
     "request": {"messages": msgs(), "routing_constraints": {"deployment_type": "standard"}}},
    {"title": 'Old catalog, Claude only, inference in Azure',
     "why": 'Claude with processing kept on Azure, using the default `old` catalog.',
     "explanation": 'Every Claude model in `old` is version 1, the Anthropic-hosted version Model Router lists, so `inference_in_azure` removes all of them. The same request with `compatibility: new` returns the three Azure-hosted Claude models.',
     "request": {"messages": msgs(), "routing_constraints": {"providers": ["Anthropic"], "inference_in_azure": True}}},
    {"title": 'Native Claude request on the old catalog, inference in Azure',
     "why": 'The same conflict reached through `claude_translation: false`.',
     "explanation": 'The `api` step keeps the eight Claude models, and the location step removes them all because they are Anthropic-hosted.',
     "request": {"claude_translation": False, "max_tokens": 512,
                 "messages": [{"role": "user", "content": USER}],
                 "routing_constraints": {"inference_in_azure": True}}},
    {"title": 'Native Claude request limited to OpenAI',
     "why": 'Contradictory request: a native Claude body, restricted to OpenAI models.',
     "explanation": 'Only Claude can take the body and only OpenAI is allowed, so nothing remains.',
     "request": {"claude_translation": False, "compatibility": "new", "max_tokens": 512,
                 "messages": [{"role": "user", "content": USER}],
                 "routing_constraints": {"providers": ["OpenAI"]}}},
    {"title": "Claude in a region where it isn't offered",
     "why": 'Claude is offered in nine US regions and Sweden Central only.',
     "explanation": 'Japan East is not one of them, so all three Claude models are removed.',
     "request": {"messages": msgs(), "compatibility": "new",
                 "routing_constraints": {"providers": ["Anthropic"], "region": "japaneast"}}},
    {"title": 'Only preview models, previews excluded',
     "why": 'A subset of preview models combined with `allow_preview: false`.',
     "explanation": 'The lifecycle step removes both models before the selection step looks for them.',
     "request": {"messages": msgs(), "routing_constraints": {"models": ["grok-4.6", "claude-fable-5-1"],
                                                             "allow_preview": False}}},
    {"title": 'Subset outside the operator allow-list',
     "why": 'A request that names models the operator has not allowed.',
     "explanation": "The allow-list keeps two models; neither is in the request's subset.",
     "settings": {"model_allowlist": ("gpt-5.4-mini", "gpt-5.5")},
     "request": {"messages": msgs(), "routing_constraints": {"models": ["claude-opus-5", "grok-4"]}}},
    {"title": 'Every chosen model excluded',
     "why": 'A configuration mistake: the same models listed in both `models` and `exclude_models`.',
     "explanation": 'Nothing is left after selection.',
     "request": {"messages": msgs(), "routing_constraints": {"models": ["gpt-5.5", "gpt-5.4"],
                                                             "exclude_models": ["gpt-5.5", "gpt-5.4"]}}},
    {"title": 'Prompt larger than every context window',
     "why": 'About 1.2 million tokens of prompt, above the largest context window (1,050,000 tokens).',
     "explanation": 'Every model is removed by the size step.',
     "request": {"messages": msgs("Analyse this log: " + "x" * 4_800_000), "compatibility": "new"}},
    {"title": 'Minimum context larger than any model',
     "why": '`min_context_tokens: 2000000` asks for a context window no model has.',
     "explanation": 'Every model is removed by the size step.',
     "request": {"messages": msgs(), "compatibility": "new", "routing_constraints": {"min_context_tokens": 2_000_000}}},
    {"title": 'Output longer than the chosen models allow',
     "why": '`max_completion_tokens: 200000` with three models whose output limit is 128,000 tokens.',
     "explanation": 'All three are removed. Some models allow more output (Llama 4 Maverick, grok-4.7, DeepSeek V4), but they are not in the subset.',
     "request": {"messages": msgs(), "compatibility": "new", "max_completion_tokens": 200_000,
                 "routing_constraints": {"models": ["gpt-5.5", "claude-opus-5-5", "gpt-6-astra"]}}},
    {"title": 'JSON mode plus computer use',
     "why": 'Two capabilities no single model is recorded as having.',
     "explanation": 'The models with `json_object: yes` (DeepSeek V4, grok-4.7) have `computer_use: unknown`; the models with computer use have `json_object` unknown or no.',
     "request": {"messages": msgs(), "compatibility": "new", "response_format": {"type": "json_object"},
                 "routing_constraints": {"capabilities": ["computer_use"]}}},
    {"title": 'Streaming from models whose streaming support is unpublished',
     "why": 'Streaming to three models the catalog has no streaming source for.',
     "explanation": 'All three have `streaming: unknown` and are removed. With `ROUTER_UNKNOWN_CAPABILITY=eligible` all three would pass.',
     "request": {"messages": msgs(), "stream": True,
                 "routing_constraints": {"models": ["Llama-4-Maverick-17B-128E-Instruct-FP8", "DeepSeek-V3.2", "grok-4"]}}},
    {"title": 'Fireworks with Data Zone residency',
     "why": 'Fireworks models with a Data Zone deployment type.',
     "explanation": 'Fireworks models deploy as Global Standard only, so all three are removed.',
     "request": {"messages": msgs(), "routing_constraints": {"providers": ["Fireworks"],
                                                             "deployment_type": "data_zone_standard"}}},
    {"title": 'Image input with text-only models',
     "why": 'An image sent with a provider restriction that leaves only text-only models.',
     "explanation": 'DeepSeek and Microsoft models in `new` are all text-only, so the six candidates are removed.',
     "request": {"compatibility": "new", "messages": [{"role": "user", "content": [
         {"type": "text", "text": "Describe this chart."}, IMAGE]}],
                 "routing_constraints": {"providers": ["DeepSeek", "Microsoft"]}}},
    {"title": 'EU residency with JSON mode',
     "why": 'Two reasonable requirements together can leave a single model. It shows how requirements combine.',
     "explanation": "JSON mode leaves the three models with `json_object: yes`. Of those, only DeepSeek-V4-Flash is offered in Sweden Central's Data Zone, so it is called directly. If it fails, there is no other model to fall back to.",
     "request": {"messages": msgs(), "compatibility": "new", "response_format": {"type": "json_object"},
                 "routing_constraints": {"region": "swedencentral", "deployment_type": "data_zone_standard",
                                         "inference_in_azure": True}}},
    {"title": 'Reasoning required from non-reasoning models',
     "why": '`capabilities: ["reasoning"]` with a subset of models that have no reasoning mode.',
     "explanation": 'Phi-4, Phi-4-mini-instruct and Llama 4 Maverick have `reasoning: unknown`, so all three are removed.',
     "request": {"messages": msgs(), "compatibility": "new", "routing_constraints": {
         "capabilities": ["reasoning"], "models": ["Phi-4", "Phi-4-mini-instruct", "Llama-4-Maverick-17B-128E-Instruct-FP8"]}}},
    {"title": 'Region with no Azure availability, inference in Azure',
     "why": 'West India with processing kept on Azure.',
     "explanation": 'The Azure-hosted models are not offered in West India and the Fireworks models run outside Azure, so nothing remains.',
     "request": {"messages": msgs(), "routing_constraints": {"region": "westindia", "inference_in_azure": True}}},
    # ------------------------------------------------------------------ rejected before stage 1
    {"title": 'Model name not in the catalog',
     "why": 'A typo or an unknown model name in `routing_constraints.models`.',
     "explanation": 'Unknown names are rejected rather than ignored, so a typo never silently widens or empties the choice.',
     "request": {"messages": msgs(), "routing_constraints": {"models": ["gpt-9"]}}},
    {"title": 'Old-catalog model requested from the new catalog',
     "why": 'A model that exists in `old` but not in `new`.',
     "explanation": 'Names are checked against the catalog the request selected. claude-opus-5 is only in `old`.',
     "request": {"messages": msgs(), "compatibility": "new", "routing_constraints": {"models": ["claude-opus-5"]}}},
]
