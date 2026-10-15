# Phase 1 hand-off

Status: **implemented, open issues recorded** (not signed off as fully complete; see Known limitations).

## Delivered
- Public SDK `pkg/sdk` (Rule, Input, Result, Skip; additive ARMResource fields, optional `ARMOutputs`).
- Packages: `findings`, `azureyaml`, `config` (+ `schemas/config.schema.json`), `rules` (selectors, engine, profiles), `baseline`, `suppress`, `project` (os.Root confinement), `bicep`, `graph`, `armmodel`, `report` (console, JSON, Markdown, SARIF 2.1.0), `app`, `cmd/foundry-doctor` (doctor, explain, compare).
- 29 Phase 1 rules: CFG 001-007/011/012, ENV 001-004, SEC 001-004/006, IDN 001/003/004/006, NET 001-005/007/008.
- CI/release workflows, release scripts, samples (good, bad, azure-yaml-only, bicep-backed), threat model.

## Changed contracts
- `ARMResource` gained Region, Kind, SKUName, Identity, Scope; `ARMOutput` and optional `ARMOutputs` interface.
- Engine-internal rule ID `FND-SUPPRESS-001` (expired suppression), not in the catalogue.

## Rule catalogue changes
None to catalogue entries. Policy keys introduced by implementers (not in catalogue): `network.dnsManagedByPolicy`, `network.centralDns`, `network.cloud`, `environments.tiers`.

## Commands implemented
`doctor`, `explain`, `compare`; exit codes 0/1/2/3/4 per PRD section 7.

## Tests executed and results
Windows/arm64: gofmt, `go vet ./...`, `go test -count=1 ./...`, staticcheck, govulncheck (0 reachable), check-rule-catalog, check-no-secrets, dependency audit: all pass. `-race`: SKIPPED (unsupported on windows/arm64; runs in Linux CI).

## Security and privacy review
security-review pass: no findings (subprocess args fixed, os.Root confinement, secret digesting, pinned actions). `internal/report` and `internal/findings` were not covered in that pass.

## Foundry lead review
Initial REJECT (CFG-006 blocker; IDN-004 x2 and NET-003 high). All four fixed with tests; re-review pending.

## Azure black belt review
Not yet run for Phase 1.

## Known limitations
Rules needing data not yet provided skip rather than pass. CFG-008..010 not implemented. NET-003 China zone table incomplete. IDN-004 deployment-identity exclusion not configurable. Bicep and azure.yaml diagnostics are warnings, not findings.

## Deferred issues with rationale
Phase 0 open items (XF provenance, race gate, CI run, adapter fixtures, provisional overlap decisions). PSRule adapter deferred. Open questions: cmd directory name (`cmd/foundry-doctor` vs PRD `cmd/foundry`), config filename `.foundry-doctor/config.yaml`, `compare` exit code, policy key names.

## Compatibility matrix
Go 1.25.12; Bicep 0.48.1 verified; azd version probed via `azd version --output json` (shape unverified).

## Artefacts for next phase
`sdk.Rule` contract, `internal/app` wiring (`builtinRules`), providers, `armmodel`.

## Exact next-phase prerequisites
Linux CI green (race, spikes); resolve policy key naming; Azure read-only adapter design (Phase 2).

## Definition of Done audit
| DoD item | Status | Current evidence |
|---|---|---|
| Local doctor runs offline with 4 formats | met | e2e tests in cmd/foundry-doctor |
| Exit codes consistent | met | e2e and app tests |
| 35-40 rules | unmet (29 implemented) | catalogue allocation; remainder deferred |
| Race-clean | unmet | needs Linux CI |
| Independent reviews | partial | foundry/go re-review and black-belt pending |
