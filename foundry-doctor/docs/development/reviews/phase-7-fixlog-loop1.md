# Phase 7 fix log, loop 1

Date: 2026-10-18. Fixer: go-implementer. Reviews: `phase-7-foundry-lead-round1.md` (FL), `phase-7-azure-black-belt-round1.md` (BB).
No Go toolchain: nothing was compiled or tested in this loop. Decisions: ADR-025.

| Finding | Status | Files |
|---|---|---|
| FL-B1 SKUName from raw sku.name | FIXED | internal/armmodel/armmodel.go, internal/armmodel/sku_test.go, pkg/sdk/sdk.go (comment), internal/rules/cost/cost_phase7_test.go, internal/cost/cost_test.go |
| FL-B2 deployment SKUs incomplete / case-sensitive / silent | FIXED | internal/cost/armcost/armcost.go, internal/cost/cost.go, internal/rules/cost/cost.go, docs/cost.md, tests |
| FL-B3 Cosmos coverage and region | FIXED (per-region lines from parent account; unresolved -> unsupported) | internal/cost/armcost/armcost.go, internal/cost/cost.go, internal/rules/cost/cost.go, tests |
| FL-B4 price selection | FIXED | internal/cost/prices/prices.go, internal/cost/prices/prices_recorded_test.go, testdata |
| FL-B5.1 FND-COST-001 catalogue vs code | ADR-025 (control-plane deferred) + catalogue v4 | rules/catalog/cost/FND-COST-001.yaml, docs/rule-catalog.md |
| FL-B5.2 FND-COST-002 azure.yaml claim | FIXED (catalogue v4, Bicep fix) | rules/catalog/cost/FND-COST-002.yaml, docs/rule-catalog.md |
| FL-B5.3 FND-COST-004 `--env` and evidence | FIXED (catalogue v3) | rules/catalog/cost/FND-COST-004.yaml |
| FL-B5.4 hand-off catalogue claims | FIXED | docs/development/hand-offs/phase-7.md |
| FL-B6 gates / TestAnnotate / DoD | NOT-FIXED (no toolchain: gates must be run by the maintainer; TestAnnotate is owned by the Phase 6 fixer). Hand-off corrected: no run claimed, assertion described correctly, DoD table follows the PRD | docs/development/hand-offs/phase-7.md |
| FL-N1 PTU note content | FIXED | internal/cost/cost.go |
| FL-N2 pay-per-token capacity not priced | FIXED (docs and reason text; quotas page not cited) | docs/cost.md, internal/cost/cost.go |
| FL-N3 Search counts default | ADR-025 item 5 (template reference gives no default; absence is unsupported with that reason) | internal/cost/cost.go |
| FL-N4 APIM additionalLocations; Consumption order | FIXED | internal/cost/cost.go, internal/rules/cost/cost.go |
| FL-N5 empty reason | FIXED | internal/cost/cost.go |
| FL-N6 cache write failure; cache path | FIXED | internal/cost/prices/prices.go, internal/app/cost.go, cmd/foundry-doctor/cost.go |
| FL-N7 range | ADR-025 item 7 + docs | docs/cost.md, internal/cost/cost.go |
| FL-N8 Cosmos multi-region | FIXED | internal/cost/cost.go |
| FL-N9 sample values | FIXED (sample marked illustrative; Cosmos shape corrected) | samples/cost-fixed/infra/main.bicep |
| FL-N10 page cap | FIXED | internal/cost/prices/prices.go |
| BB-B1 priceType / cheapest | FIXED | internal/cost/prices/prices.go, tests |
| BB-B2 currency verification and quoted form | FIXED (format validation; returned currency verified; non-USD note). Supported-currency list not hard-coded | internal/cost/prices/prices.go, internal/app/cost.go, internal/cost/cost.go, tests |
| BB-B3 paging / transport / api-version | FIXED in client (pinned version, same-origin paging, cap, no redirects); allow-list registration ADR-025 item 3 | internal/cost/prices/prices.go, docs/decisions/ADR-025-... |
| BB-B4 conditions and copy loops | FIXED | internal/cost/armcost/armcost.go, internal/cost/cost.go, internal/cost/cost_test.go |
| BB-B5 Search meters, CC, HighDensity | FIXED (S1 corrected; CC/HighDensity unsupported; meter evidence recorded) | internal/cost/catalog/catalog.go, internal/cost/cost.go |
| BB-B6 Cosmos structure | FIXED | as FL-B3 |
| BB-B7 APIM additional regions; meter names | FIXED (additional units listed; meter evidence per name; PremiumV2 unsupported) | internal/cost/catalog/catalog.go, internal/cost/cost.go |
| BB-B8 unknown resources invisible | FIXED (every resource listed; totals labelled covered resources only; ADR-025 item 6) | internal/cost/cost.go, internal/cost/report/report.go |
| BB-B9 DoD evidence / hand-off | FIXED (hand-off rewritten; recorded and golden tests added; FND-COST-003 disclosed; FND-COST-001 deferral ADR); red package: NOT-FIXED here (owned by Phase 6 fixer) | docs/development/hand-offs/phase-7.md, ADR-025 |
| BB-N1 cache fallback after not-found | FIXED (notice) | internal/cost/prices/prices.go |
| BB-N2 masked network error | FIXED | internal/cost/prices/prices.go |
| BB-N3 cache key, schema, gitignore | FIXED | internal/cost/prices/prices.go, .gitignore, docs/cost.md |
| BB-N4 future effectiveStartDate | FIXED (dropped; reported as not effective yet). Report header date: NOT-FIXED (per-line priceDate only) | internal/cost/prices/prices.go |
| BB-N5 low = high | ADR-025 item 7 | docs/cost.md |
| BB-N6 rounding | FIXED (documented; rounding test). Zero-minor-unit currencies: documented, not specially handled | docs/cost.md, internal/cost/cost_test.go |
| BB-N7 FND-COST-001 threshold / contactRoles / opinion label | PARTLY FIXED (threshold range check); contactRoles scope and finding-text label NOT-FIXED (scope is not visible offline) | internal/rules/cost/cost.go |
| BB-N8 control-plane budgets | ADR-025 item 1 | |
| BB-N9 rule and engine diverge | FIXED (shared armcost) | internal/rules/cost/cost.go |
| BB-N10 sovereign clouds | FIXED | internal/cost/armcost/armcost.go, internal/cost/cost.go |
| BB-N11 SDK note | FIXED (field comments) | pkg/sdk/sdk.go |
| BB-N12 Advisor | ADR-025 item 10 | |

Compile risks to check first: `internal/cost/prices/prices.go` (rewritten), `internal/cost/cost.go` (rewritten),
`internal/cost/armcost` (new), `internal/rules/cost/cost.go` eval004 (rewritten), report golden files (exact whitespace), gofmt alignment.

## Loop 2 (orchestrator, 2026-10-18)

- BB round 2 **B10** FIXED: `internal/cost/report/report.go:213` `fmt.Fprintln(w, "\n## Comparisons\n")` -> `fmt.Fprint(w, "\n## Comparisons\n\n")` (vet "redundant newline" check run by `go test`). Output bytes unchanged; `markdown.golden` unaffected. Repo-wide scan found no other occurrence.
