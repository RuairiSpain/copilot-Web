# Phase 7 review: Foundry lead, round 2

Reviewer: `foundry-lead`. I did not write any of the code under review. Date: 2026-10-18. Round 2 (after fix loop 1).

Inputs read:
- `REVIEW_BRIEF.md`
- `docs/development/reviews/phase-7-foundry-lead-round1.md`
- `docs/development/reviews/phase-7-fixlog-loop1.md`
- `docs/decisions/ADR-025-phase-7-cost-decisions-and-deferrals.md`
- `docs/development/hand-offs/phase-7.md`
- PRD section 15

Code read:
- `internal/cost/**`, including the new `internal/cost/armcost`
- `internal/rules/cost/**`
- `internal/armmodel/armmodel.go` and `sku_test.go`
- `pkg/sdk/sdk.go`
- `internal/app/cost.go` and `cost_test.go`
- `cmd/foundry-doctor/cost.go`
- `rules/catalog/cost/FND-COST-00{1,2,4}.yaml`
- `docs/cost.md`, `docs/rule-catalog.md`
- `samples/cost-fixed`

Environment constraints, which are not defects of the code:
- There is no Go toolchain or package registry in this sandbox, so nothing was built or tested.
- I checked compile-correctness by reading: imports, signatures, callers, duplicate test identifiers, type-switch variable use, and the golden files against the renderer.
- Platform claims were checked with web fetch and search where possible.
- As instructed for this loop, a hand-off that states the gates were not run is the accepted treatment and is not treated as a blocker.

## 1. Round 1 findings: status

