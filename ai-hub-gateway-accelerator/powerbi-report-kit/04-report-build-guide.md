# Report build guide — three new pages

Add three new pages to `Citadel-Governance-Hub-Usage-Dashboard-V1.1-Incremental.pbix`
(right-click the page tab strip -> **New Page**, rename each). Before
`03-supporting-tables.m`/`01-measures.dax`, expand the `cost` column on
`llm-usage-container` first — see `00-README.md`'s "Prerequisite"
section for the exact steps. Skipping that step is the most common way
to get stuck here, since the very first measure this guide's visuals
depend on (`[Total Cost]`) references a column that doesn't exist until
you do it. Do `03-supporting-tables.m` and `01-measures.dax` next —
every visual below references a measure defined there.

## Page 1 — "Org"

No RLS restriction beyond the `Admin` role (org-wide) — Employee and
BudgetHolder roles will see this page filtered down to their own scope
automatically once RLS is on, so it doubles as their "org view, but scoped
to me/my cost center" — no separate page logic needed.

| Visual | Type | Fields |
| --- | --- | --- |
| Total spend trend | Line chart | X: `timestamp` (date hierarchy, day level) · Y: `[Total Cost]` |
| Total tokens trend | Line chart | X: `timestamp` (day) · Y: `[Total Tokens]` |
| Spend by product | Stacked bar chart | Axis: `productName` · Value: `[Total Cost (incl. PTU)]` |
| Spend by model | Stacked bar chart | Axis: `deploymentName` · Value: `[Total Cost (incl. PTU)]` |
| **Cost by type** | Stacked bar chart | Axis: `deploymentName` · Values: `[Input Cost]`, `[Output Cost]`, `[Cached Cost]`, `[Audio Cost]`, `[Reasoning Cost]`, `[PTU Allocated Cost]` (add all six as separate Values — Power BI stacks them automatically) |
| KPI: Total Cost | Card | `[Total Cost (incl. PTU)]` — **not** `[Total Cost]` alone, which excludes PTU/fixed-capacity deployments by design (see `01-measures.dax`) |
| KPI: PTU / Fixed-Capacity Cost | Card | `[PTU Allocated Cost]` — only add this tile if you actually have `percentage`-priced deployments; it's `0`/blank otherwise |
| KPI: Forecast Month-End | Card | `[Forecast Month-End Cost]` |
| KPI: Budget Consumed | Gauge | Value: `[Budget Consumed %]` · Target: 1.0 (100%) |
| KPI: Cost Avoidance | Card | `[Cost Avoidance $]` *(only if you added `05-cost-avoidance-column.m`)* |
| Distinct users / cost centers / products | 3 small cards | `[Distinct Users]`, `[Distinct Cost Centers]`, `[Distinct Products]` |

> If you have PTU/fixed-capacity deployments, add `06-ptu-allocation-columns.m`'s
> two columns before building this page — the "Cost by type" visual and the
> Total Cost KPI both need `[PTU Allocated Cost]`.

## Page 2 — "Cost Center"

Add a **slicer**: field `customDimension1`, renamed "Cost Center" in the
model first (right-click the column in Fields pane -> **Rename**, this
is cosmetic only, doesn't affect any DAX above since those reference the
underlying `customDimension1` name — Power BI keeps DAX working against
the renamed column automatically).

| Visual | Type | Fields |
| --- | --- | --- |
| Cost-center slicer | Slicer | `customDimension1` ("Cost Center") |
| Spend trend for selected cost center | Line chart | X: `timestamp` (day) · Y: `[Total Cost (incl. PTU)]` |
| Spend by product within cost center | Stacked bar | Axis: `productName` · Value: `[Total Cost (incl. PTU)]` |
| Cost by type within cost center | Stacked bar | Axis: `deploymentName` · Values: `[Input Cost]`, `[Output Cost]`, `[Cached Cost]`, `[Audio Cost]`, `[Reasoning Cost]`, `[PTU Allocated Cost]` |
| KPI: Cost Center Total | Card | `[Total Cost (incl. PTU)]` |
| KPI: Budget | Card | `[Current Month Budget]` |
| KPI: Remaining | Card | `[Budget Remaining]` |
| Budget risk indicator | Card w/ conditional formatting | `[Budget Risk Status]` — conditional text color: "At Risk" = red, "On Track" = green (Format visual -> Callout value -> Conditional formatting -> Font color -> Rules) |

