# Foundry IQ and AI gateway sample (Phase 8)

Shape reference for the Phase 8 rules (`FND-IQ-*`, `FND-GW-*`). All names, IDs
and tenants are fake. See `docs/iq-gateway.md` for the rule behaviour.

## Contents

- `azure.yaml`: an `azure.ai.connection` service that targets a Foundry IQ
  knowledge base MCP endpoint (`.../knowledgebases/hr-kb/mcp?api-version=2026-08-01-preview`)
  with `authType: ProjectManagedIdentity`, `category: RemoteTool` and
  `audience: https://search.azure.com/` (Learn `foundry-iq-connect`, read
  2026-10-18). It carries no header block: Learn sets
  `x-ms-query-source-authorization` on the agent's MCP tool `headers` with a
  structured-input placeholder such as `{{search_auth_token}}`, which Foundry
  Doctor does not read.
- `infra/main.bicep`: an Azure AI Search service, a Foundry (AIServices)
  account with an embedding deployment, Application Insights, and an API
  Management gateway (`2024-05-01`) with a system-assigned identity, an API
  that fronts the account, a policy fragment, an API policy and a
  Cognitive Services OpenAI User role assignment for the gateway identity.
- `infra/policies/foundry-chat.xml`: well-formed inbound policy with
  `<base/>`, `validate-jwt` (single-tenant OpenID configuration, audience),
  `include-fragment` and `authentication-managed-identity`.
- `infra/policies/token-governance.xml`: the `token-governance` fragment with
  `llm-token-limit` and `llm-emit-token-metric`.

Knowledge bases and knowledge sources are Search data-plane objects and are
not created by the template. Create them with the Search REST API, then run
`foundry-doctor doctor --iq-metadata` with Reader (or Search Service
Contributor) on the search service and role-based access enabled on the
service; without `--iq-metadata`, or with `--local`, the IQ rules that need the
data plane are skipped.

## Expected results (offline, `doctor --local`)

| Rule | Expected | Why |
|---|---|---|
| FND-GW-001 | pass | `validate-jwt` with audience and a tenant-specific OpenID configuration, inherited by every operation. |
| FND-GW-002 | pass | `llm-token-limit` and `llm-emit-token-metric` resolved from the in-template fragment. |
| FND-GW-003 | pass | The API has a literal `serviceUrl`. |
| FND-GW-004 | pass | One API path. |
| FND-GW-005 | pass | `authentication-managed-identity` with the system identity; the role assignment references the APIM identity and the Foundry account. |
| FND-GW-006 | uncertain, or a finding if the compiled `loggerId` expression is not resolved to the `appinsights` logger | Application Insights diagnostic with `metrics: true`; custom metrics with dimensions is a portal setting that cannot be read. |
| FND-IQ-008 | skipped | The azure.yaml connection leg passes; the data-plane legs were not read. |
| FND-IQ-001..007, 009..011 | skipped | Data-plane metadata not read (`--iq-metadata` not given). |
| FND-IQ-012 | skipped | The semantic leg needs the knowledge-source plane. |

These expectations were derived by reading the rule code; they have not been
produced by running the binary in this fix loop (no Go toolchain).
