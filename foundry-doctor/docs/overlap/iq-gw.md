# Overlap fragment: IQ (Foundry IQ / Azure AI Search) and GW (APIM AI gateway)

Research-state semantics are defined in `research-status.md`; `-` does not mean searched-no-match
unless this fragment names the searched catalogue and revision.

Researched 2026-10-04. Versions verified: Search data plane 2026-04-01 (stable) and 2026-08-01-preview
(also 2026-05-01-preview in the spec); Search management 2025-05-01 and 2026-03-01-preview (spec also has
2026-09-01-preview); API Management ARM 2024-05-01 and 2025-09-01-preview. All property names below were read
in the REST specs under `refs/rest-api-specs` or the Learn source files named in each rule.

## Rules

| ID | Decision | Coverage | PSRule | Azure Policy | Defender | Advisor | Status |
|---|---|---|---|---|---|---|---|
| FND-IQ-001 | native | none | - | - | - | - | verified |
| FND-IQ-002 | native | none | - | - | - | - | verified |
| FND-IQ-003 | native | none | - | - | - | - | verified |
| FND-IQ-004 | native | none | - | - | - | - | verified |
| FND-IQ-005 | native | none | - | - | - | - | verified |
| FND-IQ-006 | native | none | - | - | - | - | verified |
| FND-IQ-007 | native | none | - | - | - | - | verified |
| FND-IQ-008 | adapt | partial | Azure.Search.ManagedIdentity | 6300012e-e9a4-4649-b41f-a85f5c43be91 | - | - | verified (basis security) |
| FND-IQ-009 | native | none | - | - | - | - | verified (basis security) |
| FND-IQ-010 | native | none | - | - | - | - | verified |
| FND-IQ-011 | native | none | - | - | - | - | verified |
| FND-IQ-012 | native | none | - | - | - | - | verified |
| FND-GW-001 | native | none | - | d5448c98-e503-4fdd-bcd2-784960c00d04 (base policy, context only) | - | - | verified (basis security) |
| FND-GW-002 | native | none | - | - | - | - | product-opinion |
| FND-GW-003 | native | none | - | - | - | - | verified (basis reliability) |
| FND-GW-004 | native | none | - | - | - | - | verified |
| FND-GW-005 | native | partial | Azure.APIM.ManagedIdentity | c15dcc82-b93c-4dcb-9332-fbf121685b54 | - | - | verified (basis security) |
| FND-GW-006 | native | none | - | - | - | - | verified (basis operations) |

Checkov: no ID maps to any IQ or GW rule (CKV_AZURE_173, 174, 208, 209 cover APIM TLS, APIM public access and
Search replica counts only). Bicep linter, Defender for Cloud, Advisor: no equivalent found. Counts: 17 verified,
1 product-opinion, 0 dropped, 0 proposed. Decisions: 16 native (2 with partial coverage), 1 adapt (IQ-008).

## Changed to product-opinion or dropped

- FND-GW-002 product-opinion: the policy names (`llm-token-limit`, `llm-emit-token-metric`) and attributes are
  verified platform facts, but no source says a gateway must have them. Presence is a governance preference
  (basis `opinion`, `cost`). Invalid configuration of a present policy is a documented constraint inside the same rule.
- No rule dropped.

## Platform basis versus security basis

Rules IQ-008, IQ-009, GW-001 and GW-005 are recommendations (docs say role-based access is "recommended"; the
JWT "tenant" and "when required" conditions have no platform definition), so they use basis `security` with
warning in dev, not `platform`.

## Unverified

- Legacy APIM policy names `azure-openai-token-limit` and `azure-openai-emit-token-metric`: absent from the current
  Learn policy reference (only `azure-openai-semantic-cache-lookup` appears, in the Application Insights
  dependency list). Tried: grep over `refs/azure-docs/articles/api-management`. GW-002 matches only the `llm-*` names.
