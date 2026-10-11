# Model catalogs

The router chooses from one of two catalogs, selected by `compatibility` (per request, or
`ROUTER_COMPATIBILITY` for the default):

| Compatibility | File | What it is | Use it for |
|---|---|---|---|
| `old` (default) | `decision-router/config/catalog_old.json` | Model Router version 2025-11-18's routing pool (35 models) minus its 9 deprecated models: 26 models | Comparing with Model Router like for like |
| `new` | `decision-router/config/catalog_new.json` | The latest Azure-available model per provider and role, plus MAI-Thinking-1 and three Phi-4 models: 23 models | Production routing with the newest models |

Removed from `old` as deprecated:

- `gpt-4.1`, `gpt-4.1-mini`, `gpt-4.1-nano`, `gpt-4o`, `gpt-4o-mini`, `o4-mini`: listed as
  deprecated in Microsoft's retirement schedule (5 October 2026).
- `gpt-5`, `gpt-5-mini`, `gpt-5-nano` (version 2025-08-07): the schedule still says GA, but
  under the Foundry lifecycle policy a standard model is Deprecated (closed to new customers)
  12 months after launch, which these reached on 2026-08-07. They retire on 2027-02-09.
  `lifecycle_policy` in the spec encodes the rule, and the builder fails if a retained model
  has passed it.

`old` keeps `claude-haiku-4-5` (retires 2026-11-15) and `claude-sonnet-4-5` (retires 2026-11-30)
because Model Router still routes to them; `new` leaves them out.

## Tiers come from prices

`tier` orders models by price (1 = cheapest). It sets the routing modes' price bands, the
price rank Decision-1 is shown, and the tie-break. The builder computes it from
`config/model_pricing.json`: input price, then output price, then cached-input price, then
name. To change the order, change the prices and rebuild (below); a model without a price
fails the build.

Claude prices are Anthropic's standard rates, which Claude in Microsoft Foundry bills at (US
Data Zone Standard deployments cost 10% more). `claude-haiku-5-5` costs twice as much for
prompts over 100,000 tokens; the file holds the shorter-prompt rate.

## Claude versions and hosting

Foundry numbers Claude versions by where they run: **version 1** on Anthropic infrastructure,
**version 2** on Azure. Data Zone Standard is offered only for version 2.

- `old` uses the versions Model Router lists: version 1 for every Claude model, and the dated
  snapshots `20250929` (Sonnet 4.5) and `20251001` (Haiku 4.5), treated as version 1. So every
  Claude model in `old` runs outside Azure, and `inference_in_azure` removes them.
- `new` uses version 2 (Azure-hosted) for Haiku 5.5, Sonnet 5.5 and Opus 5.5.

Deploy the version the catalog names; the router cannot tell which one a deployment runs.

## Descriptions

Decision-1 tells the options apart only by their descriptions (plus the price rank the router
appends), so each one says:

- what the model is best at, in terms of task types a request might contain;
- what it handles badly or cannot do (no tools, short output limit, text only, English only);
- where it sits on speed and capability relative to its neighbours;
- facts that affect the answer itself, such as inference outside Azure or extra refusal classifiers.

Stage 1 already enforces hard limits (tools, images, context size, region), so a description
does not need to repeat them for filtering; it repeats them only where they help Decision-1
judge quality. Edit descriptions in `catalog_spec.json` and rebuild.

## How the files are built

The catalogs are generated, not edited by hand:

```bash
git clone --filter=blob:none --sparse https://github.com/MicrosoftDocs/azure-ai-docs.git /tmp/azure-ai-docs
git -C /tmp/azure-ai-docs sparse-checkout set articles/foundry
git -C /tmp/azure-ai-docs checkout 87fa4a9688b9562e3c520ca718dcfc29f9fc488a   # the commit pinned in catalog_spec.json
cd decision-router && python scripts/build_catalogs.py --docs /tmp/azure-ai-docs
```

- **`config/catalog_spec.json`** (hand-maintained) holds which models each catalog contains,
  the description Decision-1 reads, the API type, and any value the docs do not publish, each
  tagged with its source.
