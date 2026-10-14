# Phase 1 review: Azure black belt

Reviewer: `azure-black-belt` (independent; did not write the code). Date: 2026-10-14. Verdict on the first submission: **REJECT**.
Scope: Azure integration correctness of SEC, NET, IDN, OPS-004/005/009/010, REL-005 and COST-002 rules, the ARM normaliser, the graph, and ADR-006/007/012.
Method: realistic Bicep compiled with Bicep 0.47.16 and run through the built binary (fixtures were under the reviewer's scratchpad; the shapes are described below).
Not reviewed (outside this scope): baseline, expired suppressions, output determinism, the GitHub workflow, cross-platform binaries, secrets in snapshots.

## Blockers

| id | where | issue | evidence | fix |
|---|---|---|---|---|
| B1 | `internal/rules/sec/names.go` `piece`, `identityKey`, `idKey` | SEC-002/003/004 report a connected Search, Cosmos or Storage resource as "not-applicable: out-of-scope". `identityKey` expands `format('st{0}', ...)` in the resource name but `idKey` keeps the same `format()` inside `resourceId()` as an opaque token, so the two keys never match and Foundry-linked resources are exempt from the checks. | A project with `name: 'st${uniq}'` (compiles to `format('st{0}', variables('uniq'))`): all three rules skip all three resources as out-of-scope. Changing only the storage name to `var stName` made SEC-004 evaluate it. | Run the same `nameSegments` expansion on `resourceId()` arguments, or compare canonical text on both sides. If a connection's ResourceId is readable but matches nothing, return `scopeUncertain`, never `scopeOut`. Test with the compiled-Bicep shape. |
| B2 | `internal/app/iac.go`; `internal/bicep/normalise.go` `buildScope`, `substitute` | Parameter defaults are treated as facts and `main.parameters.json` (azd's standard parameter source) is never read; only `.bicepparam` is. A wrong default gives false failures or false "not applicable" results with no warning. | `param publicNetworkAccess string = 'Enabled'` with a `main.parameters.json` setting `Disabled`: NET-001 reports "accepts traffic from all public networks" (error, likely). `deploySearch` defaults false but the parameter file sets true: SEC-002 says the resource condition is false. | Read `main.parameters.json`, resolving `${VAR}` from the azd env or marking unresolved. When a parameter file exists or azd may override values, treat default-derived values as unresolved or cap confidence at uncertain. Say in the evidence when a value came from a default. |
| B3 | `internal/rules/idn/idn002.go` `storesOf` | A project is treated as using a store because any connection points at it; the rule then demands the standard agent-setup roles at error severity in every profile. | A project with only a `CognitiveSearch` tool connection (no capability host, no capabilitySettings) yields errors for Search Index Data Contributor and Search Service Contributor. The docs list those roles for the capability-host store flow (concepts-standard-agent-setup, roles table). Recommending write roles for a read-only tool connection works against least privilege. | Restrict to stores named by capabilityHost connection lists or capabilitySettings. Otherwise skip, or uncertain at most. |
| B4 | `internal/rules/idn/idn001.go` | IDN-001 runs on every `Microsoft.CognitiveServices/accounts` with no `kind` check. | A `SpeechServices` account without identity gives "Foundry account ...: identity.type is not set" (error, certain). | Limit to `AIServices` and accounts that own projects or deployments, as SEC-001 and NET-001 do. |
| B5 | `internal/rules/net/net003.go` | NET-003 evaluates every private endpoint; the catalogue says "serves a Foundry deployment". | A private endpoint to `Microsoft.Sql/servers` (group `sqlServer`) with no zone group gives error `no-zone-group`. For unmapped groups (`expectedZones` returns nil) a PE with any zone group passes. | Evaluate only PEs whose target is the Foundry account or a Foundry-linked Search, Cosmos or Storage resource. Not-applicable or skip for groups with no known mapping. |
| B6 | `internal/rules/net/topo.go` `lastNameSegment`; `net002.go` `viaConnection` | NET-002 resolves dependents through capability-host connection names, but only when every `format()` argument is a literal. The usual compiled shape (nested `format()` or a parameter as the account segment) never resolves. | In realistic projects NET-002 skips Storage, Cosmos and Search as "dependent-resource-id ... cannot be resolved". A skip, not a false pass, but the rule is inert on the common layout. | Read the last quoted `format()` argument, as `sec/names.go` does. Put the name parsing in one shared helper. |
| B7 | `internal/bicep/normalise.go` (no `variables` handling) | Module parameters carrying `resourceId()` or other expressions become Unresolved. `variables('x')` is never substituted, even for literal variables. Role GUIDs held in variables and module-based private endpoints make the rules unusable. | Realistic project: NET-001/003/005 skip because the PE target, subnet and zone-group parent sit inside modules; IDN-002 and IDN-004 skip because the role GUID is `variables('roleSearchData')`; net result: three IDN-002 errors plus silent not-applicable results. | Substitute exact `[variables('x')]` references when the variable is literal. For module parameters, inline the caller's expression text rather than collapsing it to Unresolved. |

## Major

| id | where | issue | fix |
|---|---|---|---|
| M1 | `internal/rules/sec/sec006.go` | The Storage private-range check includes `100.64.0.0/10`. Storage documentation (storage-network-security-limitations, fetched 2026-10-14) names only 10/8, 172.16-172.31 and 192.168. The catalogue text claims 100.64/10 for Storage, which is not supported. | Apply 100.64.0.0/10 to Cosmos only; correct the catalogue text. |
| M2 | `internal/rules/ops/ops009.go` | DeveloperTier fails even when `policy.dataResidency.scope` is `global`, which imposes no residency requirement. | Fail only when the scope is not `global`. |
| M3 | `rules.go` `run` in sec; `idn002.go`, `idn004.go` and other rules | Every rule appends an unconditional `control-plane-unavailable` skip, so `--strict` exits 3 permanently (a clean project exits 3). | Emit live skips only when `--runtime` or `--preflight` was requested, or exclude by-design skips from `--strict`. |
| M4 | `internal/rules/net/topo.go`, `internal/graph` | Inline VNet subnets (`properties.subnets[]`) are not modelled; NET-003 link checks and NET-005 skip with "VNet cannot be identified". No test for this. | Model inline subnets as nodes under their VNet; add a test. |

## Minor and nit

- NET-007: `publicNetworkAccess` absent with `defaultAction: Deny` and no rules passes; NET-001 treats absent as Enabled. Make them consistent.
- NET-001: "no-private-endpoint" is certain; a PE can live in another deployment or be created by policy. Use likely.
- NET-001 vs SEC `assess`: a conditional PE with a literal-true condition still gives uncertain in NET-001. Make consistent.
- SEC-004: evidence "default TLS1_0 applies" is likely stale (Storage stopped supporting TLS 1.0/1.1 on 2026-02-03; the default for accounts created without the property is unconfirmed). Reword as "not set explicitly" and drop the default claim.
- SEC-004: the Shared Key remediation should warn when a Foundry connection uses `AccountKey` or `SAS` auth.
- IDN-003: `api-version-too-old` is certain, but preview versions 2018-09-01-preview to 2021 do support `principalType`. Soften or lower confidence.
- OPS-009: region comparison is exact and case-sensitive (ADR-007) but NET-005 normalises; a display name such as "East US" falsely fails. Normalise or document.
- OPS-010: equating ARM `model.format` with the policy `model.publisher`, and `format/name/version` with `assetId`, is unsupported (policy definition `AllowDeployRegistryModels_Audit.json`; its "empty lists plus Deny blocks all" semantics are correct).
- COST-002: ignores `options.throughput` on Cosmos database and container resources; Search SKU match is case-sensitive. Only false negatives.
- Normaliser: legacy nested `resources[]` children keep unqualified types (the compiler never emits them).
- `bicep/no-unnecessary-dependson` triggered `FND-SYS-DUPLICATE-FINGERPRINT` on a realistic project (adapter diagnostics). Add a distinguishing key.
- Name-parsing logic is duplicated in `rules/net/topo.go`, `rules/sec/names.go` and `rules/idn` (`nameKey`) with different capabilities (this causes B1 and B6). Consolidate into one shared package.

## Confirmed correct

Zone group limits (5 zones, one zone per name, one zone group per PE); the Foundry account DNS zones and the searchService, Sql and blob zones; NET-004's four constraints (delegation to `Microsoft.App/environments`, /27 minimum, 63-byte name limit, no sharing); the RBAC role GUIDs (Cosmos SQL role `...0002` stays corroborated only); the IDN-002 role set against the standard-agent-setup doc; both ARM forms (array and languageVersion 2.0) give identical results; the `existing` exclusion, copy/condition flags, module-chain confidence demotion and `expressionEvaluationOptions.scope`; secure parameters are never substituted; the doctor ran with no Azure credentials; REL-005 limits; OPS-005 and OPS-004 limits (not re-verified against docs); ADR-006 Reader versus `/action` split (a hypothesis, as the ADR says); ADR-012 skips; ADR-007 missing-key behaviour.

## Unsupported assumptions

Defaults are valid values when no parameter file is read (B2); a connection to a store implies the standard-agent roles (B3); all `Microsoft.CognitiveServices/accounts` are Foundry accounts (B4); all private endpoints serve Foundry (B5); the Storage doc covers 100.64.0.0/10 (M1); policy `publisher` equals ARM `model.format` (OPS-010); the TLS 1.0 default is still current (SEC-004).

## Missing tests

A compiled-Bicep golden with `format()` names, `uniqueString` and variables through SEC-002/003/004, NET-002 and IDN-002; `main.parameters.json` overriding a default; a Search connection with no capability host; a non-AIServices account and a non-Foundry PE; inline VNet subnets and a module-based PE with expression parameters; `--strict` on a project that otherwise has no findings.