- ARM representation of managed-identity authorization credentials on a backend entity: documented in the portal
  (`backends.md`) but `BackendCredentialsContract` in 2025-09-01-preview has only certificate, certificateIds, query,
  header, authorization. GW-005 skips backend-credential-only APIs. Tried: the openapi.json for 2024-05-01 and 2025-09-01-preview.
- `ingestionPermissionOptions` label value: Learn says `sensitivityLabel`, the 2026-08-01-preview spec enum is
  `sensitivityLabels` (also `userIds`, `groupIds`, `rbacScope`). The spec is used (IQ-009).
- Knowledge source kind for SQL: Learn calls it "Azure SQL"; the spec enum value is `indexedSql` (IQ-007).
- Whether the service rejects a non-filterable base filter field at knowledge source creation or at retrieve time
  (IQ-001), and whether a Search index missing semantic config fails at creation (IQ-002): the docs state the
  requirement, not the failure point.
- ARM name of the API Management diagnostic-setting log category "Logs related to generative AI gateway" (GW-006).
- Hierarchical namespace immutability after account creation (IQ-010): not asserted.
- `pageOverlapLength` less than half of `maximumPageLength` (IQ-006) appears in the chunking article only, not in the skill reference.
- Hybrid weights as a Foundry IQ setting: no knowledge base or knowledge source property carries them in the
  2026-08-01-preview spec; only the Search query `vectorQueries[].weight` (positive number) exists (IQ-006).
- Overlapping route prefixes and cross-hostname path reuse in APIM (GW-004): not documented; only exact duplicates fail.
- The `azd` and `azure.ai.*` extension compatibility fields are empty on purpose: these rules read Search and
  APIM resources, not `azure.yaml`.
- Learn pages carry dates up to 2026-08 and the Search tier/region tables (IQ-005) change; re-verify at Phase 8 start.

## PRD ambiguities

- FND-IQ-001 "claims": interpreted as the identity (user/group) field used for security-filter trimming.
- FND-IQ-005 "retrieval mode": interpreted as knowledge base `retrievalReasoningEffort` plus `outputMode`.
- FND-IQ-009 "when required": no platform definition; needs an ADR for where the project declares the requirement.
- FND-GW-001 "tenant": validate-jwt has no tenant attribute; three accepted derivations are product decisions.
- FND-GW-004 "route paths": interpreted as API `path` (URL suffix); operation `urlTemplate` uniqueness is not sourced.

## Proposed new rules (fragment only)

IQ:
1. Knowledge source retrieval-readiness advisory (deployment feasibility): index `description` present (up to 4000
   characters, used by the planner at low and medium effort), citation fields, a vectorizer on the vector profile
   (documented as critical when vectors are used), and no scoring-profile assumption (agentic retrieve ignores
   scoring profiles). Source: `agentic-retrieval-how-to-create-index.md`, `agentic-knowledge-source-how-to-search-index.md`.
2. Model and API lifecycle: planning model marked deprecated in the supported-model table (gpt-4o, gpt-4.1 families)
   and preview API version in use for a prod profile (supportability, schema lifecycle).
3. Semantic ranker and agentic retrieval free-plan quota exposure in prod (cost visibility): `semanticSearch` or
   `knowledgeRetrieval` still `free` on a service serving production traffic (billing and payment-required errors after the allowance).

GW:
1. Backend resilience for model endpoints: circuit breaker rule on the Foundry backend and Retry-After handling
   (`circuitBreaker`, `acceptRetryAfter`; the docs warn about 429 with very large Retry-After) plus pool use for
   multi-region deployments (release readiness, reliability). Source: `backends.md`.
2. Gateway tier supportability: `llm-token-limit` is not available on the Consumption tier and the backend circuit
   breaker is not supported there; flag AI gateway use on that tier (deployment feasibility).
3. Secret handling at the gateway: named values holding API keys for model backends should be Key Vault backed; no
   `api-key` forwarded to clients (security; builds on the existing APIM policy `NamedValueSecretsInKV`).
