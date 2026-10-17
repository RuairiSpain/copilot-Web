# Azure read-only permissions matrix (Phase 2)

Source of truth for the allow-list is `internal/azure/transport.go`
(`AllowedOperations()`); a test fails if a verb, path or template drifts from
ADR-006. Only GET and the POST-read operations below are reachable. What-if
runs only with explicit opt-in.

"Verified" = api-version taken from `rules/catalog/dep/*.yaml`.
"VERIFIED" (capitals) = checked against a primary source (Azure REST API specs
or Microsoft Learn) by the Phase 2 gate reviewers on 2026-10-17.
"REFUTED" = the primary source contradicts the earlier value; the row shows the
replacement. "UNVERIFIED" = no primary source seen; confirm before release
(open issue). `learn.microsoft.com` and `azure-rest-api-specs` were not
reachable from the authoring sandbox, so rows not named above keep their
earlier label. "Reader suffices" is a hypothesis: tests only prove that
a 403 degrades to a typed `capability unavailable` error, not that built-in
Reader grants the action.

| Capability | Method | Resource | api-version | RBAC action (403 text) | Source |
|---|---|---|---|---|---|
| Context | GET | subscriptions/{id} | 2022-12-01 | Microsoft.Resources/subscriptions/read | verified |
| Providers | GET | providers/{ns} | 2025-04-01 | Microsoft.Resources/subscriptions/providers/read | verified |
| Inventory | GET | {scope}/resources, resourcegroups/{rg} | 2025-04-01 | Microsoft.Resources/subscriptions/resources/read; Microsoft.Resources/subscriptions/resourceGroups/read; Microsoft.Resources/subscriptions/resourcegroups/resources/read | REFUTED for 2022-12-01 (the spec does not publish `Microsoft.Resources/resources` at 2022-12-01); replaced by 2025-04-01 (resources.list and resourcegroup.get). Action names `.../subscriptions/resourcegroups/resources/read` VERIFIED; `.../resourceGroups/read` is the standard resource-group read action, not separately checked. |
| Locks | GET | {scope}/providers/Microsoft.Authorization/locks | 2020-05-01 | Microsoft.Authorization/locks/read | verified |
| Subnet | GET | virtualNetworks/{v}/subnets/{s} | 2025-05-01 | Microsoft.Network/virtualNetworks/subnets/read | verified |
| Role assignments | GET | roleAssignments (assignedTo filter) | 2022-04-01 | Microsoft.Authorization/roleAssignments/read | api-version verified. The `assignedTo('{oid}')` `$filter` is undocumented in the 2022-04-01 spec (it works in Azure CLI); treat the filter as unspecified behaviour. |
| Role definitions | GET | roleDefinitions/{id} | 2022-04-01 | Microsoft.Authorization/roleDefinitions/read | VERIFIED (2022-04-01) |
| Deny assignments | GET | denyAssignments | 2022-04-01 | Microsoft.Authorization/denyAssignments/read | VERIFIED (2022-04-01; `atScope()` filter documented) |
| Models | GET | locations/{l}/models | 2026-09-01 | Microsoft.CognitiveServices/locations/models/read | verified |
| Quota | GET | locations/{l}/usages | 2026-09-01 | Microsoft.CognitiveServices/locations/usages/read | verified |
| Policy assignments | GET | policyAssignments | 2026-07-01 | Microsoft.Authorization/policyAssignments/read | verified |
| Policy restrictions | POST | PolicyInsights/checkPolicyRestrictions (body: resourceDetails, pendingFields, includeAuditEffect) | 2024-10-01 | Microsoft.PolicyInsights/checkPolicyRestrictions/read | verified (catalogue FND-DEP-009; Microsoft Learn confirms the POST shape and payload. Learn does not say whether the evaluation honours exemptions, so clean results stay uncertain and only explicit `deny` is reported as a likely denial.) |
| Name checks | POST | CognitiveServices checkDomainAvailability (subscription level, no location) 2026-09-01; Storage 2026-09-01; KeyVault 2026-05-15; Search 2025-05-01; APIM 2024-05-01; Container Registry 2025-11-01 | as listed | `Microsoft.KeyVault/checkNameAvailability/read`, `Microsoft.Storage/checknameavailability/read`, `Microsoft.ContainerRegistry/checkNameAvailability/read`, `Microsoft.ApiManagement/checkNameAvailability/read`; `Microsoft.CognitiveServices/checkDomainAvailability/action`, `Microsoft.Search/checkNameAvailability/action` (action names VERIFIED) | verified (catalogue FND-DEP-007 plus Microsoft Learn ACR `POST /subscriptions/{subscriptionId}/providers/Microsoft.ContainerRegistry/checkNameAvailability`; Cosmos remains outside the allow-list because its documented check is `HEAD /providers/Microsoft.DocumentDB/databaseAccountNames/{accountName}`) |
| Locations | GET | subscriptions/{id}/locations | 2022-12-01 | Microsoft.Resources/subscriptions/locations/read | VERIFIED (api-version FND-DEP-012; action name confirmed) |
| Provider resource types | GET | providers/{ns} (resourceTypes[].locations) | 2025-04-01 | Microsoft.Resources/subscriptions/providers/read | verified; whether locations are names or display names UNVERIFIED (both normalised) |
| Deployment history | GET | resourceGroups/{rg}/providers/Microsoft.Resources/deployments (count only) | 2026-06-01 | Microsoft.Resources/deployments/read | verified (FND-DEP-010; limit 800) |
| Subnet links | GET | subnets/{s}/ServiceAssociationLinks, ResourceNavigationLinks | 2025-05-01 | Microsoft.Network/virtualNetworks/subnets/resourceNavigationLinks/read; Microsoft.Network/virtualNetworks/subnets/serviceAssociationLinks/read | api-version verified (FND-DEP-011); link-read action names corrected to the two names listed (VERIFIED). Not `subnets/read`. |
| Permissions API | GET | {rg or resource}/providers/Microsoft.Authorization/permissions (no subscription scope) | 2022-04-01 | Microsoft.Authorization/permissions/read | verified (FND-DEP-003); evidence only, never proof |
| Soft-deleted | GET | Cognitive deletedAccounts 2026-09-01; KeyVault deletedVaults 2026-05-15; APIM deletedservices 2024-05-01 | as listed | `Microsoft.CognitiveServices/deletedAccounts/read`; `Microsoft.KeyVault/deletedVaults/read`; `Microsoft.ApiManagement/deletedservices/read` | verified; action names VERIFIED |
| What-if (opt-in) | POST | deployments/foundry-doctor-whatif/whatIf | 2026-06-01 | Microsoft.Resources/deployments/whatIf/action | verified (poll path and `deployments/read` UNVERIFIED). Not read-only access: Learn says what-if needs the same permissions as deploying (`Microsoft.Resources/deployments/*` plus write on the template's resources). Use the separate optional role `samples/preflight/preflight-whatif-role.json`; the default role excludes it. |
| Runtime project | GET | accounts/{account}/projects/{project} | 2026-09-01 | Microsoft.CognitiveServices/accounts/projects/read | UNVERIFIED (public Microsoft Learn REST pages exist for the accountmanagement 2025-06-01 project surface, but the exact 2026-09-01 data-plane version used by the adapter remains unverified; code keeps fail-closed behavior and treats unavailable access as skipped.) |
| Runtime capability hosts | GET | accounts/{account}/projects/{project}/capabilityHosts; accounts/{account}/capabilityHosts | 2026-09-01 | Microsoft.CognitiveServices/accounts/projects/capabilityHosts/read; Microsoft.CognitiveServices/accounts/capabilityHosts/read | UNVERIFIED (public Microsoft Learn REST pages exist for the 2025-06-01 capability-host project surface, but the exact 2026-09-01 data-plane version used by the adapter remains unverified; adapter failures degrade to unavailable.) |
| Runtime connections | GET | accounts/{account}/projects/{project}/connections; accounts/{account}/connections | 2026-09-01 | Microsoft.CognitiveServices/accounts/projects/connections/read; Microsoft.CognitiveServices/accounts/connections/read | UNVERIFIED (public Microsoft Learn REST pages exist for the 2025-06-01 connection-list project surface, but the exact 2026-09-01 data-plane version used by the adapter remains unverified; adapter failures degrade to unavailable.) |
| Runtime deployments | GET | accounts/{account}/deployments | 2026-09-01 | Microsoft.CognitiveServices/accounts/deployments/read | UNVERIFIED (Learn documents account deployments for older API versions, not 2026-09-01; code therefore remains fail-closed on probe errors.) |
| Cosmos SQL role assignments | GET | databaseAccounts/{account}/sqlRoleAssignments | 2025-10-15 | Microsoft.DocumentDB/databaseAccounts/sqlRoleAssignments/read | UNVERIFIED (Microsoft Learn currently redirects the REST page to 2026-03-15, so the exact 2025-10-15 Learn surface is not independently confirmable from Learn; code remains read-only and fail-closed.) |
| Private DNS VNet links | GET | privateDnsZones/{zone}/virtualNetworkLinks | 2018-09-01 | Microsoft.Network/privateDnsZones/virtualNetworkLinks/read | UNVERIFIED |
| Azure Monitor metrics | GET | {resource}/providers/Microsoft.Insights/metrics | 2024-02-01 | Microsoft.Insights/metrics/read | UNVERIFIED |
| Diagnostic settings | GET | {resource}/providers/Microsoft.Insights/diagnosticSettings | 2021-05-01-preview | Microsoft.Insights/diagnosticSettings/read | UNVERIFIED |
| Log Analytics aggregate counts | POST | `https://api.loganalytics.io/v1/workspaces/{id}/query` (count-only query body) | service endpoint | workspace query permission; exact action UNVERIFIED | UNVERIFIED |

## Degradation behaviour

- 401/403 never retry; they yield `capability unavailable: <capability> at <path> (missing permission: <action>)`.
- Effective permissions: ABAC conditions are not evaluated (`Unknown`, `ConditionSkipped`); unreadable deny assignments or role definitions give `Unknown` with a reason, never a silent allow.
- No credential: every capability reports unavailable with no network call.
- 429/5xx/transport errors retry up to 3 times, honouring Retry-After (capped 30s); concurrency is bounded (default 4).

## Not implemented / deliberate limits

- Azure Resource Graph (POST outside the ADR-006 allow-list): inventory uses ARM list only.
- Log Analytics queries: no interface in `api.go`.
- Runtime data-plane reads:
  - Foundry project endpoint metadata only (`/agents`, `/agents/{name}/versions/{version}`, `/connections`)
  - Search metadata only (`indexes`, `search.stats`, `indexers/search.status`)
  - No document, prompt, completion, or tool content retrieval is implemented.
- Policy effects are not resolved (policy definition reads are not allow-listed); assignment parameters are never decoded.
- Storage and Search expose no soft-delete listing; the adapter returns an empty list without calling Azure.
- What-if requests `ResourceIdOnly`, not `FullResourcePayloads` (ADR-021): no resource payloads (and so no payload secrets) are returned. Incremental mode cannot report deletions, so FND-DEP-008 reports uncertain, not pass, for deletions.
- Unverified per ADR-006: whether what-if leaves a deployment record; whether checkPolicyRestrictions honours exemptions (results are `potential-conflict` unless the service reports a `deny` effect; audit/modify/append stay informational).
- Cosmos DB name check is `HEAD` (catalogue FND-DEP-007), outside the GET/POST allow-list: not implemented.
- Foundry feature-region matrix (`foundry_regions.go`) is embedded documentation data (source and date `FoundryFeatureDataDate` 2026-09-07), not a live API; consumers must report it as uncertain. Unlisted regions mean "not documented".
- `register/action` is never called: `RegisterAction(ns)` only names the RBAC action to test via `EffectiveActions` or `CheckReportedActions`.

## Dependencies

No new Go modules. Credentials come from `AZURE_ACCESS_TOKEN`, then
`azd auth token`, then `az account get-access-token`, held in memory only
(`internal/azure/auth.go`).
The Azure SDK/azidentity is not used, so the licence inventory and
THIRD_PARTY_NOTICES are unchanged. See ADR-021 (a). Adopting azidentity would need a new ADR and a
`scripts/dependency_audit.py` run.
