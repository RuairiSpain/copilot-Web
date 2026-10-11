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
- **Default settings** unless an example says otherwise: catalog `old`
  (`ROUTER_COMPATIBILITY`), previews allowed, no operator allow-list.
- **Capabilities are `yes` or `no`.** Stage 1 filters on five: `tools`, `structured_output`,
  `streaming`, `reasoning` and `image_input`. `no` covers both "a source says it is
  unsupported" and "no source confirms it", so a model is only offered for what a source
  confirms.
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
Running the examples exposed catalog gaps, which were researched in this order: Microsoft's
docs, the providers' own docs, then blogs and forums. Provider sites (OpenAI, xAI, DeepSeek,
Fireworks, Hugging Face) could not be reached from the build environment, so only Microsoft's
docs were read directly.

1. **Streaming (A9):** Microsoft's Foundry pages give streaming instructions and examples for
   MAI-Thinking-1, grok-4.7 and DeepSeek-V4-Pro (`yes`). No source was found for the Phi
   models, Llama 4 Maverick, grok-4-1-fast-reasoning, DeepSeek-V4-Flash or FW-Kimi-K3 (`no`).
2. **GPT-5.6 tools (A5):** Microsoft's reasoning-models page says gpt-5.6 models reject Chat
   Completions requests that include `tools` unless `reasoning_effort` is `none`. This router
   calls Chat Completions, so their `tools` value is `no`, and gpt-5.6-terra is not in the
   coding profile.
3. **Removed capabilities:** `parallel_tools`, `json_object` and `computer_use` were mostly
   unconfirmed and are no longer in the catalog, so stage 1 doesn't filter on them (A8, R1).
4. **Standard (regional) deployments (Z3):** no model has published Standard availability, so
   `deployment_type: standard` always returns no models.

