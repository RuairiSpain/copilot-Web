# Production-readiness review

A two-reviewer review of the schema, validation rules and the Azure services the extension
will deploy. **Foundry** is the Foundry architect and lead developer; **Infra** is the Azure
infrastructure specialist. Phase 1 generates no Bicep or CLI commands yet, so the generator
items below are requirements for Phases 2 to 6, not defects in shipped code.

Status: `fixed` (in this change), `backlog` (planned), `decision` (needs a maintainer choice).
Agreement: `both`, or the single reviewer who raised it.

## Priority 0: blocks a safe release

| # | Finding | Agreement | Status |
| --- | --- | --- | --- |
| 1 | Search SKU was spelled `storage_optimised_*`; ARM requires `storage_optimized_*`. Deployment would fail. | both | fixed |
| 2 | Service firewalls (Storage, Key Vault, Search) reject RFC 1918 ranges in IP rules, and `0.0.0.0/0` defeats the control. The old example used `10.0.0.0/8`. Now `XF121`. | both | fixed |
| 3 | Private "standard agent setup" needs bring-your-own Cosmos DB, Storage and AI Search, an agent subnet delegated to `Microsoft.App/environments` (/27 or larger, one per Foundry resource), one region for every workspace resource, a private endpoint and DNS zone per dependency (including `privatelink.documents.azure.com`), and a capability host. The schema now has `cosmos`, `agentService.setup`, an agent subnet and VNet address space (`XF127`, `XF128`, `XF131`); the graph adds a capability host per project; private-mode region mismatch is an error (`XF120`). Bicep for the capability host is Phase 2. | Foundry; Infra agrees | fixed (schema); Phase 2 generates |
| 4 | RBAC is not defined. Needed: a table mapping `admins/developers/consumers/operators` to built-in roles (Azure AI Account Owner / Project Manager / User, not Owner), resource roles for each managed identity (Search Index Data Contributor, Storage Blob Data Contributor, Key Vault Secrets User, AcrPull, Cognitive Services OpenAI User, Cosmos roles), deterministic `guid()` assignment names, `principalType`, and name-to-object-ID resolution that fails on ambiguous display names. | both | backlog (Phase 2) |
| 5 | Generated Bicep must: pin API versions per resource; set `allowProjectManagement`, `customSubDomainName`, `disableLocalAuth`, `publicNetworkAccess` and deny-by-default `networkAcls` on the Foundry account; use `@secure()` and never output keys; deploy model deployments serially (`@batchSize(1)`); and pass `bicep build`, `what-if` and a policy linter (PSRule for Azure) in CI. | both | backlog (Phase 2) |
| 6 | *(Superseded: the `redis` section was removed from the schema; see `roadmap.md`.)* Azure Cache for Redis can no longer be created (new customers since 2026-04-01, existing since 2026-10-01; retires 2028-09-30). Creation is now `XF125`. Azure Managed Redis uses `Microsoft.Cache/redisEnterprise` with a database child and SKU names that carry a size (for example `Balanced_B5`), so `redis.sku` plus `capacity` cannot be mapped. | Infra; Foundry agrees | partly fixed; `redis.size` backlog |

## Priority 1: security and correctness gaps

