# Power BI report build kit — org / cost-center / individual-user

## Why this is a kit, not a `.pbix`

A `.pbix` is Power BI Desktop's own compressed binary format — its data
model is a proprietary VertiPaq store, not text. There's no reliable way to
author or edit one outside Power BI Desktop itself, which isn't available
in this environment, and I won't hand you a hand-crafted binary I can't
verify opens correctly. Everything in this kit is plain text (DAX, Power
Query M, and a visual-by-visual guide) that you paste into your **existing**
`Citadel-Governance-Hub-Usage-Dashboard-V1.1-Incremental.pbix` — already
connected, already styled — rather than building a report from zero.

Expect **15–30 minutes** of paste-and-arrange work in Power BI Desktop, not
a design exercise.

See `guides/power-bi-dashboard.md` (this repo's Power BI guide) for the
narrative walkthrough and rationale behind these pages — this folder is
the paste-in artifact, that guide is the explanation.

## Files

| File | What it's for | Where it goes |
| --- | --- | --- |
| `01-measures.dax` | Every DAX measure this kit uses, organized by category | Home → New Measure, one at a time (or select-all paste into the Measure editor if you prefer) |
| `02-rls-roles.dax` | The three RLS role filter expressions | Modeling → Manage roles |
| `03-supporting-tables.m` | Two small reference tables the measures depend on (`Budgets`, `AuthorizedCostCenters`) | Home → Transform Data → New Source → Blank Query, paste each into Advanced Editor |
| `04-report-build-guide.md` | Exact page/visual layout for the three new pages | Follow after the above three are in place |
| `05-cost-avoidance-column.m` | *(optional)* Premium-model cost comparison for the Org page's Cost Avoidance tile (PRD FR-022) | Custom column on the existing `llm-usage-container` query |
| `06-ptu-allocation-columns.m` | *(only if you have PTU/reserved-capacity deployments)* Two columns needed to allocate fixed PTU cost proportionally, matching the accelerator's own existing method | Two custom columns on the existing `llm-usage-container` query |
| `07-data-quality-measures.dax` | Price-resolution rate and cost-center coverage rate — measures the Vision doc's own ">95% attribution accuracy" success criterion directly | Home → New Measure |
| `08-payg-ptu-breakeven-column.m` + `08-payg-ptu-breakeven-measures.dax` | Monthly PAYG-vs-PTU breakeven per model, using the last 1-2 months' actual token volume | Custom column, then measures |
| `09-adoption-measures.dax` | Active-app trend, new-user growth, cost-center coverage — the Vision doc's "adoption" analytics category | Includes one calculated **table** (`FirstSeenByApp`, via Modeling → New table) plus measures |
| `10-heatmap-moving-average.dax` | Day-of-week × hour-of-day traffic heatmap, 7-day moving averages on spend/tokens | 4 calculated **columns** + 2 measures |
| `11-peak-off-peak-recommendation.dax` | Peak/off-peak PTU-vs-PAYG recommendation — read-only, no routing change (see `guides/advanced-ptu-routing.md`) | 1 calculated column + measures, needs files 06/08/10 first |
| `12-prompt-chain-efficiency-column.m` + `12-prompt-chain-efficiency-measures.dax` | Rapid-succession-request detection — **read the visibility note in the .dax file before building the page**, this is individual behavioral data | Custom columns, then measures |
| `13-user-token-length-outliers.dax` | Is a user's typical prompt length outside their team's normal range (z-score vs. peer average) | Measures only, no new columns |
| `14-cost-center-cross-comparison.dax` | Is a cost center's windowed (30-day) spend outside the org's normal range vs. other cost centers | 1 calculated table + measures, needs `Date Only` from file 10 |
| `15-modified-zscore.dax` | Median/MAD outlier detection (Iglewicz & Hoaglin) — more statistically sound than files 13/14's standard z-score for skewed data | Measures only, additive — swaps into existing Pages 9/10 |
| `16-percentile-measures.dax` | P50/P90/P95/P99 for cost and tokens — standard practice in every LLM observability tool researched | Measures only, additive — no dependencies beyond the base table |
| `17-seasonality-aware-forecast.dax` | Month-end forecast weighted by historical day-of-week pattern instead of flat linear extrapolation | 1 calculated table + measures, needs `Day Of Week Number`/`Date Only` from file 10 |
| `18-failures-latency-source.md` + `18-failures-latency-measures.dax` | Failure rate, latency percentiles, backend-vs-gateway time split — **new data source (Log Analytics, not Cosmos)**, see the file for why | New Power BI query via the Azure Monitor Logs connector, then measures |
| `19-model-version-drift.dax` + `19-developer-role.m` | Detects when an app starts using a deployment it never used before; adds the `Developer` RLS role | 2 calculated tables + measures, plus a new supporting table |
| `20-deprecated-models.m` + `20-deprecated-models-measures.dax` | Flags active traffic on models past their deprecation/retirement date | New reference table (manually maintained) + measures |
| `21-trace-deep-link.md` | Click-through from a Power BI row to the correlated Application Insights trace — **read this before building**, the one-click straight-to-Gantt-chart link wasn't verifiable enough to ship, this documents what's actually safe to ship instead | 1 URL column on `GatewayPerformanceLog` |
| `22-agent-anomaly-detection-source.md` + `22-agent-anomaly-detection-measures.dax` | Native KQL time-series anomaly detection (`series_decompose_anomalies`) for failing/slow deployments — real seasonal decomposition, stronger than files 13-15's flat peer-comparison z-scores | New Log Analytics data source + measures |
| `23-agent-hierarchy-measures.dax` | Sub-agent/MCP-tool call-chain roll-up (chargeback) and drill-down (debugging) — `agentRootId`/`agentCallerType`/`agentDepth`/`agentTrustTier` are now real fields on `llm-usage-container`, see `guides/agent-hierarchy-attribution.md` | Measures only, no new columns — the fields already arrive from the ingestion pipeline |

## Prerequisite

This kit assumes the `cost` field baked onto every `llm-usage-container`
record by the `pricing-service` change (see
`guides/cost-attribution-guide.md` in the accelerator fork) — measures
reference `cost.totalCost` etc. as flattened columns
(`cost.totalCost`, `cost.inputCost`, ...). If you haven't applied that
change yet, either apply it first, or swap the measures in
`01-measures.dax` to the live `model-pricing` join the accelerator ships
with today (marked inline where relevant).

**The `cost` field does not arrive as flattened columns on its own —
you must expand it first.** Cosmos DB's Power Query connector surfaces
`cost` as a single `Record`-typed column, not the `cost.totalCost` /
`cost.inputCost` / ... columns every measure in this kit references.
Before pasting in a single measure from `01-measures.dax`:

1. **Transform Data** → select the `llm-usage-container` query.
2. Click the expand icon (⤢) on the `cost` column's header.
3. Tick all 9 sub-fields (`inputCost`, `outputCost`, `cachedCost`,
   `audioCost`, `reasoningCost`, `totalCost`, `currency`,
   `pricingVersion`, `calculationMethod`) — leave "Use original column
   name as prefix" checked, which is what produces the exact
   `cost.totalCost`-style names this kit's measures expect.
4. **OK**, then **Close & Apply**.

Skip this step and the very first measure you paste
(`Total Cost = SUM('llm-usage-container'[cost.totalCost])`) fails with
`Column 'llm-usage-container[cost.totalCost]' wasn't found` — this is
the single most common way to get stuck with this kit, so it's called
out here before step 1 of "Order of operations" below, not buried in a
troubleshooting section.

## Order of operations

0. **Expand the `cost` column first** — see "Prerequisite" immediately
   above. Every other step assumes this is already done.
1. `03-supporting-tables.m` first (measures reference these tables).
2. If you have PTU/reserved-capacity deployments: `06-ptu-allocation-columns.m`.
3. *(optional)* `05-cost-avoidance-column.m`.
4. `01-measures.dax` next — includes per-token-type cost breakdown
   (input/output/cached/audio/reasoning) and PTU allocation, not just a
   single total.
5. `02-rls-roles.dax` — test each role with **Modeling → View As** before
   publishing.
6. `07-data-quality-measures.dax`, `08-payg-ptu-breakeven-column.m` +
   `08-payg-ptu-breakeven-measures.dax` (needs `billingMonth` from step 2),
   `09-adoption-measures.dax` — the three new reports, independent of each
   other, add whichever you need.
7. `10-heatmap-moving-average.dax`, then `11-peak-off-peak-recommendation.dax`
   (needs 06, 08, and 10 already in place — it's the most dependent file
   in the kit).
8. `12-prompt-chain-efficiency-column.m` + `.dax`, `13-user-token-length-outliers.dax`,
   `14-cost-center-cross-comparison.dax` (needs `Date Only` from step 7) —
   independent of each other, add whichever you want.
9. `15-modified-zscore.dax`, `16-percentile-measures.dax`,
   `17-seasonality-aware-forecast.dax` — statistical upgrades to reports
   already built in steps 6-8, not new pages. `17` needs `Day Of Week
   Number`/`Date Only` from step 7.
10. `19-developer-role.m` (`AuthorizedApps`) and the new `Developer` role
    in `02-rls-roles.dax`, `19-model-version-drift.dax`,
    `20-deprecated-models.m` + `.dax`, and `18-failures-latency-source.md`
    + `.dax` (the one file needing a new Log Analytics data source, not
    just a new query on the existing Cosmos source).
11. `21-trace-deep-link.md` and `22-agent-anomaly-detection-source.md` +
    `.dax` — both need file 18 already in place (same Log Analytics
    workspace connection).
12. `23-agent-hierarchy-measures.dax` — needs
    `guides/agent-hierarchy-attribution.md`'s APIM/ingestion changes
    applied first (the `agentRootId`/`agentCallerType`/`agentDepth`/
    `agentTrustTier` fields have to actually be arriving on
    `llm-usage-container` before these measures return anything but
    blanks) — no new columns in this kit itself, just measures.
13. `04-report-build-guide.md` — build all eleven pages plus the three
    drillthrough pages, then apply the swap-in points at the guide's end
    for files 15-17 and the Page 11 additions for 21-22.
13. Publish, set up the daily refresh + workspace RBAC exactly as documented
   in the accelerator's own `guides/power-bi-dashboard.md`
   ("Publishing to the Power BI Service and scheduling refresh") — that
   part is unchanged by any of this.

## On PTU cost specifically

`[Total Cost]` (sum of `cost.totalCost`) is `$0`/blank for PTU or any other
`percentage`-priced deployment **by design** — see
`guides/cost-attribution-guide.md` in the accelerator fork: a single
ingested request can't know its share of a fixed monthly cost in
isolation, so `pricing-service` leaves it null rather than guess.
`01-measures.dax`'s "PTU / FIXED-CAPACITY COST" section reimplements the
accelerator's own existing proportional-distribution method (see
`guides/power-bi-dashboard.md`, "Handling PTU and other fixed-cost
services") as measures, so it shows up correctly in these new pages
instead of silently reading as zero. **Use `[Total Cost (incl. PTU)]`
everywhere the build guide calls for a total** — not `[Total Cost]` alone.
