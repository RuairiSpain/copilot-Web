# Foundry Doctor samples

Fixtures for exercising the offline doctor. All values are fake; none contain
secrets or secret-shaped strings.

| Sample | Purpose | Expected result |
|---|---|---|
| `good/` | Valid `azure.yaml` with project, deployment and hosted agent | No findings |
| `bad/` | Duplicate `name:` key, unresolved `${EXAMPLE_MISSING_VAR}`, `uses:` typo | Findings (exit 1) |
| `good-private/` | Realistic private-networked Foundry + capability-settings sample with policy config and Bicep-backed infra | No error/warning findings from verified deterministic rules; info-only posture findings may remain |
| `bad-private/` | Intentionally broken variant of `good-private/` with targeted reliability and security regressions | Findings for the injected defects with source locations under `infra/` |
| `azure-yaml-only/` | `azure.yaml` with no infra directory | Infra checks reported as skipped, not passed |
| `bicep-backed/` | `azure.yaml` plus `infra/main.bicep` and a module (Bicep 0.48.1 valid) | Bicep analysed; no findings expected |
| `cost-fixed/` | Bicep sample with Search, APIM, Cosmos RU/s and model deployments for the cost command | Advisory monthly estimate with explicit exclusions/unsupported items |
| `assess-dev/` | Dev-profile WAF assessment fixture with local-only evidence | Owner summary emphasises unassessed scope and recommendations |
| `assess-test/` | Test-profile WAF assessment fixture with invalid YAML and unresolved references | Evidence pack shows failures and skipped controls |
| `assess-prod/` | Prod-profile WAF assessment fixture with Bicep-backed infrastructure and DR policy declaration | Developer and HTML views show mandatory and costly recommendations |
| `llm/` | Redacted advisory LLM request/response examples | `projected-request.json` is safe to send; `owner-response-bad.json` is expected to be rejected |
| `iq-gateway/` | Minimal Foundry IQ MCP connection sample for Phase 8 docs | Shape reference only; rule tests supply deterministic IQ/gateway metadata |

Each sample's `agents/assistant/` directory holds a `.gitkeep` placeholder.

Phase 5 graph examples live under `samples/graph/`.
