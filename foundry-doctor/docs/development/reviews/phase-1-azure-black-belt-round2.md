# Phase 1 round-2 verification: Azure black belt (head d557157)

Reviewer: `azure-black-belt` (independent). Date: 2026-10-15. Verdict: **REJECT** on one new blocker (R1, out-of-memory on a pathological template). B1-B3 and nine of the fourteen majors are fixed.

Method: binary built with Go 1.26.9; `--azd-version 1.35.0`; Bicep CLI 0.48.1; no Azure credentials. Inputs: earlier projects r2/{m,m2,m3,m4,ni,id1,tg}, Microsoft samples 15 and 41, `samples/good-private`, and about 20 new projects. Microsoft Learn was unreachable (ENOTFOUND); no Learn page was re-fetched this round.

## Status of the previous findings

| id | status |
|---|---|
| B1 conditional or looped module | fixed (REL-001, REL-005, OPS-005, NET-008 report "cannot be resolved offline"; `if (false)` yields nothing; holds at two nesting levels) |
| B2 same-text names across modules | fixed (connection pointing at s2 gives SEC-002 on s2 only) |
| B3 expression-valued `networkInjections` | fixed (NET-002/004/005/006/011 skip as uncertain) |
| M5, M6, M7, M11, M12, M13, m-noise (Finding.Key) | fixed |
| M2 IDN-004 principal | fixed |
| M3 partial-skip visibility | fixed (note on the first finding only) |
| M9 SEC-006 scope | fixed (often `scope-uncertain`, see R3) |
| M1 OPS-004 | partly fixed: evaluates with a real config; untagged resource and `tags: tags` are uncertain because armmodel cannot tell them apart |
| M8 IDN-003 wording | partly fixed: finding still carries `confidence: high` and `severity: error` for a name-only match |
| M10 IDN-004 scope wording | partly fixed: prints "broad deployment scope"; info finding not emitted; see R4 |
| M4 parameter files | not fixed (known): samples 15 and 41 still skip most rules |
| M14 IDN-001 OpenAI-kind account | not fixed (needs confirmation in the rule text) |
| N14 `--strict` on a clean project | partly fixed, unchanged: Storage-only project and `samples/good-private` exit 3 |

## New or remaining findings

| id | sev | where | issue | fix |
|---|---|---|---|---|
| R1 | blocker | `internal/armmodel/armmodel.go` `childScope`, `bindingText` (~206-215, 266-280) | Parameter substitution inlines the caller's expression text into every use in the child with no size cap. A 24 KB compiled template (10 nested modules, each passing `'${a}${a}…'` eight times) exhausts memory; with doubling, depth 16 takes 4 s. Under `ulimit -v 6000000`: `fatal error: out of memory`, exit 2. | Cap substituted text (for example 4 KiB); when a binding would exceed the cap keep the scope-qualified name and treat the value as unknown; cap total model size; add a regression test. |
| R2 | major | `armmodel.go` `toResource`; `rules/{sec,ops,rel,idn,cost}`; `graph` | Internal scope tokens appear in user-visible output (`[variables('nm@m2-o/m0-inner')]` in OPS-001, REL-001 and graph labels); `StripScope` is used only in net. The module path uses `m<idx>-<name>` (declaration index) and `Resource.Name` feeds the fingerprint, so inserting a module earlier changes fingerprints and resurfaces baselined findings. | Keep the qualified identity internal; display `StripScope(name)` in `Finding.Resource.Name`, skip reasons and graph labels; build the path from the module deployment name, not the index; exclude the path from the fingerprint. |
| R3 | major (safe skip) | `armmodel.go`, `foundrylink.go` | Module outputs are not resolved: `searchId: s1.outputs.id` compiles to `[reference(resourceId('Microsoft.Resources/deployments','s1'),'2025-04-01').outputs.id.value]`, so every Search, Cosmos, Key Vault and Storage resource becomes Uncertain and SEC-002/003/006 skip as `scope-uncertain`; the graph has no edge from the connection to the Search service. | Resolve `outputs.X.value` from the nested template's `outputs[X].value` using the child's substitution. |
| R4 | major (false negative shown as uncertain skip) | `armmodel.go` (`res.Scope = tx(...)` with `normalizeLiteralRef`), `idn004.go:266-272` | `Fold` turns `scope: [resourceId('Microsoft.CognitiveServices/accounts','ai-id')]` into the bare name `ai-id`; `ClassifyScope("ai-id")` is Unknown, so Owner or User Access Administrator at a Foundry resource scope (`scope: account`, the commonest form) is never reported. | Do not fold `Scope`, or classify the scope from the unfolded text; add a golden for a resource-scoped Owner. |
| R5 | minor | `internal/bicep/sourcemap.go:42`, `Options.Modules` never populated | A finding on a resource inside a module reports `location.file` as the entry file instead of `modules/*.bicep`. | Map the module deployment name to its source file. |
| R6 | minor | CFG-010 with Bicep missing | Reason is `not-applicable: project declares no model deployments` although deployments exist in the unreadable Bicep, so `--strict` ignores it. | Emit `input-unavailable` when the ARM model is unavailable. |
| R7 | minor | `graph`, `armmodel` | A module's own `dependsOn` is not propagated to its flattened resources, so there are no cross-module dependency edges (not a regression). | Add the module's `dependsOn` to its children's placements. |
| R8 | minor | IDN-003 | Name-only match reported with `confidence: high`, `severity: error` while the text says "likely". | Per-finding confidence and severity (known open), or lower the rule's severity or skip. |
| R9 | minor | `rules/skipclass.go` | `profile-key-missing:network.cloud` (NET-003 fully hidden) is by design and does not count under `--strict`. | Decide explicitly; document, or count it when private endpoints are present. |
| R10 | minor (N14) | `skipclass.go` | `--strict` can never pass for Foundry projects: many skips fall into the fail-safe default (CFG-006/012, COST-001, IQ-001..005/007/008/010/012, IDN-005, OPS-007, REL-004, SEC-007/008/009/011/014, NET-010). | Classify the "nothing to check" reasons as by design, or document a strict allowlist. |

## Unsupported assumptions still present

Parameter files and defaults are ignored (M4); Bicep variables are not exposed to the rules (IDN-004 role IDs); module outputs are opaque (R3); tags that are an expression and absent tags are indistinguishable (M1).

## Missing tests

Hostile substitution blow-up (R1); unresolved module-local names displayed without scope tokens and fingerprint stable under module reordering (R2); Owner at `scope: <resource>` end to end (R4); module output chaining into a connection (R3); finding location for a resource inside a module (R5); CFG-010 with Bicep missing (R6); a clean Foundry project under `--strict` (R10).

## Verdict note

The reviewer states round 2 reaches ACCEPT once R1 is fixed, R2 and R4 are fixed in the same pass, and R3 is recorded as a Phase 1 limitation.