| ID | Status | Evidence |
|---|---|---|
| B1 `SKUName` from the raw `sku.name` | **CLOSED** | `internal/armmodel/armmodel.go` now sets `SKUName` from the normalised `res.SKU["name"]`. `sku_test.go` covers a parameter file, a module parameter and an unresolved value. `cost_phase7_test.go` `TestCost002ParameterResolvedPTU` checks a PTU SKU that comes from a parameter (finding), `GlobalStandard` (pass) and an unresolved value (`unresolved-expression`). `armcost.SKUName` falls back to `SKU["name"]`. |
| B2 Deployment SKUs incomplete, case-sensitive and silent | **CLOSED** | `armcost.deploymentSKUs` holds all 9 documented SKU codes and matches them case-insensitively. The codes, scopes and billing classes match the comparison table at https://learn.microsoft.com/azure/foundry/foundry-models/concepts/deployment-types (re-read 2026-10-18). The cost engine excludes pay-per-token, batch and developer types. PTU deployments are unsupported with the PTU count from `sku.capacity`, the type and scope, and the parent account region. A missing, unresolved or undocumented SKU is unsupported. Tested in `TestDeploymentSKUsAreClassifiedCaseInsensitively`. `docs/cost.md` is updated. |
| B3 Cosmos DB coverage and region | **CLOSED** | `armcost.IsCosmosThroughputType`, `CosmosThroughput` and `ReadCosmosAccount` are shared by the engine, `eval002` and `eval004`. `options.throughput` on SQL databases and containers is read. The region comes from the parent account's `locations`, with one line per region. Multi-region write, free tier, zone redundancy, autoscale, other Cosmos APIs and unresolved accounts are unsupported. Tested in `TestCosmosDatabaseContainerAndRegions` and `TestCosmosAccountBillingModesAreUnsupported`. |
| B4 Price selection | **CLOSED** | The filter now has `priceType eq 'Consumption'`, and the client also checks `type`. Tiered, expired and not-yet-effective rows are dropped. Primary meter rows are preferred, and a non-primary row is noted. Two or more distinct prices give `ErrAmbiguous`, which is never replaced by a cached price. The returned currency is checked. This matches https://learn.microsoft.com/rest/api/cost-management/retail-prices/azure-retail-prices. Tested in `TestOnlyCurrentPrimaryConsumptionRowIsUsed`, `TestAmbiguousPricesAreRefused`, `TestCurrencyMismatchIsRefused` and the recorded-response tests. |
| B5.1 FND-COST-001 control-plane claim | **CLOSED** | Catalogue entry v4: `phases: ['2']`, `inputs: [bicep-arm]`, and the evidence and testability text cite the ADR-025 item 1 deferral. The `control-plane-unavailable` test is removed and the threshold-range check is added (`TestCost001ThresholdRange`). |
| B5.2 FND-COST-002 `azure.yaml` claim | **CLOSED** | Catalogue entry v4: `inputs: [bicep-arm]`. The evidence says "azure.yaml deployments are not read (ADR-025)", and the `fix` is now Bicep. |
| B5.3 FND-COST-004 `--env` and overstated evidence | **CLOSED** | Catalogue entry v3: the `fix` is `foundry-doctor cost --environment prod --compare dev`, which matches the flag in `cmd/foundry-doctor/cost.go`. The evidence now says low equals high and lists what is unsupported. |
| B5.4 Hand-off catalogue claims | **CLOSED** | The hand-off lists every catalogue change and version increase, and says what changed compared with Phases 1 and 2. It discloses that `docs/rule-catalog.md` was edited by hand and must be regenerated. |
| B6 Gates, `TestAnnotate` and the DoD table | **CLOSED for this loop (accepted treatment)** | The hand-off now says that no build, test, vet, gofmt or `verify-phase.sh --strict` run was made, and that the maintainer must run them. The `TestAnnotate` assertion is described correctly, and ownership is assigned to the Phase 6 fixer (the test is now in `cmd/foundry-doctor/annotate_test.go`). The DoD rows say "met in code, pending test run". The maintainer's strict gate run is still required before release. |
| N1 PTU note content | CLOSED | `cost.go` `deployment`: PTU count, type and scope, region, hourly billing per PTU, reservations not modelled. Matches https://learn.microsoft.com/azure/foundry/openai/concepts/provisioned-throughput-billing. |
| N2 Pay-per-token `sku.capacity` | CLOSED | The reason text and `docs/cost.md` say the capacity is a throughput allocation and is not priced. No unit is claimed, so no citation is needed. |
| N3 Search counts default | CLOSED (ADR-025 item 5) | An absent count is unsupported, with a reason, and no default of 1 is assumed. This is the conservative choice. |
| N4 APIM `additionalLocations` and Consumption order | CLOSED | `cost.go` `apim`: Consumption is excluded before the capacity check. Each additional location is listed with its units and region. Tested. |
| N5 Empty reason | CLOSED | Every unsupported path sets a reason, including `sku %q has no mapping ...`. |
| N6 Cache write failure and cache path | CLOSED | `Resolve` keeps the live price and adds a notice (`TestCacheWriteFailureKeepsLivePrice`). The default cache path is under `--dir` (`CostRequest.Validate`, `internal/app/cost_test.go`). |
| N7 Range | CLOSED (ADR-025 item 7) | The `rangeNote` is shown in the report and in `docs/cost.md`. |
| N8 Cosmos multi-region | CLOSED | One line per account region. Multi-region write is unsupported. |
| N9 Sample values | CLOSED | `samples/cost-fixed/infra/main.bicep` is marked illustrative, and the Cosmos sample now has the correct account and database shape. |
| N10 Page cap | CLOSED | `MaxPages = 20` (`TestPageCap`). Same-origin `NextPageLink` only, and redirects are refused. |

## 2. New findings

### Blocking

**B7. `go test ./internal/cost/report` will fail on a vet printf error, so the new golden tests never run.**
- **Where:** `internal/cost/report/report.go:213`, `fmt.Fprintln(w, "\n## Comparisons\n")`.
- **Why:** `go test` runs a subset of vet checks that includes `printf`. That check reports `fmt.Fprintln arg list ends with redundant newline` for a string literal argument that ends in `\n`, and cmd/go treats the report as a failure. Source: https://github.com/golang/go/issues/49322 ("Those packages fail to build because cmd/go considers this vet problem"); the same wording is shown for literal arguments in https://github.com/golang/go/issues/57059.
- **Impact:** This line was added in this loop; it was not in the round-1 code. It breaks the package that holds the new `TestRenderGolden` and `TestRenderJSONRoundTrip`, which are the golden-report evidence for the PRD section 15 testing strategy. It would also fail the `go vet` gate.
- **Fix:** `fmt.Fprint(w, "\n## Comparisons\n\n")`. The output is byte-identical, so `testdata/markdown.golden` stays valid. I checked the golden file against the renderer: blank line, `## Comparisons`, blank line, list.