- **`config/model_pricing.json`** (hand-maintained) holds USD list prices; `tier` is computed from it.
- **The docs snapshot** supplies:
  - lifecycle and retirement dates (model retirement schedule);
  - Model Router membership (Model Router supported-models table);
  - regions per deployment type (Global Standard, Data Zone Standard, Standard region tables);
  - context, input and output limits, and capabilities (model capability tables, Claude model table).
- **The builder fails** if a model can't be found in the docs it is told to read, or if `old`
  is missing a model Model Router lists and that isn't recorded as excluded.
- **Tests** fail if the spec changes without a rebuild (`test_catalogs_are_in_sync_with_the_spec`)
  or if tiers stop following prices (`test_tiers_follow_prices`).
- `--tables` also prints the summary tables at the end of this page.

Values not in the docs snapshot:

| Values | Source used |
|---|---|
| Fireworks models' limits and capabilities | Early project catalog (Foundry model cards, retrieved 2026-10-10) |
| Claude Opus 5 image input | Early project catalog |
| grok-4-1-fast-reasoning reasoning | xAI publishes it as the reasoning variant of grok-4-1-fast |
| Streaming for Azure OpenAI chat models | Chat Completions API behaviour; the capability tables list it for only some models |
| Claude tools, parallel tools, streaming, structured output | What the router's Messages adapter translates: `json_schema` becomes `output_config.format` (structured output: yes); JSON mode is not translated (`json_object`: no) |
| Fireworks regions | Global (confirmed by the project owner): `region_status: global`, Global Standard only; passes any region filter, excluded by `inference_in_azure` because inference runs on Fireworks infrastructure |
| Regions for gpt-oss-120b and grok-4 | Not published: `region_status: unknown`, so region-constrained requests exclude them |

## Entry format

Each model entry keeps the simple fields (`name`, `deployment`, `tier`, `aliases`,
`description`) and adds what stage 1 filters on:

```json
{
  "name": "gpt-5.4-mini", "deployment": "gpt-5.4-mini", "tier": 8, "aliases": [],
  "description": "Compact GPT-5.4 reasoning model and a good default for everyday work: ...",
  "provider": "OpenAI", "version": "2026-03-17", "lifecycle": "ga", "retires": "2027-09-21",
  "in_model_router": true,
  "api": "openai_chat", "endpoint_path": "/openai/v1/chat/completions",
  "limits": {"context_tokens": 400000, "max_input_tokens": 272000, "max_output_tokens": 128000},
  "modalities": {"input": ["text", "image"], "output": ["text"]},
  "capabilities": {"tools": "yes", "parallel_tools": "yes", "structured_output": "yes", "json_object": "unknown",
                   "streaming": "yes", "reasoning": "yes", "image_input": "yes", "computer_use": "yes"},
  "hosting": {"infrastructure": "azure", "azure_region_controls_processing": true},
  "deployment_types": ["data_zone_standard", "global_standard"],
  "regions": {"global_standard": ["australiaeast", "..."], "data_zone_standard": ["eastus", "..."]},
  "region_status": "published",
  "source": {"docs_commit": "87fa4a9...", "files": ["articles/foundry/..."]}
}
```

- **`api`** sets how the model is called:
  - `openai_chat`: `/openai/v1/chat/completions`.
  - `mai_chat`: `/mai/v1/chat/completions`. MAI-Thinking-1 is OpenAI-compatible but has its own path.
  - `anthropic_messages`: `/anthropic/v1/messages`, through the translation layer.
- **Capability values** are `yes`, `no` or `unknown`. See `STAGE1_FILTERING.md` for how unknowns are treated.
- **Phi models** are assumed to be served on the `/openai/v1` route, like other Foundry models.
  Confirm this on your deployment before relying on them; if they aren't, change their `api` in the spec.

### `old` (26 models)

