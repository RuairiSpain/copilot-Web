# Overlap fragment: DEP

Phase 0 research, verified 2026-10-04 against the local clones in `refs/` (Learn source repositories and GitHub specs, because
learn.microsoft.com is blocked), plus raw GitHub files where a clone was incomplete (noted per rule). Test scenario convention:
`positive` = the rule reports a violation, `negative` = compliant input, `skipped` = the rule cannot run and says why, `uncertain`
= the evidence cannot settle the question (never reported as pass).

API versions read: Microsoft.Resources subscriptions 2022-12-01, providers 2025-04-01, deployments 2026-06-01; Microsoft.CognitiveServices
2026-09-01; Microsoft.Authorization permissions and roleAssignments 2022-04-01, locks 2020-05-01, policy 2026-07-01; Microsoft.PolicyInsights 2024-10-01;
Microsoft.KeyVault 2026-05-15; Microsoft.ApiManagement 2024-05-01; Microsoft.Search 2025-05-01; Microsoft.Storage 2026-09-01;
Microsoft.DocumentDB 2026-03-15; Microsoft.Network 2025-05-01. azd design doc: `cli/azd/docs/design/provision-validation.md`.

## Rule table

| ID | Decision | Coverage | Mapped external IDs | Status |
|---|---|---|---|---|
| FND-DEP-001 | native | none | none | verified |
| FND-DEP-002 | native | none | none | verified |
| FND-DEP-003 | adapt | partial | azd local provision validation (roleAssignments/write check; no rule ID published) | verified |
| FND-DEP-004 | native | none | none | verified |
| FND-DEP-005 | native | none | none | verified |
| FND-DEP-006 | native | none | none (PSRule Azure.KeyVault.SoftDelete/PurgeProtect and Policy 1e66c121..., 0b60c0b2... audit protection settings, not name blocking) | verified |
| FND-DEP-007 | adapt | partial | PSRule Azure.KeyVault.Name, Azure.Storage.Name, Azure.Search.Name, Azure.APIM.Name, Azure.Cosmos.AccountName, Azure.ACR.Name (format only) | verified |
| FND-DEP-008 | native | partial | none (ARM what-if is the evidence source, not a rule) | verified |
| FND-DEP-009 | wrap | partial | Policy e56962a6-4747-49cd-b67b-bf8b01975c4c, e765b5de-1225-4ba3-bd56-1ac6695af988 (examples of restrictions surfaced) | verified |
| FND-DEP-010 | native | none | none | verified |
| FND-DEP-011 | native | partial | none | verified |
| FND-DEP-012 | adapt | partial | none (PSRule Azure.Resource.AllowedRegions and the allowed-locations policies answer a governance question, see rationale) | verified |

Counts: 12 rules. Status: verified 12, product-opinion 0, proposed 0, dropped 0.
Decisions: native 8 (001, 002, 004, 005, 006, 008, 010, 011), adapt 3 (003, 007, 012), wrap 1 (009), reuse 0, drop 0.
Implementation owner: native 11, azure-policy 1 (DEP-009, because the decision is wrap).

## Per-check API and role summary

All calls are read-only in effect. "Reader" means the built-in Reader role (actions `*/read`), which covers every permission that ends
in `/read`. It does not cover permissions that end in `/action`, even where the HTTP verb is POST and nothing is created.

