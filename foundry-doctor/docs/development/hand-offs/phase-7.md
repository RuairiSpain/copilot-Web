# Phase 7 hand-off

Status: **post-merge gate loop 1 (Go 1.26.9, 2026-10-19): both reviewers REJECT; blockers fixed in the same loop, pending loop 2
re-review.** Fixed: the price-cache JSON schema is delivered (`schemas/price-cache.schema.json`, with a test); cached records are
untrusted and validated (finite non-negative price, matching currency, exact one-hour unit, Consumption type, no future
retrieval date); an hourly meter must be exactly `1 Hour` or `1/Hour`; stale pricing is tested at engine level, shows the
oldest retrieval date, marks each stale line and flags the console total; APIM Premium with more than one unit is not priced
(later units cost less and that meter is unverified); `samples/cost-fixed` now yields an estimate (parameters file and a
Foundry account) and a real-Bicep end-to-end test proves it; `--hours-per-month` is bounded to 0..744; region display names
with spaces are normalised.
Fix log: `docs/development/reviews/phase-7-fixlog-loop1.md`. Decisions and deferrals: ADR-025.

## Delivered
- Cost-estimation engine under `internal/cost` (PRD section 15 packages: `internal/cost`, `catalog`, `prices`, `report`) and a new
  shared extraction package `internal/cost/armcost` used by both the engine and the COST rules.
- `foundry-doctor cost` with console, json and markdown output.
- Every deployed resource is classified as Estimated, Excluded (consumption-based or not modelled) or Unsupported (fixed cost not
  priced, with the reason). Literal-false conditions are honoured; non-literal conditions and copy loops are unsupported.
- Foundry deployment SKUs: the full documented deployment-type table, matched case-insensitively; PTU deployments listed with PTU
  count, type and region; unknown, missing or unresolved SKUs listed as unsupported.
- Cosmos DB: throughput on `throughputSettings`, SQL databases and containers (`options.throughput`); region and region count from
  the parent account; multi-region write, free tier, zone redundancy, autoscale and other Cosmos APIs listed as unsupported.
- Search: S1 meter corrected to `Standard S1 Unit`; confidential compute and HighDensity unsupported. APIM: `additionalLocations`
  units listed; Consumption excluded before the capacity check.
- Retail Prices API client: pinned api-version, quoted currency, `priceType eq 'Consumption'`, primary-meter preference, tiered and
  expired rows dropped, ambiguity refused, returned currency verified, same-origin paging with a 20-page cap, no redirects; cache
  schema v2; live price kept when the cache cannot be written; cache fallback with a visible notice.
- FND-COST-001 (offline budget checks; threshold range check added), FND-COST-002 and FND-COST-004 in `internal/rules/cost`.
  `Register()` also ships FND-COST-003, a Phase 8 rule implemented early (ADR-025 item 9).

## Changed contracts
- `pkg/sdk.ARMResource` carries `SKU map[string]any` next to `SKUName`. `SKUName` is now derived from the substituted
  `SKU["name"]` (parameter file and module parameters), so the two always agree (`internal/armmodel`).
- Price cache schema version 2 (keys include api-version, price type, currency, hourly flag); a v1 cache is ignored.
- Default price cache path is `<--dir>/.foundry-doctor/cache/prices.json` (was relative to the working directory).
- `--currency` must be a three-letter code.

## Rule catalogue changes
- FND-COST-001 version 3 -> 4: phases `['2','7']` -> `['2']`, inputs drop `control-plane`, evidence states the control-plane Budgets
  API list is deferred (ADR-025) and adds the 0-1000 threshold check, test `control-plane-unavailable` removed, positive test
  `budget-threshold-out-of-range` added. Behaviour change: new finding key `budget-threshold-out-of-range`.
- FND-COST-002 version 3 -> 4: inputs drop `azure.yaml`; evidence says compiled template only, case-insensitive deployment types,
  substituted `sku.name`; `fix` is a Bicep deployment; first source re-verified 2026-10-18 with the full deployment-type table.
- FND-COST-004 version 2 -> 3: `fix` uses `--environment` (the flag that exists); evidence aligned with what is delivered (low = high,
  unsupported list, shared extraction); SQL database and container API versions added; Retail Prices API source verified
  2026-10-18; Cosmos pricing page source added; new finding keys `incomplete:search-meter`, `incomplete:apim-additional-locations`,
  `incomplete:cosmos-region`, `incomplete:cosmos-account-billing`, `incomplete:cosmos-api-type`, `incomplete:deployment-sku`.
- FND-COST-003: unchanged (Phase 8 owns its semantics).
- Compared with Phases 1 and 2: Phase 7 changed the behaviour of FND-COST-001 (threshold check), FND-COST-002 (all documented PTU
  types case-insensitively via the shared table; parameter-resolved `sku.name` now seen) and implemented FND-COST-004.
- `docs/rule-catalog.md` was updated by hand (FND-COST-001 phase `2`, FND-COST-002 source date `2026-10-18`); `docs/overlap-analysis.md`
  needs no change. **Regeneration with `go run ./cmd/rulecatalog generate-docs` and `generate-overlap` must be re-run with Go.**