| Tier | Model | Provider | Version | Lifecycle | Retires | Inference on | Context | In/out $ per 1M | Tools | Images | JSON schema |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 | `FW-GLM-5.3-Flash` | Fireworks | 1 | ga | – | fireworks | 1M | 0.1 / 0.4 | yes | yes | unknown |
| 2 | `gpt-5.4-nano` | OpenAI | 2026-03-17 | ga | 2027-09-21 | azure | 400K | 0.1 / 0.8 | yes | yes | yes |
| 3 | `gpt-5.6-luna` | OpenAI | 2026-07-09 | ga | 2028-01-11 | azure | 1.05M | 0.1 / 0.8 | yes | yes | yes |
| 4 | `gpt-oss-120b` | OpenAI | 1 | ga | – | azure | 128K | 0.15 / 0.6 | yes | no | yes |
| 5 | `Llama-4-Maverick-17B-128E-Instruct-FP8` | Meta | 1 | ga | – | azure | 1M | 0.19 / 0.85 | no | yes | unknown |
| 6 | `grok-4-1-fast-reasoning` | xAI | 1 | ga | – | azure | 125K | 0.2 / 0.5 | yes | yes | unknown |
| 7 | `DeepSeek-V3.2` | DeepSeek | 1 | ga | – | azure | 125K | 0.28 / 0.42 | yes | no | unknown |
| 8 | `gpt-5.4-mini` | OpenAI | 2026-03-17 | ga | 2027-09-21 | azure | 400K | 0.375 / 3 | yes | yes | yes |
| 9 | `FW-GLM-5.3` | Fireworks | 1 | ga | – | fireworks | 1M | 0.6 / 2.5 | yes | no | yes |
| 10 | `FW-Kimi-K3` | Fireworks | 1 | ga | – | fireworks | 1M | 0.6 / 2.5 | yes | yes | unknown |
| 11 | `claude-haiku-4-5` | Anthropic | 20251001 | ga | 2026-11-15 | anthropic | 200K | 1 / 5 | yes | yes | yes |
| 12 | `gpt-5.6-terra` | OpenAI | 2026-07-09 | ga | 2028-01-11 | azure | 1.05M | 1 / 8 | yes | yes | yes |
| 13 | `gpt-5.2` | OpenAI | 2025-12-11 | ga | 2027-06-08 | azure | 400K | 1.75 / 14 | yes | yes | yes |
| 14 | `claude-sonnet-5` | Anthropic | 1 | ga | 2027-06-30 | anthropic | 1M | 2 / 10 | yes | yes | yes |
| 15 | `gpt-5.4` | OpenAI | 2026-03-05 | ga | 2027-09-02 | azure | 1.05M | 2.5 / 15 | yes | yes | yes |
| 16 | `gpt-5.5` | OpenAI | 2026-04-24 | ga | 2027-10-26 | azure | 1.05M | 2.5 / 15 | yes | yes | yes |
| 17 | `gpt-5.6-sol` | OpenAI | 2026-07-09 | ga | 2028-01-11 | azure | 1.05M | 2.5 / 15 | yes | yes | yes |
| 18 | `claude-sonnet-4-5` | Anthropic | 20250929 | ga | 2026-11-30 | anthropic | 200K | 3 / 15 | yes | yes | yes |
| 19 | `grok-4` | xAI | 1 | ga | – | azure | 262K | 3 / 15 | yes | no | unknown |
| 20 | `grok-4.6` | xAI | 1 | preview | – | azure | 200K | 3 / 15 | yes | yes | unknown |
| 21 | `claude-opus-4-6` | Anthropic | 1 | ga | 2027-02-02 | anthropic | 1M | 5 / 25 | yes | yes | yes |
| 22 | `claude-opus-4-7` | Anthropic | 1 | ga | 2027-04-06 | anthropic | 1M | 5 / 25 | yes | yes | yes |
| 23 | `claude-opus-4-8` | Anthropic | 1 | ga | 2027-09-01 | anthropic | 1M | 5 / 25 | yes | yes | yes |
| 24 | `claude-opus-5` | Anthropic | 1 | ga | 2027-07-08 | anthropic | 1M | 5 / 25 | yes | yes | yes |
| 25 | `claude-fable-5-1` | Anthropic | 1 | preview | 2027-12-05 | anthropic | 1M | 10 / 50 | yes | yes | yes |
| 26 | `gpt-6-astra` | OpenAI | 2026-09-03 | ga | – | azure | 1.05M | 10 / 50 | yes | yes | yes |


