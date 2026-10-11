# Routing project: Microsoft-Decision-1 router

A model router for Microsoft Foundry that takes the same input as Foundry Model Router
and returns the same output, but makes its choice with **Microsoft-Decision-1**:

1. **Stage 1 (deterministic).** Starting from the chosen catalog (`compatibility`: `old` is
   Model Router's pool without deprecated models, `new` is the latest models), filters remove
   models by lifecycle, the operator allow-list, the request's `routing_constraints` (model
   subset, providers, capabilities, region, residency, context size), what the request itself
   needs (tools, images, JSON schema, streaming), and finally the routing mode's price band.
2. **Stage 2 (Decision-1).** The conversation, the routing instructions and the candidates
   with their descriptions go to Decision-1 as one `choice` question. Its probabilities give
   a ranked list of models.
3. **Select.** The model with the highest probability is called first. If its probability is
   below `ROUTER_LOW_CONFIDENCE_THRESHOLD` (default 0.5), the second-ranked model is called
   first and the top model second.
4. **Log.** The candidates, probabilities, ranking and execution order are written to the
   decision log.
5. **Generate.** The first model in that order answers the request. If that call fails, the
   next model in the order is tried; every model tried is a stage-1 candidate.

There is no other router behind Decision-1. If it cannot answer, the request fails.

| Folder | Contents |
|---|---|
| `decision-router/` | The service, the comparison harness, tests and deployment manifests |
| `dataset-v31/` | 40,000 prompts used to compare this router with Foundry Model Router |
| `docs/` | Architecture and the Decision-1 contract, stage-1 filtering, the model catalogs, authentication and Foundry Agent Service, option-order testing |

## Quick start

```bash
cd decision-router
pip install -r requirements-dev.txt
pytest -q                                    # no credentials needed

export FOUNDRY_ENDPOINT=https://<resource>.services.ai.azure.com
export ROUTER_API_KEYS=<a-long-random-key>   # or ROUTER_ENTRA_TENANT_ID + ROUTER_ENTRA_AUDIENCE
python -m decision_router                    # serves on :5001
```

```bash
curl -s localhost:5001/v1/chat/completions -H 'content-type: application/json' -H "api-key: $ROUTER_API_KEYS" -d '{
  "messages": [{"role": "user", "content": "Summarise the attached contract clause."}],
  "routing_mode": "cost", "max_completion_tokens": 400}'
```

Claude models are called through the Anthropic Messages API and their answers are translated
back, so every response has the same chat-completions shape.

The response body is the chosen model's chat completion, unchanged. Its `model` field names
the model that answered, as with Model Router. Routing details are in the
`x-router-ranking`, `x-router-low-confidence` and `x-router-served-model` headers and in the
decision log.

## Comparing with Foundry Model Router

`decision-router/scripts/compare_with_model_router.py` sends each dataset prompt to this
router and to a Model Router deployment, with the same body and the same model
deployments, and reports how often they choose the same model. It also reports latency,
cost and which models each side picked in each mode. With `--shuffle-options` it also measures
whether Decision-1's answer depends on the order the models are listed in
(`docs/OPTION_ORDER_TESTING.md`). See `docs/ARCHITECTURE.md`.

## What changed from v4

- Removed: the TF-IDF classifier and its weights, its training and retraining scripts, the
  RouterBench ingest, the BYOM container, the SemIf comparison inputs, the circuit breaker,
  the managed-router fallback, and the shadow sampler.
- Dataset: 1,000 rows were labelled with a model their own budget rail forbids
  (`extra_high` → `deepseek-v4-flash`). The generator now applies the deepseek override only
  where the rail allows it. Those rows are relabelled `gpt-5.5`; every prompt and ID is
  unchanged. The old Jev query file, which used the wrong payload shape, is replaced by
  `decision-router/scripts/export_decision1_requests.py`.
