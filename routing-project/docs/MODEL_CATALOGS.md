# Model catalogs

The router chooses from one of two catalogs, selected by `compatibility` (per request, or
`ROUTER_COMPATIBILITY` for the default):

| Compatibility | File | What it is | Use it for |
|---|---|---|---|
| `old` (default) | `decision-router/config/catalog_old.json` | Model Router version 2025-11-18's routing pool (35 models) minus its 6 deprecated models: 29 models | Comparing with Model Router like for like |
| `new` | `decision-router/config/catalog_new.json` | The latest Azure-available model per provider and role, plus MAI-Thinking-1 and three Phi-4 models: 23 models | Production routing with the newest models |

Removed from `old` as deprecated (Microsoft retirement schedule, 5 October 2026): `gpt-4.1`,
`gpt-4.1-mini`, `gpt-4.1-nano`, `gpt-4o`, `gpt-4o-mini`, `o4-mini`.

`old` keeps `claude-haiku-4-5` (retires 2026-11-15) and `claude-sonnet-4-5` (retires 2026-11-30)
because Model Router still routes to them; `new` leaves them out.

## Tiers are provisional

`tier` orders models by price (1 = cheapest). It sets the routing modes' price bands, the
price rank Decision-1 is shown, and the tie-break. Microsoft's price list could not be reached
when the catalogs were built, so the order below is an estimate and is marked
`"tier_status": "provisional"` in both files. To change it, edit `tier` in
`config/catalog_spec.json` and rebuild (below).

## How the files are built

The catalogs are generated, not edited by hand:

```bash
git clone --filter=blob:none --sparse https://github.com/MicrosoftDocs/azure-ai-docs.git /tmp/azure-ai-docs
git -C /tmp/azure-ai-docs sparse-checkout set articles/foundry
git -C /tmp/azure-ai-docs checkout 87fa4a9688b9562e3c520ca718dcfc29f9fc488a   # the commit pinned in catalog_spec.json
cd decision-router && python scripts/build_catalogs.py --docs /tmp/azure-ai-docs
```

- **`config/catalog_spec.json`** (hand-maintained) holds which models each catalog contains,
  their tier, the description Decision-1 reads, the API type, and any value the docs do not
  publish, each tagged with its source.
- **The docs snapshot** supplies:
  - lifecycle and retirement dates (model retirement schedule);
  - Model Router membership (Model Router supported-models table);
  - regions per deployment type (Global Standard, Data Zone Standard, Standard region tables);
  - context and output limits, and capabilities (model capability tables, Claude model table).
- **The builder fails** if a model can't be found in the docs it is told to read, or if `old`
  is missing a model Model Router lists and that isn't recorded as excluded.
- **A test** (`test_catalogs_are_in_sync_with_the_spec`) fails if the spec changes without a rebuild.

Values not in the docs snapshot:

| Values | Source used |
|---|---|
| Fireworks models' limits and capabilities | Early project catalog (Foundry model cards, retrieved 2026-10-10) |
| Claude Opus 5 and Fable 5.1 image input | Early project catalog |
| Streaming for Azure OpenAI chat models | Chat Completions API behaviour; the capability tables list it for only some models |
| Claude tools, parallel tools, streaming, structured output | What the router's Messages adapter translates (structured output: no) |
| Regions for Fireworks models, gpt-oss-120b and grok-4 | Not published: `region_status: unknown`, so region-constrained requests exclude them |

## Entry format

Each model entry keeps the simple fields (`name`, `deployment`, `tier`, `aliases`,
`description`) and adds what stage 1 filters on:

```json
{
  "name": "gpt-5.4-mini", "deployment": "gpt-5.4-mini", "tier": 9, "aliases": [],
  "description": "Compact GPT-5.4 model for high-volume everyday work: ...",
  "provider": "OpenAI", "version": "2026-03-17", "lifecycle": "ga", "retires": "2027-09-21",
  "in_model_router": true,
  "api": "openai_chat", "endpoint_path": "/openai/v1/chat/completions",
  "limits": {"context_tokens": 400000, "max_output_tokens": 128000},
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

### `old` (29 models)

| Tier | Model | Provider | Lifecycle | Retires | In Model Router | API | Inference on | Context | Tools | Images |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | `gpt-5-nano` | OpenAI | ga | 2027-02-09 | yes | openai_chat | azure | 400K | yes | yes |
| 2 | `gpt-5.4-nano` | OpenAI | ga | 2027-09-21 | yes | openai_chat | azure | 400K | yes | yes |
| 3 | `FW-GLM-5.3-Flash` | Fireworks | ga | – | yes | openai_chat | fireworks | 1M | yes | yes |
| 4 | `gpt-oss-120b` | OpenAI | ga | – | yes | openai_chat | azure | 131K | yes | no |
| 5 | `gpt-5-mini` | OpenAI | ga | 2027-02-09 | yes | openai_chat | azure | 400K | yes | yes |
| 6 | `grok-4-1-fast-reasoning` | xAI | ga | – | yes | openai_chat | azure | 128K | yes | yes |
| 7 | `Llama-4-Maverick-17B-128E-Instruct-FP8` | Meta | ga | – | yes | openai_chat | azure | 1M | no | yes |
| 8 | `DeepSeek-V3.2` | DeepSeek | ga | – | yes | openai_chat | azure | 128K | yes | no |
| 9 | `gpt-5.4-mini` | OpenAI | ga | 2027-09-21 | yes | openai_chat | azure | 400K | yes | yes |
| 10 | `gpt-5.6-luna` | OpenAI | ga | 2028-01-11 | yes | openai_chat | azure | 1.1M | yes | yes |
| 11 | `FW-Kimi-K3` | Fireworks | ga | – | yes | openai_chat | fireworks | 1M | yes | yes |
| 12 | `FW-GLM-5.3` | Fireworks | ga | – | yes | openai_chat | fireworks | 1M | yes | no |
| 13 | `claude-haiku-4-5` | Anthropic | ga | – | yes | anthropic_messages | azure | 200K | yes | yes |
| 14 | `gpt-5` | OpenAI | ga | 2027-02-09 | yes | openai_chat | azure | 400K | yes | yes |
| 15 | `gpt-5.2` | OpenAI | ga | 2027-06-08 | yes | openai_chat | azure | 400K | yes | yes |
| 16 | `gpt-5.4` | OpenAI | ga | 2027-09-02 | yes | openai_chat | azure | 1.1M | yes | yes |
| 17 | `gpt-5.6-terra` | OpenAI | ga | 2028-01-11 | yes | openai_chat | azure | 1.1M | yes | yes |
| 18 | `gpt-5.5` | OpenAI | ga | 2027-10-26 | yes | openai_chat | azure | 1.1M | yes | yes |
| 19 | `grok-4` | xAI | ga | – | yes | openai_chat | azure | 262K | yes | no |
| 20 | `grok-4.6` | xAI | preview | – | yes | openai_chat | azure | 200K | yes | yes |
| 21 | `claude-sonnet-4-5` | Anthropic | ga | 2026-11-30 | yes | anthropic_messages | anthropic | 200K | yes | yes |
| 22 | `claude-sonnet-5` | Anthropic | ga | 2027-06-30 | yes | anthropic_messages | azure | 1M | yes | yes |
| 23 | `gpt-5.6-sol` | OpenAI | ga | 2028-01-11 | yes | openai_chat | azure | 1.1M | yes | yes |
| 24 | `gpt-6-astra` | OpenAI | ga | – | yes | openai_chat | azure | 1.1M | yes | yes |
| 25 | `claude-opus-4-6` | Anthropic | ga | 2027-02-02 | yes | anthropic_messages | anthropic | 1M | yes | yes |
| 26 | `claude-opus-4-7` | Anthropic | ga | 2027-04-06 | yes | anthropic_messages | anthropic | 1M | yes | yes |
| 27 | `claude-opus-4-8` | Anthropic | ga | 2027-09-01 | yes | anthropic_messages | azure | 1M | yes | yes |
| 28 | `claude-opus-5` | Anthropic | ga | 2027-07-08 | yes | anthropic_messages | azure | 1M | yes | yes |
| 29 | `claude-fable-5-1` | Anthropic | preview | 2027-12-05 | yes | anthropic_messages | anthropic | 1M | yes | yes |

### `new` (23 models)

| Tier | Model | Provider | Lifecycle | Retires | In Model Router | API | Inference on | Context | Tools | Images |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | `Phi-4-mini-instruct` | Microsoft | ga | – | no | openai_chat | azure | 131K | no | no |
| 2 | `gpt-5.4-nano` | OpenAI | ga | 2027-09-21 | yes | openai_chat | azure | 400K | yes | yes |
| 3 | `Phi-4` | Microsoft | ga | – | no | openai_chat | azure | 16K | no | no |
| 4 | `FW-GLM-5.3-Flash` | Fireworks | ga | – | yes | openai_chat | fireworks | 1M | yes | yes |
| 5 | `gpt-oss-120b` | OpenAI | ga | – | yes | openai_chat | azure | 131K | yes | no |
| 6 | `Phi-4-reasoning` | Microsoft | ga | – | no | openai_chat | azure | 32K | no | no |
| 7 | `DeepSeek-V4-Flash` | DeepSeek | ga | 2028-02-20 | no | openai_chat | azure | 1M | yes | no |
| 8 | `grok-4-1-fast-reasoning` | xAI | ga | – | yes | openai_chat | azure | 128K | yes | yes |
| 9 | `Llama-4-Maverick-17B-128E-Instruct-FP8` | Meta | ga | – | yes | openai_chat | azure | 1M | no | yes |
| 10 | `gpt-5.4-mini` | OpenAI | ga | 2027-09-21 | yes | openai_chat | azure | 400K | yes | yes |
| 11 | `gpt-6-luna` | OpenAI | ga | – | no | openai_chat | azure | 1.1M | yes | yes |
| 12 | `FW-Kimi-K3` | Fireworks | ga | – | yes | openai_chat | fireworks | 1M | yes | yes |
| 13 | `FW-GLM-5.3` | Fireworks | ga | – | yes | openai_chat | fireworks | 1M | yes | no |
| 14 | `DeepSeek-V4-Pro` | DeepSeek | ga | 2028-02-20 | no | openai_chat | azure | 1M | yes | no |
| 15 | `claude-haiku-5-5` | Anthropic | ga | – | no | anthropic_messages | azure | 1M | yes | yes |
| 16 | `MAI-Thinking-1` | Microsoft | preview | – | no | mai_chat | azure | 256K | yes | no |
| 17 | `gpt-5.6-terra` | OpenAI | ga | 2028-01-11 | yes | openai_chat | azure | 1.1M | yes | yes |
| 18 | `gpt-5.5` | OpenAI | ga | 2027-10-26 | yes | openai_chat | azure | 1.1M | yes | yes |
| 19 | `grok-4.7` | xAI | ga | – | no | openai_chat | azure | 500K | yes | yes |
| 20 | `claude-sonnet-5-5` | Anthropic | ga | – | no | anthropic_messages | azure | 1M | yes | yes |
| 21 | `gpt-6-astra` | OpenAI | ga | – | yes | openai_chat | azure | 1.1M | yes | yes |
| 22 | `gpt-6.1-sol` | OpenAI | ga | – | no | openai_chat | azure | 1.1M | no | yes |
| 23 | `claude-opus-5-5` | Anthropic | ga | – | no | anthropic_messages | azure | 1M | yes | unknown |