### `new` (23 models)

| Tier | Model | Provider | Version | Lifecycle | Retires | Inference on | Context | In/out $ per 1M | Tools | Images | JSON schema |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 | `Phi-4-mini-instruct` | Microsoft | 1 | ga | – | azure | 128K | 0.075 / 0.3 | no | no | unknown |
| 2 | `FW-GLM-5.3-Flash` | Fireworks | 1 | ga | – | fireworks | 1M | 0.1 / 0.4 | yes | yes | unknown |
| 3 | `claude-haiku-5-5` | Anthropic | 2 | ga | – | azure | 1M | 0.1 / 0.5 | yes | yes | yes |
| 4 | `gpt-5.4-nano` | OpenAI | 2026-03-17 | ga | 2027-09-21 | azure | 400K | 0.1 / 0.8 | yes | yes | yes |
| 5 | `Phi-4` | Microsoft | 7 | ga | – | azure | 16K | 0.125 / 0.5 | no | no | unknown |
| 6 | `gpt-oss-120b` | OpenAI | 1 | ga | – | azure | 128K | 0.15 / 0.6 | yes | no | yes |
| 7 | `Phi-4-reasoning` | Microsoft | 1 | ga | – | azure | 32K | 0.15 / 0.6 | no | no | unknown |
| 8 | `Llama-4-Maverick-17B-128E-Instruct-FP8` | Meta | 1 | ga | – | azure | 1M | 0.19 / 0.85 | no | yes | unknown |
| 9 | `grok-4-1-fast-reasoning` | xAI | 1 | ga | – | azure | 125K | 0.2 / 0.5 | yes | yes | unknown |
| 10 | `DeepSeek-V4-Flash` | DeepSeek | 2026-04-23 | ga | 2028-02-20 | azure | 1M | 0.2 / 0.8 | yes | no | unknown |
| 11 | `gpt-5.4-mini` | OpenAI | 2026-03-17 | ga | 2027-09-21 | azure | 400K | 0.375 / 3 | yes | yes | yes |
| 12 | `gpt-6-luna` | OpenAI | 2026-09-22 | ga | – | azure | 1.05M | 0.5 / 2.5 | yes | yes | yes |
| 13 | `FW-GLM-5.3` | Fireworks | 1 | ga | – | fireworks | 1M | 0.6 / 2.5 | yes | no | yes |
| 14 | `FW-Kimi-K3` | Fireworks | 1 | ga | – | fireworks | 1M | 0.6 / 2.5 | yes | yes | unknown |
| 15 | `DeepSeek-V4-Pro` | DeepSeek | 2026-04-23 | ga | 2028-02-20 | azure | 1M | 1 / 4 | yes | no | unknown |
| 16 | `gpt-5.6-terra` | OpenAI | 2026-07-09 | ga | 2028-01-11 | azure | 1.05M | 1 / 8 | yes | yes | yes |
| 17 | `MAI-Thinking-1` | Microsoft | 2026-06-01 | preview | – | azure | 250K | 1.5 / 7.5 | yes | no | unknown |
| 18 | `claude-sonnet-5-5` | Anthropic | 2 | ga | – | azure | 1M | 2 / 10 | yes | yes | yes |
| 19 | `gpt-6.1-sol` | OpenAI | 2026-09-29 | ga | – | azure | 1.05M | 2 / 10 | no | yes | yes |
| 20 | `gpt-5.5` | OpenAI | 2026-04-24 | ga | 2027-10-26 | azure | 1.05M | 2.5 / 15 | yes | yes | yes |
| 21 | `grok-4.7` | xAI | 1 | ga | – | azure | 500K | 3 / 15 | yes | yes | unknown |
| 22 | `claude-opus-5-5` | Anthropic | 2 | ga | – | azure | 1M | 4 / 20 | yes | yes | yes |
| 23 | `gpt-6-astra` | OpenAI | 2026-09-03 | ga | – | azure | 1.05M | 10 / 50 | yes | yes | yes |