| Rule | Call (verb path, api-version) | Required permission | Reader suffices | Answer |
|---|---|---|---|---|
| DEP-001 | GET /subscriptions/{id} 2022-12-01; GET /tenants 2022-12-01 | Microsoft.Resources/subscriptions/read, tenants/read | Yes | Definitive for tenant mismatch and subscription state; uncertain when the subscription GET fails |
| DEP-002 | GET /subscriptions/{id}/providers/{ns} 2025-04-01 | Microsoft.Resources/subscriptions/providers/read | Yes | Definitive |
| DEP-003 | GET .../resourcegroups/{rg}/providers/Microsoft.Authorization/permissions 2022-04-01; GET /subscriptions/{id}/providers/Microsoft.Authorization/roleAssignments 2022-04-01 | Microsoft.Authorization/permissions/read, roleAssignments/read | Yes (to read) | Missing permission is a likely violation; present permission is uncertain (cannot prove create rights) |
| DEP-004 | GET /subscriptions/{id}/providers/Microsoft.CognitiveServices/locations/{loc}/models 2026-09-01 | Microsoft.CognitiveServices/locations/models/read | Yes | Definitive for not offered; uncertain for gated models |
| DEP-005 | GET .../locations/{loc}/usages 2026-09-01; GET .../modelCapacities?modelFormat&modelName&modelVersion 2026-09-01 | locations/usages/read, modelCapacities/read (Cognitive Services Usages Reader or Reader, subscription scope) | Yes | Definitive when units are known and no existing deployment is updated; otherwise uncertain |
| DEP-006 | GET /subscriptions/{id}/providers/Microsoft.CognitiveServices/deletedAccounts 2026-09-01; GET .../Microsoft.KeyVault/deletedVaults 2026-05-15; GET .../Microsoft.ApiManagement/deletedservices 2024-05-01 | deletedAccounts/read, deletedVaults/read, deletedservices/read | Yes | Definitive for list membership (Key Vault effect likely) |
| DEP-007 | POST .../Microsoft.CognitiveServices/checkDomainAvailability 2026-09-01; POST .../Microsoft.Storage/checkNameAvailability 2026-09-01; POST .../Microsoft.KeyVault/checkNameAvailability 2026-05-15; POST .../Microsoft.Search/checkNameAvailability 2025-05-01; POST .../Microsoft.ApiManagement/checkNameAvailability 2024-05-01; HEAD /providers/Microsoft.DocumentDB/databaseAccountNames/{name} 2026-03-15 | KeyVault/checkNameAvailability/read, Storage/checknameavailability/read, ApiManagement/checkNameAvailability/read, DocumentDB/databaseAccountNames/read; but CognitiveServices/checkDomainAvailability/action and Search/checkNameAvailability/action | Yes for KeyVault, Storage, APIM, Cosmos. No for Foundry subdomain and Search (action permissions) | Definitive when the call returns; skipped with the missing permission otherwise |
| DEP-008 | POST .../resourcegroups/{rg}/providers/Microsoft.Resources/deployments/{name}/whatIf 2026-06-01 (long-running) | Microsoft.Resources/deployments/whatIf/action (Learn: same requirements as deploying) | No | Definitive for a returned Delete; uncertain for Ignore, Unsupported, Deploy, reference() deltas |
| DEP-009 | POST .../resourceGroups/{rg}/providers/Microsoft.PolicyInsights/checkPolicyRestrictions 2024-10-01; fallback GET .../Microsoft.Authorization/policyAssignments 2026-07-01 | Microsoft.PolicyInsights/checkPolicyRestrictions/read, Microsoft.Authorization/policyAssignments/read | Yes | Likely only; a clean result is uncertain |
| DEP-010 | GET .../Microsoft.Authorization/locks (subscription, resource group, resource, by scope) 2020-05-01; GET .../resourcegroups/{rg}/providers/Microsoft.Resources/deployments 2026-06-01 | Microsoft.Authorization/locks/read, Microsoft.Resources/deployments/read | Yes | Definitive for lock existence and level; likely for effect |
| DEP-011 | GET .../virtualNetworks/{vnet}/subnets/{subnet} 2025-05-01 (+ serviceAssociationLinks, resourceNavigationLinks, vnet GET) | Microsoft.Network/virtualNetworks/subnets/read | Yes | Definitive for delegation, size, range, name, region; likely for exclusivity; free addresses not computable |
| DEP-012 | GET /subscriptions/{id}/locations 2022-12-01; GET /subscriptions/{id}/providers/{ns} 2025-04-01; dated Learn feature matrix | Microsoft.Resources/subscriptions/locations/read, subscriptions/providers/read | Yes | Definitive for API checks; likely for the documentation-derived matrix |

### What-if and "read-only" (DEP-008), the nuance

- Verified: the what-if operation "doesn't make any changes to existing resources" (Learn, deploy-what-if, ms.date 06/26/2026). The API is a
  long-running POST (200 or 202). The permission is `Microsoft.Resources/deployments/whatIf/action`, and the Learn permissions text says what-if has
  the same permission requirements as deploying (write access on the resources plus `Microsoft.Resources/deployments/*`). Reader does not hold it.
- Not verified: whether a what-if call leaves a deployment record or activity-log entry. The sources read do not say, so none is claimed.
- Recommendation (product opinion, needs an ADR because CLAUDE.md says the doctor never mutates Azure): treat what-if as non-mutating but
  deployment-privileged. Allow it only as an explicit opt-in, never in the default run, never combined with a real deployment in one step, and report
  it as skipped (with the missing action) when the caller cannot call it. The default Phase 2 preflight must be useful without it.