| # | Finding | Agreement | Status |
| --- | --- | --- | --- |
| 7 | Gateway SKUs in private mode: Consumption supports neither private endpoints nor VNet integration (`XF124` error); BasicV2 has no outbound VNet integration (`XF124` warning). v2 tiers are isolated with a private endpoint and disabled public access, not internal VNet mode, so `gateway.security.internalOnly` must map to that. `PremiumV2` added. | Infra; Foundry agrees | fixed (SKUs); mapping backlog |
| 8 | *(Superseded for Service Bus and Container Registry, which were removed from the schema; see `roadmap.md`.)* Private endpoints require Premium for Service Bus and Container Registry; the schema now exposes both (`events.sku`, `runtime.registry.sku`) and selects Premium automatically in private mode. Log Analytics and Application Insights need Azure Monitor Private Link Scope to keep ingestion private. Generator must derive these and warn about cost. | Infra; Foundry agrees | fixed (schema, `XF129`); Phase 2 generates, AMPLS backlog |
| 9 | Identity model: Foundry accounts and projects use a system-assigned identity, but the schema offers only a user-assigned one. `managedIdentity.type` now offers `systemAssigned` and `systemAssignedAndUserAssigned`; Foundry always uses system-assigned (`XF130`). | Foundry; Infra agrees | fixed |
| 10 | Foundry IQ schema versus the Azure AI Search knowledge-base API (2025-11-01-preview): real source kinds are `searchIndex`, `azureBlob`, `indexedOneLake`, `web` (plus SharePoint and preview SQL/file kinds). `adls` is not a kind (it is `azureBlob` over ADLS Gen2) and `custom` has no equivalent. A knowledge base needs an LLM deployment for query planning and supports `retrievalReasoningEffort` and `outputMode`; our `vectorWeight`/`keywordWeight`, `routing.threshold` have no confirmed API mapping. Verify each field against the API before Phase 4 and add contract tests. | Foundry; Infra agrees | backlog (Phase 4) |
| 11 | Outbound URLs must be https and must not target loopback or link-local hosts (instance metadata). `XF122`. | both | fixed |
| 12 | Search sizing: per-SKU replica and partition limits, 36 search units, no private endpoint on the free tier, one replica has no SLA in prod. `XF123`. | Infra; Foundry agrees | fixed |
| 13 | `governance.resourceLocks` defaults to true, which makes `azd down` fail unless locks are removed first. Key Vault purge protection and soft-deleted Foundry accounts block name reuse. Generator needs a deterministic name suffix, and the down hook must remove locks and purge. | both | backlog (Phase 6) |
| 14 | Data residency: `GlobalStandard` (the default deployment SKU) can process data in any region. With `governance.dataResidency` set, Global SKUs are an error and implicit deployments use `DataZoneStandard`. `XF126`. | Foundry; Infra agrees | fixed |
| 15 | Gateway telemetry: logging prompts or completions, and tracking by user object ID, put personal data in Log Analytics. Require an explicit opt-in, pseudonymise user IDs, document retention. The email-domain claim is mutable; restrict by tenant ID (`tid`) rather than `allowedDomains` alone. | Foundry; Infra agrees on prompt logging | backlog (Phase 5) |
| 16 | Add an `xfoundry doctor` preflight: deployer roles (Contributor plus RBAC Administrator at the resource group; Resource Policy Contributor for policy assignments; Cost Management Contributor for budgets; Graph read for name resolution), resource-provider registration, model quota, region and SKU availability, soft-deleted names. | Infra; Foundry agrees | backlog (Phase 2/3) |

## Priority 2: release and maintenance

| # | Finding | Agreement | Status |
| --- | --- | --- | --- |
| 17 | azd extensions run as a separate process over gRPC (extension framework, `requiredVersions.extensions`). Decision: all extension code is Go, so it can use the azd extension SDK and ship as one static binary per platform. Phase 1 has been ported from Python. azd's root schema allows unknown keys, so `x-foundry` is accepted. | Foundry; Infra agrees on packaging and signing | decided: Go |
| 18 | Schema `$id` points at `example.org`; publish a real URL and register with SchemaStore. Rename the Foundry "hub" in docs to avoid confusion with an Azure hub VNet. `existingVnetResourceId` needs subnet IDs; a generated VNet now takes an `addressSpace`. `$id` and the hub naming remain. | Infra; Foundry agrees | partly fixed |
| 19 | Default `versionUpgradeOption` of `OnceNewDefaultVersionAvailable` changed model behaviour without a release; `NoAutoUpgrade` is now the default. | Foundry only (Infra neutral) | fixed |
| 20 | Default `Standard_ZRS` is unavailable in some regions; fall back to LRS with a warning. | Infra only (Foundry neutral) | backlog |
| 21 | Open-source hygiene: LICENSE, SECURITY.md, CODEOWNERS, Dependabot, CodeQL, actions pinned by SHA, `pip-audit`, SBOM and signed releases. | Infra; Foundry agrees | backlog |

## Evidence

Checked against Microsoft documentation and the azd schema while preparing this review:
Search SKU names; the Foundry account `projects` child type and required properties; Redis
retirement dates and the need for a recent `redisEnterprise` API version; APIM v2 private
endpoint and VNet integration support; standard agent private-networking prerequisites;
Foundry RBAC role names; Storage firewall behaviour for private ranges; Service Bus and
Container Registry private-endpoint tiers; the azd extension framework; azd's open root schema.

Not independently confirmed (verify at generation time): exact ARM API versions other than
Redis Enterprise, APIM policy names such as `llm-token-limit`, Container Apps regional
availability, and the exact Azure Managed Redis size list.
