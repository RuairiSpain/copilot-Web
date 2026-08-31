# Flagged Agents/Deployments — native KQL anomaly detection

This is a genuinely better tool than anything files 13-15 built in DAX.
The modified z-score work (file 15) compares against a **flat** peer or
window average — it has no concept of trend or day-of-week seasonality,
so it can't distinguish "this deployment is having a bad afternoon" from
"this deployment is always slower on Mondays." Kusto (the query language
behind Log Analytics/Application Insights) has **real time-series
decomposition built in** — `series_decompose_anomalies()` splits a series
into seasonal + trend + residual components and flags outliers in the
residual, which is much closer to what Azure Cost Management's own
forecasting does (referenced two turns ago) than a DAX z-score ever could
be. Running it in KQL also means the anomaly detection happens at the
source, once, instead of being recomputed by every report that opens the
`.pbix`.

**Confidence note**: I verified `series_decompose_anomalies()` is real,
documented Kusto functionality and confirmed its parameter usage against
a working example, but couldn't reach Microsoft's function-reference page
directly in this environment to triple-check exact parameter defaults.
**Test this query in the Log Analytics query editor and tune the
threshold before wiring it into Power BI** — treat the numbers below
(`1.5` threshold, `'linefit'` trend) as reasonable starting points, not
verified-correct-for-your-traffic defaults.

## The query

Add as a new Power BI data source the same way as file 18 (**Get Data ->
Azure -> Azure Monitor Logs**), name the query `AgentAnomalyLog`:

```kql
let LlmLogs = ApiManagementGatewayLlmLog
| where TimeGenerated > ago(30d)
| project CorrelationId, DeploymentName;
let Joined = ApiManagementGatewayLogs
| where TimeGenerated > ago(30d)
| join kind=inner (LlmLogs) on CorrelationId;
Joined
| make-series
    RequestCount = count(),
    FailureCount = countif(IsRequestSuccess == false),
    AvgLatencyMs = avg(TotalTime)
    on TimeGenerated from ago(30d) to now() step 1h
    by DeploymentName
| extend (FailureAnomalyFlag, FailureAnomalyScore, FailureBaseline) =
    series_decompose_anomalies(FailureCount, 1.5, -1, 'linefit')
| extend (LatencyAnomalyFlag, LatencyAnomalyScore, LatencyBaseline) =
    series_decompose_anomalies(AvgLatencyMs, 1.5, -1, 'linefit')
| mv-expand
    TimeGenerated to typeof(datetime),
    RequestCount to typeof(long),
    FailureCount to typeof(long),
    AvgLatencyMs to typeof(real),
    FailureAnomalyFlag to typeof(int),
    FailureAnomalyScore to typeof(real),
    FailureBaseline to typeof(real),
    LatencyAnomalyFlag to typeof(int),
    LatencyAnomalyScore to typeof(real),
    LatencyBaseline to typeof(real)
| where FailureAnomalyFlag != 0 or LatencyAnomalyFlag != 0
| project TimeGenerated, DeploymentName, RequestCount, FailureCount, AvgLatencyMs,
          FailureAnomalyFlag, FailureAnomalyScore, FailureBaseline,
          LatencyAnomalyFlag, LatencyAnomalyScore, LatencyBaseline
```

**What this returns**: only the hours where either failure count or
latency deviated from that deployment's own seasonal+trend baseline —
not a raw firehose. `AnomalyFlag` is `-1` (anomalously low), `0` (normal,
filtered out already), or `1` (anomalously high); `AnomalyScore`
indicates how far outside the expected range. A row appearing here means
"this hour looked different from this deployment's own normal pattern for
this hour of day/day of week" — not "this hour was objectively bad,"
which matters for a deployment that's simply growing (rising trend is
part of what gets modeled out, not flagged).

**Tuning**: raise the `1.5` threshold (e.g. to `2.5`–`3`) if this produces
too many rows to be useful on your actual traffic; the KQL docs describe
it as roughly analogous to a z-score-style sensitivity control. The
`ago(30d)` window needs at least a few weeks of history before the
seasonal component means anything — expect noisier results on a
newly-deployed model.