- Same pattern for `checkNameAvailability`, `checkDomainAvailability`, `checkSkuAvailability` and `checkPolicyRestrictions`: POST verbs that evaluate and return
  an answer. They create nothing. Only some are covered by Reader (see table); `/action` ones need extra permission.

## Rules set to dropped or product-opinion

None. All twelve have a platform or documentation source for the facts they check. Judgement calls inside rules that are Foundry Doctor choices rather than
platform statements (labelled in each rule's notes): the dev warning severities of DEP-005 and DEP-008, the stateful-resource list in DEP-008, the staleness
threshold of the regional data in DEP-012, and the /27 hard floor versus /24 advice in DEP-011.

## Unverified

- Defender for Cloud and Advisor: the Advisor and Defender clones were not present in `refs/azure-docs` (sparse checkout lacked them), so mapping arrays are empty
  rather than guessed. A recommendation for quota or provider registration may exist.
- Key Vault Learn pages (soft-delete overview, recovery) are in a repository that was not reachable; whether a soft-deleted vault reserves the vault name is taken
  only from the list API, hence DEP-006 Key Vault matches are likely, not certain. The spec for deletedVaults was read.
- Whether the permissions API result reflects deny assignments, ABAC conditions, locks, policy or eligible (PIM) roles (DEP-003). The schema shows only
  actions, notActions, dataActions, notDataActions. Treated as uncertain.
- Which built-in role grants `Microsoft.CognitiveServices/checkDomainAvailability/action` and `Microsoft.Search/checkNameAvailability/action` for DEP-007.
  Reader does not; the role was not looked up.
- Container Registry name availability: only the permission name (`Microsoft.ContainerRegistry/checkNameAvailability/read`) was read, not the path or api-version. Not in DEP-007.
- Registration state values: the providers spec says only "string"; Learn shows Registered and Registering. Other values are not named.
- Location entry format in `resourceTypes[].locations` (name versus display name) is unspecified in the spec (DEP-012). The normalisation is unverified.
- Unit scale between a deployment's `sku.capacity` and a usage line limit for each deployment type (DEP-005). Only the documented example (thousands of tokens per minute) is asserted.
- Whether `checkPolicyRestrictions` honours exemptions and initiative parameters (DEP-009).
- Whether the azd server-side ARM preflight (`provision.preflight`, ValidatePreflight) checks any of these items. The azd design document names only the roleAssignments check
  for local validation. The azd core source is not in the clone, so its behaviour was not read.
- Network, PolicyInsights and Authorization policy specs were not in the shared `rest-api-specs` clone; they were read from a separate sparse clone and raw GitHub
  (the `verifiedVia` fields say so).
- The Learn locks article (ms.date 02/06/2025) and the recover/purge article (ms.date 10/02/2025) are older than the other sources.

## PRD / process ambiguities

- FND-DEP-008 title says "delete/replace"; the what-if ChangeType enum has no Replace value (Create, Delete, Ignore, Deploy, NoChange, Modify, Unsupported).
  Recreate detection is not available from the API and is not claimed. Suggest an ADR on wording or on a derived replace heuristic.
- CLAUDE.md "the doctor is read-only" versus POST-based checks and what-if (see nuance above). Needs an ADR to state that "read-only" means no mutation of Azure or azd state,
  with deployment-privileged probes (what-if) behind explicit opt-in.
- DEP-009 uses decision `wrap`; the brief maps wrap to a tool owner, so the owner is `azure-policy` with package `internal/adapters/azurepolicy`. If the lead considers the
  PolicyInsights call part of the native Azure client, change to `native` and decision `adapt`.
- Platform-basis rules are `error` everywhere; DEP-003 is platform-basis for the missing-permission case only, and its "present" case is always uncertain, so the
  severity statement does not imply a pass.

## Proposed new rules (fragment only, no catalogue files)

DEP
1. Agent subnet exhaustion forecast (deployment feasibility, supportability): compare delegated subnet size and existing `ipConfigurations`/`privateEndpoints` count against the
   Container Apps subnet sizing guidance. Blocked on a documented per-replica IP formula, so source first.
2. Cross-region dependency cost note (cost visibility): flag BYO Cosmos DB, AI Search and Storage placed in a different region from the Foundry resource, which Learn allows
   but says has cost implications. Informational severity, needs only the compiled template.
3. Deployment-history headroom (release readiness): warn at a configurable count approaching the 800-deployment history limit for the target resource group, independent of locks.
   Read-only (`Microsoft.Resources/deployments/read`), documented limit (lock-resources.md).
