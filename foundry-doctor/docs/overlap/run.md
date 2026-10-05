# Overlap fragment: RUN (FND-RUN-001..008)

Researched 2026-10-04 against the local clones listed in `docs/development/phase-0-research-brief.md`, plus a few raw
GitHub files (Azure SDK for Go azquery source). ARM property names and enums come from
`Azure/azure-rest-api-specs` (Cognitive Services 2026-09-01 stable, Monitor metrics 2024-02-01, Authorization 2022-04-01,
Cosmos DB 2025-10-15, Private DNS 2018-09-01, Search data plane 2026-04-01). Policy GUIDs were read from the definition
files. Defender and Advisor recommendation IDs were not available in any clone and are left empty.

## Decision table

| ID | Decision | Coverage | PSRule | Azure Policy | Microsoft azd doctor check | Status |
|---|---|---|---|---|---|---|
| FND-RUN-001 | adapt | partial | none | none | remote.project-storage-rbac (Storage only) | verified |
| FND-RUN-002 | native | partial | Azure.AI.PrivateEndpoints (existence only) | c4bc6f10-cb41-49eb-b000-d5ab82e2a091 (config, DINE) | none | verified |
| FND-RUN-003 | native | partial | none | none | remote.connections (name match only) | verified |
| FND-RUN-004 | native | none | none | none | none (model-deployments check removed, PR 8394) | verified |
| FND-RUN-005 | adapt | partial | none | none | remote.agent-status, remote.foundry-endpoint, remote.connections, local.toolboxes | product-opinion |
| FND-RUN-006 | native | none | Azure.Search.IndexSLA, QuerySLA (config only) | none | none | verified |
| FND-RUN-007 | native | partial | none | 1b4d1c4e, b4330a05, 55d1f543, 08ba64b8 (config only) | none | verified |
| FND-RUN-008 | adapt | partial | none | none | `azd provision --preview` (idea) | product-opinion |

Counts: 6 verified, 2 product-opinion, 0 dropped, 0 proposed. Decisions: native 5 (002, 003, 004, 006, 007),
adapt 3 (001, 005, 008), reuse 0, wrap 0, drop 0. All implementation owners are `native`.

## Probe and permission summary

| ID | Read-only probe (verb, path, api-version) | Role or permission | Vantage point | Metadata read |
|---|---|---|---|---|
| 001 | GET project, project capabilityHosts, project/account connections (2026-09-01); GET `/{scope}/providers/Microsoft.Authorization/roleAssignments?$filter=principalId eq {id}` (2022-04-01); GET Cosmos `databaseAccounts/{a}/sqlRoleAssignments` (2025-10-15) | role assignment read at bound scopes; ARM read (action names not verified) | control plane, any | IDs, role definition IDs, principal IDs |
| 002 | OS DNS resolution of service FQDNs; GET `Microsoft.Network/privateDnsZones/{z}/virtualNetworkLinks` (2018-09-01) | none for DNS; ARM read for zones | inside the VNet only; Skipped elsewhere, never Pass | DNS answers, link state |
| 003 | GET project, capabilityHosts (project and account), connections (2026-09-01) | ARM read (action names not verified) | control plane, any | provisioningState, connection names/error |
| 004 | GET `accounts/{a}/deployments` (2026-09-01); GET `/{accountId}/providers/Microsoft.Insights/metrics` AzureOpenAIRequests (2024-02-01) | ARM read; metrics read (role not verified) | control plane, any | deployment state, request counts by StatusCode |
| 005 | GET `{projectEndpoint}/agents?limit=1`, `/agents/{n}/versions/{v}`, `/connections` (api-version v1) | Reader-level per azd source; Foundry User for developers | project endpoint reachability | agent status, connection names |
| 006 | GET `/indexes('{i}')`, `/indexes('{i}')/search.stats`, `/indexers('{x}')/search.status` (2026-04-01) | Search Service Contributor (object management) | needs network reach to the search endpoint; Skipped when private and outside VNet | schema, document count, indexer status |
| 007 | GET `{resource}/providers/Microsoft.Insights/diagnosticSettings` (2021-05-01-preview); metrics (2024-02-01); POST `/workspaces/{id}/query` (count only) | diagnostic setting read; workspace query (role not verified) | control plane and query endpoint | setting names, categories, row counts |
| 008 | placeholder; candidate ARM GET or `POST .../deployments/{n}/whatIf` (2025-04-01) | unresolved | control plane | n/a |

