# Phase 7 review: Azure black belt (round 2)

Reviewer: `azure-black-belt` (independent; did not write the code). Date: 2026-10-18. Round 2 (after fix loop 1).
Inputs: `REVIEW_BRIEF.md`, round 1 (`phase-7-azure-black-belt-round1.md`), `phase-7-fixlog-loop1.md`, ADR-025, PRD section 15,
hand-off `phase-7.md`, and the code in the canonical copy (`/mnt/workspace/working/canon/foundry-doctor`).

Environment: there is no Go toolchain and no package-registry access, so `go build`, `go vet` and `go test` were not run. I checked
compile-correctness by reading imports, signatures, callers, test helpers and golden files. The shell has no network. Platform facts
were checked with web search and fetch. Per the orchestrator, a hand-off that honestly says the gates were not run is accepted for
this loop. The hand-off does say this.

## Round 1 findings: verification

| Id | Status | Evidence |
|---|---|---|
| B1 priceType / cheapest wins | **CLOSED** | `prices.RequestURL` adds `priceType eq 'Consumption'`. `Lookup` re-checks `item.Type`, drops `tierMinimumUnits != 0`, expired rows and future rows, prefers `IsPrimaryMeterRegion`, and returns `ErrAmbiguous` when more than one distinct price is left. Tests: `TestOnlyCurrentPrimaryConsumptionRowIsUsed` (DevTest, Reservation, tiered, non-primary and expired rows), `TestAmbiguousPricesAreRefused` (also checks that `Resolve` does not fall back to the cache). Matches the API doc: Reservation rows carry the term price even with `1 Hour` (https://learn.microsoft.com/rest/api/cost-management/retail-prices/azure-retail-prices). |
| B2 currency | **CLOSED** | The query now sends the quoted `currencyCode='EUR'` (documented form). Returned `currencyCode` is checked (`ErrCurrencyMismatch`, never replaced by the cache). `Cache.Lookup` refuses a record in another currency. `CostRequest.Validate` checks the ISO shape. A non-USD report carries the "reference values" note. Tests: `TestRequestURLPinsVersionCurrencyAndPriceType`, `TestCurrencyMismatchIsRefused`, `TestCurrencyMismatchIsUnsupported`, `TestCachedRecordInOtherCurrencyIsIgnored`, `TestCurrencyAndRegionHandling`. Not done: a hard-coded supported-currency list. Acceptable, because a mismatch fails closed. |
| B3 paging / transport / api-version | **CLOSED** (allow-list registration deferred by ADR-025 item 3 with mitigations) | `api-version=2023-01-01-preview` is pinned. `nextAllowed` only follows a link on the same scheme, host and port. `MaxPages=20`. `CheckRedirect` returns `ErrUseLastResponse`, so a 3xx is an error. Bodies are capped at 4 MiB. Tests: `TestPagingFollowsSameOriginOnly` (two pages plus a foreign host), `TestPageCap`, `TestRedirectIsNotFollowed`. The ADR names an owner and a phase for registering the origin in the `internal/azure` allow-list. That is acceptable for an unauthenticated public endpoint. |
| B4 conditions / loops | **CLOSED** | `armcost.PresenceOf` is shared by the engine and the rules. A literal false is skipped and not listed. A non-literal condition or a copy loop is unsupported and marks the estimate incomplete. Test: `TestConditionsAndLoops`. |
| B5 Search meters | **CLOSED** | `standard` now maps to `Standard S1 Unit`. This matches the public endpoint (https://prices.azure.com/api/retail/prices?$filter=serviceName%20eq%20%27Azure%20Cognitive%20Search%27). `computeType: Confidential` and `hostingMode: HighDensity`, or either one left as an expression, are unsupported. Unverified meter names carry an UNVERIFIED line note (`MeterSpec.Verified`). A missing replica or partition count is unsupported, never assumed to be 1. |
| B6 Cosmos structure | **CLOSED** | `options.throughput` on SQL databases and containers is read. Region and region count come from the parent account (`FindParent`, `ReadCosmosAccount`), with one line per region. Multi-region write, free tier, zone redundancy, autoscale, serverless, other API types and unresolved account fields are unsupported or excluded, each with a reason. This matches the pricing page, which bills RU/s per region (https://azure.microsoft.com/pricing/details/cosmos-db/standard-provisioned/). Tests: `TestCosmosDatabaseContainerAndRegions`, `TestCosmosAccountBillingModesAreUnsupported`. |
| B7 APIM | **CLOSED** | Each `additionalLocations` entry is listed as unsupported with its unit count and region, and the estimate is marked incomplete. `PremiumV2` is unmapped and therefore unsupported. Meter evidence is recorded per name. Recorded fixture: `apim-basicv2-eastus.json`. |
| B8 unknown resources invisible | **CLOSED** (design per ADR-025 item 6) | Every deployed resource is Estimated, Excluded ("resource type not modelled ...") or Unsupported. Totals, comparisons and the advisory text say "covered resources only". I accept that unknown types are Excluded rather than marked incomplete, because the label is explicit in all three renderers. |
| B9 DoD evidence / hand-off | **CLOSED** for evidence and disclosure | The hand-off was rewritten. It discloses FND-COST-003 and that the gates were not run. The DoD table no longer claims runs. Recorded-response and golden tests were added. The FND-COST-001 control-plane deferral is recorded in ADR-025 item 1, with catalogue v4 changing phases to `['2']`. The red `cmd` package belongs to the Phase 6 fixer and is disclosed. |
| N1 cache after not-found | CLOSED | `Resolve` adds the notice "live API no longer lists this meter; cached price shown". |
| N2 masked network error | CLOSED | `TestLiveFailureFallsBackToCacheWithNotice` checks that the live cause (503) is kept. |
| N3 cache key / schema / gitignore | CLOSED | The key includes api-version, priceType, currency and the hourly flag. A v1 cache is ignored. `.gitignore` has `.foundry-doctor/cache/`. |
| N4 effective dates | PARTIALLY | Future and expired rows are dropped. There is still no report-header price date; each line has `priceDate`. Acceptable. |
| N5 low = high | CLOSED | ADR-025 item 7 plus `rangeNote` in the report. |
| N6 rounding | CLOSED | Documented, with `TestRoundingPerLine`. |
| N7 FND-COST-001 details | PARTIALLY | The 0-1000 threshold check was added. The contactRoles scope and the opinion label are not changed. Disclosed in the hand-off. Non-blocking. |
| N8 control-plane budgets | CLOSED (deferred) | ADR-025 item 1, with the permission and the "uncertain, never FAIL" rule recorded. |
| N9 rule and engine diverge | CLOSED | eval004 uses `armcost`. Test: `TestCost004CosmosSharedExtraction`. |
| N10 sovereign clouds | CLOSED | `armcost.SovereignRegion` gives a reason that quotes the Commercial-only statement in the API doc. |
| N11 SDK note | CLOSED | `pkg/sdk` field comments. |
| N12 Advisor | CLOSED (deferred) | ADR-025 item 10. |

## Compile-correctness reading

These all look correct on reading:
- `prices.go`: imports, the `next, err =` reassignment in the paging loop, and the named results in `Resolve`.
- `cost.go`: all imports are used, and `catalog.MeterSpec.Verified` exists.
- `armcost`: imports only `pkg/sdk`, so there is no import cycle with `internal/cost` or `internal/rules/cost`.
- The rules package uses `armcost`.
- Test helpers (`run`, `kind`, `pol`, `model`, `budget`, `notification`, `byName`) exist in the same test packages. There are no duplicate top-level identifiers in the four touched packages.
- `app.CostRequest.Validate` and `cmd` flags agree.
- The console golden file matches `console()` byte for byte.
- The markdown golden file matches the bytes `markdown()` would write.

The test logic is also consistent with the code:
- Price-server query matching works with `url.Values.Encode`: `+` for spaces, `%27` for quotes, `%2F` for `/`.
- The expected totals use the same per-line rounding as the engine.

## New findings

### Blocking

**B10 `go test ./internal/cost/report` will fail vet: `Fprintln` with a redundant trailing newline.**
- Location: `internal/cost/report/report.go:213` (in `markdown`), `fmt.Fprintln(w, "\n## Comparisons\n")`. This line is new in loop 1.
- Why it matters: `go test` runs the vet `printf` analyzer by default. That analyzer reports "fmt.Fprintln arg list ends with redundant newline" and the package then fails under `go test` (golang/go#49322, https://github.com/golang/go/issues/49322: "Those packages fail to build because cmd/go ... considers this vet problem"). The new golden test `TestRenderGolden` is in this package, so the PRD golden-report test can never pass.
- Fix: replace it with `fmt.Fprint(w, "\n## Comparisons\n\n")`. The output is the same, so `markdown.golden` stays valid.

### Non-blocking

- **N13** Copy-looped Foundry model deployments are common (Bicep `for` over a deployments array). Even pay-per-token ones are marked unsupported, so the estimate is incomplete and `--compare` is never comparable on such templates. Consider: a looped resource whose SKU is a literal pay-per-token type could be listed as Excluded, because its instance count does not change the fixed-capacity total.
- **N14** A SQL database that sets `options.throughput` and also has a `throughputSettings` child would be priced twice. This is unusual. Add a dedupe on the database name, or a test that documents the behaviour.
- **N15** Child resources whose compiled `name` is still an expression (`[format('{0}/{1}', ...)]`, the usual Bicep `parent:` output when the account name is a parameter without a value) always get "parent Cosmos DB account is not in the compiled plan". The behaviour is correct (no guess), but `docs/cost.md` should say that a parameter file is needed for Cosmos estimates.
- **N16** ADR-025 item 8 marks APIM `Developer Unit` and `Standard Unit` as verified, but the recorded fixtures only contain `Basic v2` (APIM) and Search rows. Add the APIM Developer and Standard rows to `testdata`, or downgrade those two names to UNVERIFIED.
- **N17** The recorded responses come from a search-engine snapshot, not a direct API call. The hand-off says so. The maintainer's first networked run should re-record them (already listed as a next-phase prerequisite).

## Verdict

All nine round-1 blockers are closed or closed by ADR. The price-selection, currency, paging, condition, Search, Cosmos, APIM and coverage fixes are correct against the Retail Prices API documentation and the public endpoint. One new blocking defect remains: B10 (one line, vet failure in the golden-report package). Once B10 is fixed, this reviewer expects to approve without another full round, subject to the maintainer's gate run.

VERDICT: REJECT
