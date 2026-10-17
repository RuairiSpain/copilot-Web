# Phase 1 re-review: Azure black belt (code from `main`)

Reviewer: `azure-black-belt` (independent). Date: 2026-10-15. Code: branch `claude/foundry-doctor-phase-1` (main's implementation, PR #18). Verdict: **REJECT**.

Method: Bicep CLI 0.48.1 (the code's contract version) downloaded; binary built with Go 1.25.13 and run as `foundry-doctor doctor --local --profile prod` with a stub `azd` (1.35.0) because rules with an `azd` range skip without one. No Azure credentials. Facts checked against Microsoft Learn and azure-rest-api-specs.

Defects violating "no false pass / false fail on realistic Bicep": N1-N6, N9, N10. Defects violating "skipped never equals passed" at process level: N11, N14.

## Findings

| id | sev | where | issue | fix |
|---|---|---|---|---|
| N1 | blocker | `internal/rules/sec/sec.go:266-280`; `sec_later.go:44,220` | `inFoundryScope` is true for any resource with an expression name once one Foundry connection exists; literal names match by substring. SEC-002/003/005/010 fail unrelated Search, Cosmos, Key Vault resources. | Compare canonical IDs and whole names; unprovable correlation is a skip `scope-uncertain`. |
| N2 | blocker | `armmodel.go:102-136`, `bicep/arm.go:49-51`, `pkg/sdk` | `condition` and `copy` never reached `sdk.ARMResource`; conditional resources count as deployed (conditional Search fails SEC-002; conditional private endpoint satisfies NET-001). | Contract fields added (`Conditional`, `Condition`, `Looped`); rules must honour them. Literal false = not deployed; parameter-driven = uncertain; a conditional private endpoint is not proof. |
| N3 | blocker | `net/rules.go:458,471,476`; `net/helpers.go:34-40` | `unresolved()` knows only `parameters(`, `variables(`, `reference(`. `[resourceGroup().location]` compared as literal, so NET-005 fails with certain confidence. | Any string starting with `[` (not `[[`) is unresolved; one shared helper. |
| N4 | blocker | `net/rules.go:63-74`; `helpers.go:101-110,183-200` | Same helper: NET-001 does not match a private endpoint whose target is `resourceId(type, format('ai-{0}', uniqueString(...)))`. | Compare only when both sides are literals; otherwise skip. |
| N5 | blocker | `ops/ops.go:330,237-243` | OPS-001 matches `diagnosticSettings.scope` to the resource name with `refMatches`; expression names never match, so a configured account is reported as having none. | Compare normalised expression text of the scope's last argument; else uncertain. |
| N6 | blocker | `idn/idn.go:265-268` | `refersToClientID` uses `Contains(..., "appid")`; a parameter named `appIdentityPrincipalId` fails IDN-003. | Match property accesses `.clientId`/`.appId` or whole-word identifiers. |
| N7 | major | `idn/idn.go:139`; `idn004.go:152` | IDN-001 has no `kind` check (SpeechServices account gets an error). | Limit to AIServices and accounts owning projects or deployments. |
| N8 | major | `idn004.go:143-152`; `armmodel.go:194-204` | IDN-004 inert on compiled Bicep: `subscriptionResourceId` role IDs and `format()` scopes unhandled. | Take the last literal argument of `subscriptionResourceId`/`extensionResourceId`; resolve scope from the last `format()` argument. |
| N9 | major | `ops/ops.go:777-791` | OPS-010 compares `format/[parameters('modelName')]/version` as a literal against the allow list. | Skip when format, name or version contains an expression. |
| N10 | major | `cost/cost.go:113-123,394-409,443-450` | COST-001 `contactEmails: [parameters(...)]` returns nil, failing the contact check. | Treat strings starting with `[` as unresolved. |
| N11 | major | `app/app.go:347-360`; `wire.go:127-166`; `config/profiles.go:28-31` | Missing Bicep CLI or compile failure: one stderr warning, about 100 rules skipped, exit 0. `validation.bicep: required` is never read outside config. | When required, map load failures to exit 2. |
| N12 | major | `rules/rules.go:148-156` | Rules with an `azd` range skip as `input-unavailable` when `azd` is absent (about half the catalogue); closed upper bound `<=1.36.0-beta.1`. | Reason `azd-version-unknown`; reconsider the gate for Bicep-only rules. |
| N13 | major | engine; ADR-012 | Engine does not skip `bicep-arm` rules with `synthetic-infrastructure` and has no `Summary.SkippedByReason`. | Implement or amend ADR-012. |
| N14 | major | `app` strict handling | `--strict` on a clean project exits 3 (85 skips, 22 `not-implemented`, 10 not applicable, 4 `profile-key-missing`). | Only skips that hid a check trigger `--strict`. |
| N15 | minor | `net/rules.go:285-300` | NET-003 evaluates private endpoints of non-Foundry targets for mapped groups. | Limit to Foundry account and linked stores. |
| N16 | minor | ADR-007; `net/rules.go:183-190`; `ops.go:311` | Code reads `policy.network.dnsManagedByPolicy`/`centralDns`; ADR-007 documents `managedByAzurePolicy` and `network.publicAccess`; the latter has no consumer. | Honour ADR keys or amend ADR-007. |
| N17 | minor | `net/net_later.go:211-228` | NET-011 does not exempt agent subnets referenced via `reference(...).subnets[0].id`. | Resolve injection to a subnet first. |
| N18 | minor | `sec/sec.go:465` | Cosmos `0.0.0.0` described as "all addresses"; it allows Azure datacenter IPs. | Reword. |
| N19 | minor | `net/net_later.go:62-63` | NET-006 skips CGNAT as "documentation conflicts"; current doc permits it except three reserved 100.100.x ranges already in the code. | Evaluate it. |
| N20 | minor | `ops/ops.go:618`; `cost.go:202,209` | OPS-006 reads a boolean with `getString`; COST-002 raises a policy limit below 3000 to 3000. | Read bool; use limit as configured. |
| N21 | minor | `rel/rel.go:250-262` | REL-004 counts any resource-group-scope lock, including other groups; scope-less lock not counted. | Compare with the deployment group. |
| N22 | minor | `graph/graph.go:201-225`; `app/graph_cmd.go:261-275` | In-template resources with expression names appear as `external:` nodes. | Resolve by type plus name segments. |
| N23 | minor | `ops/ops.go:298-305` | OPS-001 log categories have no cited source. | Cite or mark unverified. |

## Checklist from the earlier review

| id | status on main |
|---|---|
| B1 | absent in that form; inverse defect present (N1) |
| B2 | absent (`main.parameters.json` never read; `[parameters()]` values are skipped) |
| B3 | not applicable (IDN-002 is `not-implemented`) |
| B4 | present |
| B5 | partly present (unmapped groups skip; mapped groups on non-Foundry targets still evaluated) |
| B6 | present, fails safe |
| B7 | present, fails safe; IDN-004 inert (N8) |
| M1 | absent for Storage; Cosmos inclusion unverified |
| M2 | present (`ops.go:723`) |
| M3 | present (N14) |
| M4 | present in effect (`vnet.properties.subnets[0].id` compiles to a `reference()` expression) |
| NET-007 vs NET-001 | absent |
| NET-001 certain confidence | present; combined with N2, N4 |
| SEC-004 TLS default claim | absent |
| SEC-004 AccountKey/SAS warning | present (missing) |
| IDN-003 api-version-too-old | present (preview versions support `principalType`) |
| OPS-009 region compare | absent (normalised; differs from ADR-007 point 6) |
| OPS-010 format = publisher | present |
| COST-002 | `options.throughput` ignored (present); APIM SKU case-sensitive (present); Search SKU lower-cased (absent) |
| Legacy nested `resources[]` | children dropped silently |
| Duplicate-fingerprint diagnostics | not tested |
| Name parsing duplicated | present (`net/helpers.go:101`, `ops.go:237`, `sec/sec.go:177`, `idn004.go:71`, `rel.go:461`, `armmodel.go:169-243`) |

## Facts checked

Correct: NET-003 Foundry DNS zones; NET-004 delegation, /27 and 63-byte subnet name; NET-006 reserved ranges; SEC-006 Storage private ranges; REL-008 `spilloverDeploymentName`; OPS-009 DeveloperTier statement; NET-002 `capabilitySettings` exists (preview, also on projects; NET-002 reads only the account). Unverified: Cosmos private-range list with 100.64.0.0/10; OPS-001/004/005 limits; REL-005 limits; IDN-004 role GUIDs not re-fetched.

## Unsupported assumptions

Connection plus name expression means "referenced by Foundry"; ARM strings without three keywords are literals; compiled template resources are deployed; unmatched private endpoint means none exists; absence in the template means absence in Azure (OPS-003, COST-001, REL-004); Cosmos treats 100.64.0.0/10 as non-enforced; OPS-001 log category lists; azd range still matches current releases; multi-zone DNS applies to every account private endpoint.

## Missing tests

Compiled-Bicep goldens with `format()`/`uniqueString()` names through SEC, NET-001, OPS-001, IDN-003; conditional Search, private endpoint and connection; `resourceGroup().location` through NET-005; unrelated expression-named Search/Cosmos with a Foundry connection; IDN-003 parameter names; IDN-004 with `subscriptionResourceId`; non-AIServices accounts; parameter-valued model name/version under OPS-010; COST-001 parameter emails; missing Bicep CLI and compile failure exit codes with and without `validation.bicep: required`; `--strict` on a clean project; `main.parameters.json` present; inline VNet subnets and graph; non-Foundry blob/vault/registry private endpoints under NET-003; ADR-007 keys; run without `azd`, asserting the skip reason names azd. No rule test uses any compiled-Bicep expression shape.