## Commands implemented
- `foundry-doctor cost` (`--environment`, `--compare`, `--currency`, `--hours-per-month`, `--offline`, `--price-cache`, `--format`, `--out`).

## Tests executed and results
Run on Go 1.26.9: `go build`, `go vet`, `gofmt -l`, `go test ./...` and the catalogue and secret checks pass. The real-Bicep
sample test (`go test -tags spike -run TestCostFixedSampleEstimates ./cmd/foundry-doctor` with `BICEP_PATH` set to Bicep 0.48.1)
passes and was checked to fail against the original sample. CI runs `-race`, staticcheck and govulncheck. The earlier "not run"
statements are withdrawn.
Added in the gate: hourly-unit table; rejected cache records (negative, NaN, monthly or scaled unit, reservation, future date);
engine stale, mixed-age and tampered-cache tests; Premium capacity 3; console stale and incomplete flags; price-cache schema test;
region normalisation; hours-per-month bounds.

## Security and privacy review
- Public, unauthenticated Retail Prices API only; no credentials, tokens or tenant data are sent. Transport mitigations and the
  deferred `internal/azure` allow-list registration are recorded in ADR-025 item 3.
- Cost output contains resource names, types, regions and public prices only.

## Foundry lead review
- Round 1: REJECT. Fixes applied in loop 1, pending re-review.

## Azure black belt review
- Round 1: REJECT. Fixes applied in loop 1, pending re-review.

## Known limitations
- PTU, Cosmos autoscale, multi-region write, zone redundancy, free tier, APIM additional regions, Search CC and HighDensity are listed
  as unsupported, not priced.
- Meter names other than those seen in the recorded response are UNVERIFIED (ADR-025 item 8); the recorded responses come from a
  search-engine snapshot, not a direct API call.
- Low equals high (ADR-025 item 7).
- Unknown resource types are excluded, not priced; totals are "covered resources only".

## Deferred issues with rationale
- FND-COST-001 control-plane budget list and Azure Advisor cost recommendations: need a new allow-listed `internal/azure`
  operation and permissions (ADR-025 items 1 and 10).
- Registering the Retail Prices origin in the `internal/azure` allow-list with a replay test (ADR-025 item 3), owner of `internal/azure`.
- FND-COST-001 review N7 (contactRoles scope, labelling the Forecasted requirement as product opinion in finding text): not changed.

## Compatibility matrix
- Offline ARM-only estimate: implemented, untested in this loop
- Live retail price refresh: implemented, untested in this loop (recorded-response tests only)
- Offline cached retail estimate: implemented, untested in this loop
- Unsupported, ambiguous, wrong-currency and stale price reporting: implemented, untested in this loop
- Sovereign clouds: listed as unsupported

## Artefacts for next phase
- `internal/cost/armcost` (shared plan extraction), price cache schema v2, recorded Retail Prices responses.

## Exact next-phase prerequisites
- Run the gates listed above and regenerate the catalogue docs.
- Verify the UNVERIFIED meter names (ADR-025 item 8) with direct API calls and record them in `internal/cost/prices/testdata`.

## Definition of Done audit
| DoD item | Status (met / unmet / requirement changed by ADR-NNN) | Current evidence |
|---|---|---|
| No cost finding fails CI by default | met (re-reviewed in loop 2 pending) | COST severities are info/warning, default `--fail-on` is error (`internal/app/app.go`); `cost` returns exit 0 when it renders |
| Every estimate exposes assumptions and excluded costs | met (loop 1 fixes applied; re-review pending) | every resource is Estimated, Excluded or Unsupported (`internal/cost/cost.go` `extract`); line notes; `docs/cost.md`; `internal/cost/cost_test.go` |
| Stale or unavailable pricing is reported, not guessed | met (loop 1 fixes applied; re-review pending) | `internal/cost/prices` (Consumption only, ambiguity refused, currency verified); `prices_recorded_test.go` |
| Currency and region handling are tested | met (tests run on Go 1.26.9) | `TestCurrencyAndRegionHandling`, `TestCurrencyMismatchIsUnsupported`, `TestCurrencyMismatchIsRefused`, `TestRecordedSearchS1NonPrimary`, sovereign region case in `TestSearchAndAPIMEdgeCases` |

## Open non-blocking items (gate loop 1)
- Meter date (`effectiveStartDate`) is JSON-only, not in the console or Markdown line.
- Error text embeds the request URL; unverified meters (Search standard2 and storage optimized, APIM tiers, Cosmos `100 RU/s`) are
  still priced with an UNVERIFIED note and counted in the total; recorded fixtures are from a search-engine snapshot, not the API.
- No per-run memoisation or time budget for live calls; cache writes are not atomic; default cache sits under `--dir`.
- Foundry dependency costs (Cosmos, Storage, Search behind agents) and `serviceTier` billing are not named in the exclusions.
- `armcost` and `catalog` have only the small tests added here; tiered fixed meters are skipped rather than refused.
- Price and API-version claims need a Learn and live Retail Prices re-check (both unreachable from the review sandbox).
