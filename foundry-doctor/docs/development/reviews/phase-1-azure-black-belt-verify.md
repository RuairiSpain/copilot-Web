# Phase 1 verification: Azure black belt (head 2c14b36)

Reviewer: `azure-black-belt` (independent). Date: 2026-10-15. Verdict: **REJECT**. Of N1-N23: 15 fixed, 6 partly fixed, 2 fixed with caveats, none unfixed. Three blocking residual defects (B1-B3) and eleven non-blocking ones.

Method: binary built with Go 1.25.13; Bicep CLI 0.48.1; stub azd 1.35.0 (and `--azd-version 1.36.0-beta.1`); no Azure credentials. About 20 new projects plus Microsoft's samples 15-private-network-standard-agent-setup and 41-standard-agent-setup compiled and run. Tests passed in a scratch copy (`go test ./internal/... ./pkg/...`).

## Status of earlier findings

| id | status |
|---|---|
| N1 | partly fixed: top-level resources fixed; module layouts still over-link (B2) |
| N2 | partly fixed: direct `if (...)` resources handled; modules are not (B1) |
| N3, N4, N5, N9, N10, N13, N15, N16, N18, N19, N20, N21, N22, N23 | fixed |
| N6 | fixed; a parameter named `appId` is flagged with confidence "high" from the name alone (M8) |
| N7 | fixed (an OpenAI-kind account with deployments and no identity is not flagged, M14) |
| N8 | fixed for literal GUIDs; role IDs held in Bicep variables stay `unresolved-expression`; missing `principalType` hides Owner findings (M2) |
| N11 | fixed; compile-failure message says "1 diagnostics" and omits the text |
| N12 | partly fixed: reason `azd-version-unknown` and header note work; Bicep-only rules are still gated on azd |
| N14 | partly fixed: by-design heads no longer count; a clean Storage-only project still exits 3 under `--strict` because CFG-006/008/009/010/012, ENV-001..004, IQ-*, IDN-005, SEC-008/009/014, OPS-007/008, REL-004/006/009, GW-006 and NET-003 skips fall into the fail-safe default |
| N17 | partly fixed (expression-valued `networkInjections`, B3) |

## New or residual findings