## azd ai agent doctor mapping

Source: `azure-dev/cli/azd/extensions/azure.ai.agents` version 1.0.0-beta.18, `internal/cmd/doctor`. Check IDs:
local.grpc-extension, local.azure-yaml, local.environment-selected, local.manual-env-vars, local.agent-service-detected,
local.project-endpoint-set, local.agent-yaml-valid, local.toolboxes, remote.auth, remote.foundry-endpoint, remote.rbac,
remote.project-storage-rbac, remote.connections, remote.agent-status. Statuses are pass, warn, fail, skip.

- RUN-001: partially covered by remote.project-storage-rbac (Storage connections bound in the capability host
  `storageConnections`, direct or inherited assignments, NotDataActions handling; keys, SAS and service principal connections skipped).
  Search and Cosmos are not covered. remote.rbac checks the developer's own role on the project, which is not this rule.
- RUN-003: remote.connections only checks that configured connection names exist.
- RUN-005: remote.agent-status, remote.foundry-endpoint, remote.connections and local.toolboxes cover the same evidence for azd hosted agents.
- RUN-002, 004, 006, 007: no azd doctor equivalent. A model-deployments check was removed (CHANGELOG, PR 8394) for false failures.
- Wrapping azd doctor was not chosen: it is tied to azure.yaml and the azd environment, prints human output, and the extension is beta.

## Rules changed to product-opinion

- FND-RUN-005: the agents API (api-version `v1`) has no REST spec in the clones and no source describes a side-effect-free
  tool or MCP probe. The rule is restricted to agent version status and connection metadata, never invokes tools.
- FND-RUN-008: phase future placeholder; drift scope and noise handling are design decisions with no primary source.

## Unverified

- Built-in role names that carry the exact actions for: ARM reads of projects, capabilityHosts and connections (RUN-003);
  Azure Monitor metrics read (RUN-004); diagnostic settings read and Log Analytics query (RUN-007). Role contents are not in the clones.
- Log Analytics query REST spec: not in the clone and the raw path guesses returned 404. Path `/workspaces/{id}/query`
  verified from the azquery Go SDK source only; spec version 2022-10-27 from its autorest.md.
- Diagnostic settings have only preview api-versions (2021-05-01-preview) in the spec clone.
- Foundry project data-plane agents API (`v1`): no spec in the clones; azd source only (RUN-005).
- Per-metric dimension list for AzureOpenAIRequests; the docs list StatusCode and ModelDeploymentName for the namespace.
- Which Search role covers `search.stats` and `docs/$count`; the RBAC table groups operations coarsely.
- Cosmos DB built-in data-plane role GUIDs and Reader role action contents.
- Meaning of ConnectionPropertiesV2 `peStatus` Inactive for health, and whether `error` is populated for every bad connection.
- What counts as a private IP for RUN-002 (the docs say "private IP address").
- Defender for Cloud and Advisor recommendation IDs for all RUN rules.
- Whether the capability settings (account-level) flow changes which roles the project identity needs; the docs say Azure AI Search needs no caller role there.

## Proposed new rules (fragment only)

- Deployment feasibility: model quota and region availability preflight for each declared deployment (reads the
  deployment SKU and capacity from the template and compares with available quota through read-only ARM usage calls). Source needed before allocation.
- Supportability: Foundry account and dependency resources in a supported region and not in a preview-only API combination,
  using the version of the stable spec in `compatibility.apiVersions` (schema lifecycle).
- Cost visibility: provisioned-throughput (PTU) utilization well below 100 percent for a sustained window, via
  AzureOpenAIProvisionedManagedUtilizationV2, as a right-sizing hint (product opinion).