Under `BudgetHolder` RLS, the slicer only ever offers the cost center(s)
that user is authorized for — no extra visual-level filter needed, RLS
already restricts the underlying rows.

## Page 3 — "My Usage" (individual user)

This page is meant to be identical for every viewer — RLS is what makes it
"my usage" per person, not a slicer (a slicer a user could change would let
them browse someone else's data, defeating the point).

| Visual | Type | Fields |
| --- | --- | --- |
| My spend trend | Line chart | X: `timestamp` (day) · Y: `[Total Cost (incl. PTU)]` |
| My requests by product | Bar chart | Axis: `productName` · Value: `[Total Requests]` |
| KPI: My Total Cost (this month) | Card | `[Total Cost (incl. PTU)]`, with a page-level filter `timestamp` is in current month |
| KPI: My Total Requests | Card | `[Total Requests]` |
| KPI: My Total Tokens | Card | `[Total Tokens]` |

Under the `Employee` role this page shows only the signed-in user's rows;
under `BudgetHolder`/`Admin` it shows whatever the page-level filters
allow (typically you'd hide this page from those roles entirely via
**View -> Selection pane -> page visibility**, since it's a personal page,
not a management one).

## Page 4 — "Data Quality" (Admin only)

Requires `07-data-quality-measures.dax`. Hide this page from Employee and
BudgetHolder roles (**View -> Selection pane** — an operational health
check, not something either persona needs).

| Visual | Type | Fields |
| --- | --- | --- |
| KPI: Price Resolution Rate | Gauge | Value: `[Price Resolution Rate]` · Target: 0.95 |
| KPI: Cost Center Coverage Rate | Card | `[Cost Center Coverage Rate]` |
| Unresolved price trend | Line chart | X: `timestamp` (day) · Y: `[Unresolved Price Count]` |
| Unresolved price by model | Table | Rows: `deploymentName` · Values: `[Unresolved Price Rate (Current Row Context)]` — sort descending, the models at the top need a pricing fix |
| Missing cost center by product | Table | Rows: `productName` · Values: `[Missing Cost Center Rate (Current Row Context)]` — sort descending, the products at the top need `customDimension1` wired up (see `guides/power-bi-dashboard.md`, "Activating custom dimensions") |

## Page 5 — "PAYG vs PTU Economics" (Admin only, skip if you have no PTU deployments)

Requires `08-payg-ptu-breakeven-column.m` and `08-payg-ptu-breakeven-measures.dax`,
plus the `billingMonth` column from `06-ptu-allocation-columns.m`. Add a
filter on this page: `deploymentName` ends with `-PTU` (or your actual
naming convention), so it only shows reserved-capacity deployments.

| Visual | Type | Fields |
| --- | --- | --- |
| Monthly volume vs. breakeven | Combo chart | X: `billingMonth` · Columns: `[Monthly Output Tokens]` · Line: `[Breakeven Output Tokens (Monthly)]` as a constant reference per deployment |
| Cost comparison | Combo chart | X: `billingMonth` · Columns: `[PTU Base Cost (Selected Deployment)]` and `[PAYG Equivalent Cost]` side by side |
| Savings trend | Line chart | X: `billingMonth` · Y: `[PTU vs PAYG Savings $]` — a horizontal zero line makes "is this reservation paying off" a glance |
| Detail table | Table | Rows: `deploymentName`, `billingMonth` · Values: `[Monthly Output Tokens]`, `[Breakeven Output Tokens (Monthly)]`, `[PTU Base Cost (Selected Deployment)]`, `[PAYG Equivalent Cost]`, `[PTU vs PAYG Savings $]`, `[PTU vs PAYG Savings %]` |

Read this alongside `guides/advanced-ptu-routing.md` — this page is the
evidence you'd want in hand before considering that document's proposed
(and explicitly **not implemented**) automated routing feature.

## Page 6 — "Adoption" (Admin/Executive)

Requires `09-adoption-measures.dax`, including the `FirstSeenByApp`
calculated table (**Modeling -> New table**, not New Measure).

| Visual | Type | Fields |
| --- | --- | --- |
| Active apps trend | Line chart | X: `timestamp` (day) · Y: `[Active Apps]` |
| KPI: New Apps This Month | Card | `[New Apps This Month]` |
| KPI: Total Known Apps | Card | `[Total Known Apps]` |
| KPI: Cost Center Coverage | Gauge | Value: `[Cost Center Coverage %]` · Target: 1.0 |
| Adoption by product | Bar chart | Axis: `productName` · Value: `[Active Apps]` |

## Page 7 — "Traffic Heatmap" (Admin, useful org-wide too)

Requires `10-heatmap-moving-average.dax`. Set **Day Of Week**'s sort-by
column to **Day Of Week Number** first (Column tools -> Sort by column) or
the heatmap's rows come out alphabetical instead of calendar order.

| Visual | Type | Fields |
| --- | --- | --- |
| Traffic heatmap | Matrix | Rows: `Day Of Week` · Columns: `Hour Of Day` · Values: `[Total Requests]` (or `[Total Tokens]`) — Format visual -> Cell elements -> turn on background color scale for the classic heatmap look |
| Spend trend with moving average | Line chart | X: `timestamp` (day) · Y (two lines): `[Total Cost (incl. PTU)]` and `[7-Day Moving Avg Cost]` |
| Tokens trend with moving average | Line chart | X: `timestamp` (day) · Y (two lines): `[Total Tokens]` and `[7-Day Moving Avg Tokens]` |

This page is also the visual evidence behind Page 8 below — look at it
first, the recommendation numbers make more sense once you can see where
the peaks actually fall.

## Page 8 — "PTU Peak/Off-Peak Recommendation" (Admin only, skip if no PTU deployments)

Requires `11-peak-off-peak-recommendation.dax` on top of Page 7's columns
and Page 5's PAYG/PTU breakeven measures. Same page-level filter as Page
5 (`deploymentName` ends with `-PTU`).

| Visual | Type | Fields |
| --- | --- | --- |
| Peak vs. off-peak split | Donut/pie | Legend: `Is Peak Hour` · Value: `[Monthly Output Tokens]` |
| KPI: Peak-Hour Utilization | Gauge | Value: `[Peak-Hour Utilization]` |
| KPI: Estimated Off-Peak Overpay | Card | `[Estimated Off-Peak Overpay]` — **title it exactly that, not "savings" or "recoverable cost"**, see the caveat comment in the DAX file before presenting this number to anyone |
| By-deployment detail table | Table | Rows: `deploymentName` · Values: `[Peak-Hour Utilization]`, `[Off-Peak Weight Share]`, `[Off-Peak Allocated PTU Cost]`, `[Off-Peak PAYG Equivalent Cost]`, `[Estimated Off-Peak Overpay]` |

Add a text box linking to `guides/advanced-ptu-routing.md` — this page is
the "should we bother building that" evidence the document explicitly
asks for before automating anything.

## Page 9 — "Prompting Patterns" (Employee: self only · Admin: aggregate)

Requires `12-prompt-chain-efficiency-column.m` and
`12-prompt-chain-efficiency-measures.dax`, plus `13-user-token-length-outliers.dax`
for the token-length comparison.

**Read the visibility note at the top of `12-prompt-chain-efficiency-measures.dax`
before building this page** — the recommended structure is two different
views of the same measures, not one page everyone sees the same way:

| Visual | Type | Fields | Who sees it |
| --- | --- | --- | --- |
| KPI: My Rapid-Fire Rate | Card | `[Rapid-Fire Rate]` | Employee (self-filtered by RLS already) |
| KPI: My Prompt-Length Z-Score | Card, conditional formatting | `[User Prompt Length Z-Score]`, colored neutral unless `[Is Outside Typical Prompt-Length Band]` = "Outside typical range" | Employee |
| Request gap distribution | Histogram (or binned bar chart) | `secondsSincePreviousRequest`, bucketed | Employee — lets someone see their own pattern, not just a single number |
| Platform-wide rapid-fire cost | Card | `[Rapid-Fire Chain Cost]` | Admin — "is this costing us anything material," not a per-person list |
| Aggregate rate by product (no user names) | Bar chart | Axis: `productName` · Value: `[Rapid-Fire Rate]` | Admin |

Deliberately **no per-user leaderboard visual is specified here** — see
the visibility note. If you decide your organization wants managers to
see individual rows, that's a page you build with that decision made
explicitly, not a default this kit sets.

## Page 10 — "Cost Center Comparison" (Budget Holder: their own row highlighted · Admin: all)

Requires `14-cost-center-cross-comparison.dax` (needs `Date Only` from
file 10 already in place).

| Visual | Type | Fields |
| --- | --- | --- |
| Cost centers ranked | Bar chart | Axis: `customDimension1` · Value: `[Cost Center Avg Daily Cost (Windowed)]` — sorted descending, with a constant reference line at `[Org Avg of Cost Center Daily Cost]` |
| KPI: Z-Score | Card, conditional formatting | `[Cost Center Cost Z-Score]` |
| KPI: Status | Card | `[Is Cost Center Outside Typical Band]` |
| Volatility | Card | `[Cost Center StdDev Daily Cost (Windowed)]` — a team with wildly spiky days is a different problem than a team with steadily high spend, worth showing separately |

Under `BudgetHolder` RLS this page still needs the ranked bar chart to
show *all* cost centers for context (a budget holder can't judge "is my
team high or low" from their own number alone) — set the bar chart's
visual-level filter to none and rely on a conditional-formatting rule
(font/bar color) to highlight the viewer's own cost center, rather than
RLS-restricting the chart down to one bar.

> **Scope note**: `CostCenterDailyCost` is a snapshot as of last refresh
> (trailing 30 days ending "now"), not a fully dynamic per-day rolling
> series you could put on a trend line and watch the z-score evolve day
> by day. That version is buildable (recompute the table with a rolling
> anchor date instead of a fixed `MAX(...) - 30`) but meaningfully more
> complex — ask if you want it built once you've seen whether the
> snapshot version is useful enough on its own.

## Drillthrough pages (progressive disclosure for any persona)

Three drillthrough targets, each built once and reachable by right-click
from anywhere the relevant field appears:

**"Cost Center Detail"** — new page, add `customDimension1` to the
**Drillthrough** field well (Visualizations pane, top). Build: daily spend
trend for the filtered cost center, `[Total Cost (incl. PTU)]` by user
(`appId`) and by `deploymentName`, plus this cost center's row from Page
10. Add a **Back** button (Insert -> Buttons -> Back). Reachable by
right-clicking any `customDimension1` value on Pages 2, 6, or 10.

**"User Detail"** — new page, `appId` in the Drillthrough field well.
Build: that user's request timeline, model mix, `[Rapid-Fire Rate]` and
`[User Prompt Length Z-Score]` cards from Page 9. Reachable from Pages 3,
6, or 9 — the natural flow for investigating a flagged outlier: see it on
Page 9 → right-click → drill through to the full picture before drawing
any conclusion.

**"Model Detail"** — new page, `deploymentName` in the Drillthrough field
well. Build: cost breakdown by type (reuse the Page 1 visual, filtered),
and — if it's a PTU deployment — the full Page 5/8 breakeven and
peak/off-peak detail for just that model. Reachable from Pages 1, 2, 5,
or 8.

Drillthrough pages don't need new measures — they reuse everything
already built, just filtered to whatever was clicked.

## Page 11 — "Developer" (new persona, files 18-20)

Requires the new `Developer` RLS role (`02-rls-roles.dax`) and
`19-developer-role.m` (`AuthorizedApps`). Uses three independent data
sources — the `GatewayPerformanceLog` Log Analytics query (18), the
existing Cosmos `llm-usage-container` (19), and the new
`ModelDeprecationSchedule` table (20) — laid out as three sections on one
page rather than three separate pages, since a developer investigating
"why is my agent misbehaving" wants all three in one place, not a
navigation exercise.

**Section 1 — Failures & Latency** (needs file 18):

| Visual | Type | Fields |
| --- | --- | --- |
| KPI: Failure Rate | Card, conditional formatting | `[Failure Rate]` |
| KPI: P95 Latency | Card | `[P95 Total Time (ms)]` |
| KPI: Backend Time Share | Gauge | `[Backend Time Share]` — low share means the gateway itself is adding meaningful overhead, worth a platform-team conversation rather than a model-choice one |
| Latency trend | Line chart | X: `TimeGenerated` (hour) · Y: `[P50 Total Time (ms)]` and `[P95 Total Time (ms)]` |
| Failures by deployment | Table | Rows: `DeploymentName` · Values: `[Failure Rate (Current Row Context)]`, `[P95 Total Time — Current Row Context (ms)]` |
| Failures by response code | Bar chart | Axis: `ResponseCode` · Value: `[Failed Request Count]` |

**Section 2 — Model Version Changes** (needs file 19):

| Visual | Type | Fields |
| --- | --- | --- |
| Deployment mix over time | Stacked area chart | X: `Date Only` (from `DailyDeploymentMix`) · Legend: `deploymentName` · Value: request count — visually shows exactly when a mix shifted |
| KPI: New Pairings This Week | Card | `[New Model Deployments This Week]` |
| Newest app-deployment pairings | Table | Rows: `appId`, `deploymentName` from `FirstSeenByAppDeployment` · Value: `[Days Since This App-Deployment Pairing First Seen]` — sort ascending, smallest number first |

**Section 3 — Deprecated Models** (needs file 20):

| Visual | Type | Fields |
| --- | --- | --- |
| KPI: Requests on Deprecated Models | Card, red if >0 | `[Requests on Deprecated Models (Trailing 30 Days)]` |
| KPI: Deployments Needing Migration | Card | `[Deployments Needing Migration]` |
| Status table | Table | Rows: `deploymentName` · Value: `[Deprecation Status]` — conditional format: red for "RETIRED", orange for "Deprecated"/"Retiring soon", default for "OK" |

## Page 11 additions — trace links + native anomaly detection (files 21-22)

**Section 1 (Failures & Latency) gets a new column**: add file 21's
`TraceLink` column to the "Failures by deployment" table (or any table
showing individual `GatewayPerformanceLog` rows) — set its Data Category
to Web URL and it renders as a clickable "Open in Log Analytics" link per
row, landing the developer on the correlated Application Insights data
for that request (see file 21 for exactly what it opens and why it isn't
a one-click straight-to-Gantt-chart link — that part wasn't verifiable
enough to ship).

**New Section 4 — Flagged Deployments** (needs file 22, its own Log
Analytics data source separate from `GatewayPerformanceLog`):

| Visual | Type | Fields |
| --- | --- | --- |
| KPI: Anomalous Hours (30d) | Card | `[Anomalous Hours This Deployment]`, unfiltered (org-wide count) |
| Flagged deployments table | Table | Rows: `DeploymentName` · Values: `[Anomalous Hours This Deployment]`, `[Anomaly Type]`, `[Most Recent Anomaly]` — sort by most recent anomaly, descending |
| Anomaly detail | Table (drillthrough target, or just filtered by table-row selection) | `TimeGenerated`, `RequestCount`, `FailureCount`, `AvgLatencyMs`, `[Anomaly Direction (Failures)]`, `[Anomaly Direction (Latency)]` |

This table is inherently short — the KQL only returns hours that were
already flagged, so a table with hundreds of rows means either genuinely
widespread instability or the threshold needs raising (see the tuning
note in file 22).

## Statistical upgrades — swap-in points on existing pages (files 15-17)

No new pages. These replace/augment specific visuals already built.

**File 15 (modified z-score)** — on Page 9 ("Prompting Patterns") and
Page 10 ("Cost Center Comparison"), point the Z-Score card and Status
card at `[User Prompt Length Modified Z-Score]` /
`[Is Outside Typical Prompt-Length Band (Modified)]` and
`[Cost Center Cost Modified Z-Score]` /
`[Is Cost Center Outside Typical Band (Modified)]` instead of files
13/14's originals. Keep the old measures in the model (harmless, unused)
or swap the visual references fully — your call.

**File 16 (percentiles)** — add to Page 1 ("Org"): a table or small
multiples showing `[P50 Cost Per Request]`, `[P90 Cost Per Request]`,
`[P95 Cost Per Request]`, `[Cost Tail Ratio (P95/P50)]` next to the
existing `[Avg Cost per Request]` card — seeing the median and the mean
disagree is itself informative. Add `[P50 Prompt Tokens]`/`[P95 Prompt
Tokens]` to the Model Detail drillthrough page.

**File 17 (seasonality-aware forecast)** — on Page 1's "KPI: Forecast
Month-End" card, add `[Seasonality-Aware Forecast Month-End Cost]`
alongside the existing `[Forecast Month-End Cost]`, plus
`[Forecast Delta (Seasonality vs Linear)]` as a small secondary card so
the difference between the two methods is visible, not hidden behind a
silent swap.

## After building all fourteen pages

1. **Modeling -> View As** — check each role sees exactly what it should
   before publishing anything.
2. Publish per the existing `guides/power-bi-dashboard.md` → "Publishing
   to the Power BI Service and scheduling refresh" section — unchanged by
   this kit.
3. In the workspace dataset's **Security** settings, map an Entra ID
   security group to each of the four roles (Employee / BudgetHolder /
   Admin / Developer) — group-based assignment, not per-user.