| id | sev | where | issue | fix |
|---|---|---|---|---|
| B1 | blocker | `internal/armmodel/armmodel.go:84-99,126` | `walk` flattens a module's nested template but takes Conditional/Condition/Looped from the child only. Resources in `module x = if (flag)` or a looped module count as deployed. Example: `module s1 'modules/search.bicep' = if (deploySearch)` (default false) gives `WARNING FND-REL-001` for a Search service that is not deployed. | Propagate ancestors' Conditional/Condition/Looped to descendants (MaybeDeployed if any ancestor is, NotDeployed on a literal false). Add goldens for a conditional and a looped module. |
| B2 | blocker | `internal/armexpr/refs.go:351`, `armmodel.go:96` | Module-local expressions such as `[parameters('name')]` are compared by text across different nested templates, so two instances of one module look identical. The deployment `parameters` binding is never substituted. SEC-002 still reports an unrelated module Search as "referenced by Foundry" (confidence high); two same-text resources collapse to one finding; the graph shows one search node for two Search modules. | Qualify identity with the deployment path and substitute the caller's parameter values when flattening; compare Same only within one template scope; until then treat `parameters()` names from different nested templates as Unknown. |
| B3 | blocker | `internal/rules/net/helpers.go:280-290`, `net_later.go:225-239` | When `networkInjections` is an ARM expression string (as in Microsoft's sample 15), `injections` reads only an array, sees nothing, NET-004/006 report `not-applicable` (hidden under `--strict`) and NET-011 warns about the agent subnet. A /28 agent subnet behind a ternary is never reported. | Treat an unresolved `networkInjections` expression as uncertain for NET-002/004/006/011, not `not-applicable`. Add the sample shape as a golden. |
| M1 | major | `ops/ops.go:572-600` `eval004` | OPS-004 can never evaluate with a real config: `Policy.Get("tags.required")` returns `[]config.TagRequirement`, the switch handles only `[]any` and an anonymous struct slice, so the rule skips `profile-key-missing:tags.required` (silent under `--strict`). A resource with no tags is skipped, not failed. | Add the `[]config.TagRequirement` case; distinguish absent tags (fail) from expression-valued tags (uncertain); test with the real EffectivePolicy. |
| M2 | major | `idn/idn004.go:~165-172` | A role assignment with no `principalType` is skipped as `unresolved-expression` even when the principal provably is a Foundry identity, so Owner/Contributor/UAA assignments from older templates are missed. | Classify the principal first; report the real reason. |
| M3 | major | `result()` in sec, idn and others | "Findings win, otherwise uncertainty skips": when a rule returns any finding, resources it could not evaluate vanish from the report. | Carry partial skips alongside findings, or emit an info finding listing unevaluated resources. |
| M4 | major | design | Parameter files (`main.parameters.json`, `.bicepparam`) and defaults are not used, so parameter-driven conditions and regions make rules inert on realistic templates: Microsoft sample 15 skips nearly all SEC/IDN/NET/OPS/REL rules as `conditional-resource` or uncertain. | Record as a Phase 1 limitation with a skip reason naming the missing parameter file; optionally resolve from a parameter file, clearly marked. |
| M5 | major | `net` region compare, `armexpr/refs.go compareSegment` | Identical region expressions are not treated as equal: account and VNet both at `[parameters('location')]` skip "regions are not comparable" (NET-005 inert). | Compare normalised expression text within one template scope once B2 is fixed. |
| M6 | major | `net/rules.go:304-306` `detectCloud` | NET-003 skips "cloud undeterminable" whenever regions are parameters and also when there is no private endpoint; the reason head is not by-design, so it hides under `--strict`. | `not-applicable` when no private endpoint is in scope; `profile-key-missing:network.cloud` for the policy case. |
| M7 | major | PE-in-module layouts, `net/rules.go:50-90,276-290` | The commonest AVM-style layout (private-endpoint module with `privateLinkServiceId` as a parameter) makes NET-001/003/005 uncertain. No false result, but the headline rule is inert. Same root cause as B2. | Bind module parameters (B2). |
| M8 | minor | `armexpr/refs.go:496-527`, `idn.go:324` | IDN-003 reports a client-ID read with confidence "high" from a parameter name alone. | Property accesses `.clientId` are certain; name-only matches are "likely". |
| M9 | minor | `sec/sec_later.go`, `sec.go:422-431` | SEC-006 checks every Cosmos, Key Vault, Search and Storage resource although its description says "Foundry-related". | Use `foundrylink` or amend the description. |
| M10 | minor | `idn004.go`, catalogue FND-IDN-004 | Evidence prints `broad scope ""` for the deployment scope; the catalogue says Contributor at one Foundry resource's scope is reported as info, but no info finding is emitted. | Print "deployment scope"; add the info finding. |
| M11 | minor | `net_later.go` NET-011 | Only inline `vnet.properties.subnets` are checked, not `Microsoft.Network/virtualNetworks/subnets` child resources. | Include child subnet resources. |
| M12 | minor | graph / armmodel | Graph merges same-named module resources into one node. | Same root cause as B2. |
| M13 | minor | `rel/rel.go` `lockCoversGroup` | A parameter-valued lock scope returns "none", not "unknown". | Return unknown. |
| M14 | minor | IDN-001 | An OpenAI-kind account with deployments and no identity is not flagged. May be intended. | Confirm in the rule text. |
| m-noise | minor | rules without `Finding.Key` | Every run prints `warning: FND-OPS-003: findings share a fingerprint because the rule sets no Finding.Key` (COST-004 as well). | Set `Finding.Key` in ops/cost/rel rules. |

## Unsupported assumptions still present

A module's resources are deployed whenever the module is declared (B1); equal text for `parameters('x')` means the same value across nested templates (B2); a non-array `networkInjections` means no injection (B3); IDN-004 requires `principalType` to classify a principal (M2); template regions equal deployed regions; connection names compared with resource names are `Different` only when literal, otherwise `Unknown`, so a capability host that lists connection names can make the whole link set uncertain.

## Missing tests

Compiled goldens for a conditional module, a looped module, one module deployed twice, and a connection module reusing the param name `name`; expression-valued `networkInjections` using sample 15; OPS-004 end to end with the real config type and a resource with no tags; IDN-004 with no `principalType` and `variables()` role IDs; partial-skip visibility alongside findings; `--strict` on a Foundry-complete clean project; NET-005 with identical expressions; NET-003 with no private endpoint and with a parameter location.

## Facts checked

Azure Monitor supported log categories for accounts and projects (OPS-001 sets match); private DNS zone names; Microsoft samples 15 and 41; conditional resource and module behaviour in Bicep. Budget contact rules were not re-fetched.