A `no` that only means "unconfirmed" can be changed to `yes` by confirming the capability with
one test request against the deployment, then recording the source in `catalog_spec.json`.
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
     "explanation": "Stage 1 removes the six models that can't call tools through Chat Completions: the three Phi models, Llama 4 Maverick, gpt-6.1-sol (tool calling only through the Responses API) and gpt-5.6-terra (tools only with reasoning turned off). Claude stays, because the translation layer converts tool calls. 17 models remain.",
     "request": {"messages": msgs("Find where the retry limit is set."), "compatibility": "new", "tools": [TOOL]}},
    {"title": 'Image input',
     "why": 'Requests with an `image_url` part need a vision-capable model; sending them to a text-only model would fail or ignore the image.',
     "explanation": 'Eight text-only models are removed (Phi, gpt-oss-120b, DeepSeek V4, FW-GLM-5.3, MAI-Thinking-1). 15 models that accept images remain.',
     "request": {"compatibility": "new", "messages": [{"role": "user", "content": [
         {"type": "text", "text": "What does this architecture diagram show?"}, IMAGE]}]}},
    {"title": 'JSON-schema output',
     "why": 'Callers that parse the answer programmatically send `response_format: json_schema`. Only models that enforce the schema should be offered.',
     "explanation": 'Models with `structured_output: yes` remain, including the three Claude models (the adapter sends the schema as `output_config.format`). The 11 removed models have `structured_output: no`: no source confirms that they enforce a schema, and the catalog records anything unconfirmed as `no`.',
     "request": {"messages": msgs(), "compatibility": "new", "response_format": SCHEMA}},
    {"title": 'JSON mode (json_object)',
     "why": 'JSON mode asks for any valid JSON without a schema. It shows that JSON mode is not a capability stage 1 filters on.',
     "explanation": "Nothing is removed: `json_object` is no longer a catalog capability, so all 23 models are candidates. A model that doesn't support JSON mode may answer in free text. Claude always does, because the translation layer drops `response_format: json_object` (the Messages API has no equivalent). Callers that need guaranteed JSON should send a JSON schema instead (example A7).",
     "request": {"messages": msgs(), "compatibility": "new", "response_format": {"type": "json_object"}}},
    {"title": 'Streaming',
     "why": '`stream: true` is common in chat front ends. This shows that streaming support is a filter, not a formatting detail.',
     "explanation": 'Seven models are removed because no source confirms they stream, so the catalog records `streaming: no`: the three Phi models, Llama 4 Maverick, grok-4-1-fast-reasoning, DeepSeek-V4-Flash and FW-Kimi-K3. 16 models remain.',
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
     "explanation": 'The profile selects the `new` catalog, keeps its eight coding models, requires tool calling and turns pinning on. All eight support tools, so all eight go to Decision-1. `parallel_tool_calls: true` adds no requirement, because parallel tool calls are no longer a catalog capability.',
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
    {"title": 'Named cost band: high',
     "why": 'The `new` catalog\'s named bands pick models by output price instead of by thirds. `high` is $10 to $25 per million output tokens.',
     "explanation": '`routing_cost_band` replaces the mode\'s price band. The five models with output prices from $10 to $25 remain; the mode (`balanced`) still sets the instructions Decision-1 receives.',
     "request": {"compatibility": "new", "routing_cost_band": "high", "messages": msgs()}},
    {"title": 'Dollar range instead of a band name',
     "why": 'A caller gives its own range. `3-10` covers the $3 to $10 gap that no named band includes.',
     "explanation": 'Both ends are inclusive, so gpt-5.4-mini ($3) and the two $10 models are kept alongside the three models that sit in the gap: DeepSeek-V4-Pro, gpt-5.6-terra and MAI-Thinking-1.',
     "request": {"compatibility": "new", "routing_cost_band": "3-10", "messages": msgs()}},
    {"title": 'Cost band profile with tools',
     "why": 'A Foundry agent that can\'t send `routing_cost_band` uses the built-in `decision-router-low` profile, and its request carries tools.',
     "explanation": 'The profile selects the `new` catalog and the `low` band. The capabilities step first removes the six models without Chat Completions tool calling: the three Phi models, Llama 4 Maverick, gpt-5.6-terra and gpt-6.1-sol. The band then keeps the six remaining models priced at $1 or less per million output tokens.',
     "request": {"model": "decision-router-low", "messages": msgs("Look up the order status."), "tools": [TOOL]}},
    {"title": 'Action with a cost label: code, top',
     "why": 'With `routing_constraints.actions`, the labels low, medium, high and top mean even groups of the models tagged for that action, by output price, instead of fixed dollar ranges.',
     "explanation": 'The capabilities step keeps the 14 models tagged `code`. They split into four groups of 3, 4, 3 and 4 by output price, and `top` is the most expensive group: gpt-5.5, grok-4.7, claude-opus-5-5 and gpt-6-astra. gpt-5.5 and grok-4.7 cost the same ($15), so they stay in one group.',
     "request": {"compatibility": "new", "routing_cost_band": "top", "messages": msgs("Refactor this module."),
                 "routing_constraints": {"actions": ["code"]}}},
    {"title": 'Action with a cost label, after a tools filter',
     "why": 'The groups are formed from what the earlier steps leave, so a request with tools gets a split of the code models that can call tools.',
     "explanation": 'Tool calling removes gpt-5.6-terra from the 14 code models, and the remaining 13 are split again, into groups of 3, 3, 3 and 4. The `high` group becomes DeepSeek-V4-Pro, MAI-Thinking-1 and claude-sonnet-5-5; with all 14 code models it is MAI-Thinking-1, gpt-5.6-terra and claude-sonnet-5-5.',
     "request": {"compatibility": "new", "routing_cost_band": "high", "messages": msgs("Fix the failing test."),
                 "tools": [TOOL], "routing_constraints": {"actions": ["code"]}}},
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
    {"title": 'Streaming from models whose streaming support is unpublished',
     "why": 'Streaming to three models the catalog has no streaming source for.',
     "explanation": 'No source confirms streaming for any of the three, so the catalog records `streaming: no` and all three are removed.',
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
    {"title": 'Reasoning required from non-reasoning models',
     "why": '`capabilities: ["reasoning"]` with a subset of models that have no reasoning mode.',
     "explanation": 'Phi-4, Phi-4-mini-instruct and Llama 4 Maverick have `reasoning: no`, so all three are removed.',
     "request": {"messages": msgs(), "compatibility": "new", "routing_constraints": {
         "capabilities": ["reasoning"], "models": ["Phi-4", "Phi-4-mini-instruct", "Llama-4-Maverick-17B-128E-Instruct-FP8"]}}},
    {"title": 'Region with no Azure availability, inference in Azure',
     "why": 'West India with processing kept on Azure.',
     "explanation": 'The Azure-hosted models are not offered in West India and the Fireworks models run outside Azure, so nothing remains.',
     "request": {"messages": msgs(), "routing_constraints": {"region": "westindia", "inference_in_azure": True}}},
    {"title": 'Top band with Claude only',
     "why": 'A named band combined with a provider restriction that has no model in that band.',
     "explanation": 'The `top` band is $25 to $60 and, in `new`, contains only gpt-6-astra. No Claude model in `new` costs that much, so the `price_band` step removes the last three. Unlike the routing modes, a cost band can leave nothing.',
     "request": {"compatibility": "new", "routing_cost_band": "top", "messages": msgs(),
                 "routing_constraints": {"providers": ["Anthropic"]}}},
    # ------------------------------------------------------------------ rejected before stage 1
    {"title": 'Capability that is no longer in the catalog',
     "why": 'A request still asking for one of the removed capabilities (parallel_tools, json_object, computer_use).',
     "explanation": 'Unknown capability names are rejected rather than ignored, so a client that relied on a removed capability learns it is no longer filtered on.',
     "request": {"messages": msgs(), "routing_constraints": {"capabilities": ["parallel_tools"]}}},
    {"title": 'Model name not in the catalog',
     "why": 'A typo or an unknown model name in `routing_constraints.models`.',
     "explanation": 'Unknown names are rejected rather than ignored, so a typo never silently widens or empties the choice.',
     "request": {"messages": msgs(), "routing_constraints": {"models": ["gpt-9"]}}},
    {"title": 'Old-catalog model requested from the new catalog',
     "why": 'A model that exists in `old` but not in `new`.',
     "explanation": 'Names are checked against the catalog the request selected. claude-opus-5 is only in `old`.',
     "request": {"messages": msgs(), "compatibility": "new", "routing_constraints": {"models": ["claude-opus-5"]}}},
    {"title": 'Named cost band on the old catalog',
     "why": 'The named bands exist only in `new`; the `old` catalog keeps the routing modes\' thirds.',
     "explanation": 'The `old` catalog defines no band names, so `low` is rejected. A dollar range such as `"0-1"` works on either catalog.',
     "request": {"routing_cost_band": "low", "messages": msgs()}},
    {"title": 'Actions on the old catalog',
     "why": 'Action tags exist only in the `new` catalog.',
     "explanation": 'The `old` catalog\'s models have no action tags, so an `actions` constraint is rejected rather than removing every model.',
     "request": {"messages": msgs(), "routing_constraints": {"actions": ["code"]}}},
]