Apart from B7, I found nothing that would fail to compile:
- Imports are all used.
- `armcost.SKUCapacity` returns 3 values and every call site handles 3.
- Variables bound in type switches are used in at least one branch.
- `client`, `keys`, `paramModel`, `estimate`, `cosmosAccount` and `typeDeployment` are not declared twice in their test packages.
- `byName` comes from `params_test.go` in the same external test package (`armmodel_test`).
- `*armmodel.Model` satisfies `sdk.ARMModel`.
- Both golden files match the renderer byte for byte.

I also traced each new test against the code and expect all of them to pass, including `TestPageCap` (exactly 20 fetches) and `TestCacheWriteFailureKeepsLivePrice` (ENOTDIR is not `ErrNotExist`, but the live answer returns before the cache error is used).

### Non-blocking

- **N11. Misleading reason when the account name is unresolved.** `armcost.FindParent` matches only on literal names. If the Cosmos account name is still an expression (for example `uniqueString`) and no parameter file binds it, the engine says "parent Cosmos DB account is not in the compiled plan". The account is in the plan; its name is unresolved. Change the reason to "parent Cosmos DB account not resolved (name unresolved or not in the plan)".
- **N12. `eval004` and the engine still differ on unresolved Search and APIM `sku.name`.** The engine reports them as unsupported. `eval004` counts the resource as supported and raises no `incomplete:*` key for the SKU. Add an `incomplete:search-sku` / `incomplete:apim-sku` key so that `check` and `cost` agree, as ADR-025 intends.
- **N13. The illustrative sample prices nothing offline.** It uses `location = resourceGroup().location`, so the Search, APIM and Cosmos regions stay unresolved and every fixed-cost line is unsupported. That is honest, but the sample does not demonstrate a priced line. Use a literal region in the sample or document the limitation.
- **N14. Hand-edited catalogue docs.** `docs/rule-catalog.md` was edited by hand. This is disclosed; the maintainer must run `go run ./cmd/rulecatalog generate-docs` and `generate-overlap` before the catalogue sync test is trusted.
- **N15. Weak provenance for "verified" meter names.** The verified meter names come from a search-engine snapshot of the public endpoint, not a direct API call (`testdata/README.md`, ADR-025 item 8). This is disclosed. Re-capture the responses directly before release.
- **N16. Search lines in the recorded data are mostly non-primary.** The recorded data shows `Standard S1 Unit` rows marked non-primary in several regions. Real Search lines may therefore often carry the non-primary note. This is acceptable; record it in `docs/cost.md` so users are not alarmed.

## 3. Unsupported assumptions remaining

- The UNVERIFIED meter names listed in ADR-025 item 8: Search `Standard S2 Unit` and `Storage Optimized L1/L2 Unit`; APIM `Basic Unit`, `Premium Unit`, `Isolated Unit` and `Standard v2 Unit`; Cosmos `100 RU/s`. Each one is labelled UNVERIFIED in the code and docs and can only produce "no retail price", never a guessed price, so this is acceptable for Phase 7.
- Cosmos autoscale billing (10-100% of the maximum per hour) and per-region RU/s billing are cited to the azure.microsoft.com pricing page. I did not re-fetch it in this round. Both only lead to unsupported or per-region lines, never to a guessed figure.

## 4. Missing tests

- Engine: a Cosmos account whose name is an expression (N11).
- `eval004`: an unresolved Search or APIM `sku.name` (N12).
- CLI: a `cost` run against `samples/cost-fixed` with a recorded price cache, to cover the end-to-end golden output.

## 5. Verdict

All round-1 blocking findings are closed with code, test or catalogue evidence. B6 is closed for this loop under the accepted treatment, and the maintainer's strict gate run is still required. One new blocking issue was introduced by the fixes: **B7** (`report.go:213`, a vet printf failure that fails `go test` for the package holding the new golden tests). The fix is one line, and no golden file changes.

Re-review needs: B7 fixed. The non-blocking items N11-N16 can be tracked in the hand-off.

VERDICT: REJECT
