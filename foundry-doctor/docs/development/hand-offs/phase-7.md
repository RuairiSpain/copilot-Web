# Phase 7 hand-off

## Delivered
- Added Phase 7 cost-estimation engine under `internal/cost`.
- Added `foundry-doctor cost` command with console/json/markdown output.
- Implemented `FND-COST-001` and `FND-COST-002` in `internal/rules/cost`.
- Preserved top-level ARM `sku` objects so capacity-aware rules can read
  `sku.capacity`.

## Changed contracts
- `pkg/sdk.ARMResource` now carries `SKU map[string]any` in addition to
  `SKUName`.
- `internal/armmodel` preserves the top-level `sku` object when present.

## Rule catalogue changes
- None.

## Commands implemented
- `foundry-doctor cost`

## Tests executed and results
- Targeted:
  - `go test -count=1 ./internal/cost/... ./internal/rules/cost ./internal/app ./cmd/foundry-doctor`
  - Cost packages and app passed.
  - `cmd/foundry-doctor` still has an **unrelated Phase 6 annotate test
    expectation failure** (`TestAnnotate` expects non-empty GitHub output).

## Security and privacy review
- Public Retail Prices API only; no credentials required.
- Cost output is advisory and excludes token usage and secrets.
- No response bodies with secrets or invoice-only data are read.

## Foundry lead review
- Pending independent review.

## Azure black belt review
- Pending independent review.

## Known limitations
- PTU pricing remains explicitly `UNVERIFIED` for generic ARM deployment SKUs.
- Cosmos autoscale is listed as unsupported until the retail-meter mapping is
  verified.
- Environment comparisons cannot materialize unresolved parameter values.

## Deferred issues with rationale
- Runtime or control-plane budget discovery for `FND-COST-001` was not added to
  the shared Azure adapter here to avoid overlapping the concurrent runtime
  phases.

## Compatibility matrix
- Offline ARM-only estimate: supported
- Live retail price refresh: supported
- Offline cached retail estimate: supported
- Unsupported or stale price reporting: supported

## Artefacts for next phase
- `.foundry-doctor/cache/prices.json` schema is established for later reuse.

## Exact next-phase prerequisites
- Verify generic PTU retail-meter mapping before treating PTU estimates as
  supported.
- Decide whether Cosmos autoscale entry-price + RU meter mapping should be
  promoted from unsupported to supported.

## Definition of Done audit
| DoD item | Status (met / unmet / requirement changed by ADR-NNN) | Current evidence |
|---|---|---|
| No cost finding fails CI by default | met | `FND-COST-001/002` use catalogue severities; command is informational only |
| Every estimate exposes assumptions and excluded costs | met | `docs/cost.md`; cost report fields for notes/excluded/unsupported |
| Stale or unavailable pricing is reported, not guessed | met | `internal/cost/prices`; `internal/cost/cost_test.go` |
| Currency and region handling are tested | met | `internal/cost/prices/prices_test.go` and live-query filtering |
