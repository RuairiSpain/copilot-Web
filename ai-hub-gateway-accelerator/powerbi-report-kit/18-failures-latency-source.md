# Failures + Latency — new Power BI data source (not Cosmos)

**This one is structurally different from every other file in this kit.**
Everything through file 17 reads `llm-usage-container` in Cosmos DB.
Failure/latency data does not exist there — confirmed by reading the
actual `llm-usage-ingestion/workflow.json` schema, which has no latency,
status-code, or error field at all. That data genuinely exists in the
platform already (`guides/platform-observability-guide.md`, capability 3)
— it's just in **Log Analytics**, a data store this kit has never
queried.

## Adding the data source

Power BI Desktop → **Get Data → Azure → Azure Monitor Logs** (this is a
built-in connector, no gateway/extension needed). Point it at the hub's
Log Analytics workspace (same one `guides/platform-observability-guide.md`
already documents), paste the KQL below into the query box.

```kql
let LlmLogs = ApiManagementGatewayLlmLog
| where TimeGenerated > ago(30d)
| project CorrelationId, TimeGenerated, DeploymentName, TotalTokens;
let GatewayLogs = ApiManagementGatewayLogs
| where TimeGenerated > ago(30d)
| project CorrelationId, ResponseCode, TotalTime, BackendTime, IsRequestSuccess, AppId = ApimSubscriptionId;
LlmLogs
| join kind=inner (GatewayLogs) on CorrelationId
| project TimeGenerated, DeploymentName, TotalTokens, ResponseCode,
          TotalTime, BackendTime, IsRequestSuccess, AppId,
          GatewayOverheadTime = TotalTime - BackendTime
```

`DeploymentName`/`TotalTokens` on `ApiManagementGatewayLlmLog` and
`ResponseCode`/`TotalTime`/`BackendTime`/`IsRequestSuccess`/`ApimSubscriptionId`
on `ApiManagementGatewayLogs` are the accelerator's own documented example
query field (former) and Microsoft's official Log Analytics schema
reference (latter) — not guessed. `CorrelationId` is the join key the
guide itself says correlates these two tables ("with correlated entries
in `ApiManagementGatewayLogs`").

**`AppId` (aliased from `ApimSubscriptionId`)** — added specifically so
the `Developer` RLS role (`02-rls-roles.dax`) can apply to this page too,
closing the gap that role's own comment used to flag ("the KQL... doesn't
project one today"). `ApimSubscriptionId` is the same identity
`frag-set-llm-usage.xml`'s `appId` dimension defaults to
(`context.Subscription.Id`) when a request doesn't set its own `appId`
variable — the closest verifiable equivalent this table has to
`llm-usage-container[appId]`, not a guess. Not exercised against a real
Log Analytics workspace in this session — confirm this column actually
resolves before relying on Developer-role filtering here.

**Performance note**: this pulls raw per-request rows for 30 days. Fine
for a moderate-traffic gateway; at the "billions of tokens" scale this
platform targets, switch to a pre-aggregated version — replace the final
`project` with:

```kql
| summarize
    RequestCount = count(),
    FailedCount = countif(IsRequestSuccess == false),
    P50Ms = percentile(TotalTime, 50),
    P95Ms = percentile(TotalTime, 95),
    P99Ms = percentile(TotalTime, 99),
    AvgBackendMs = avg(BackendTime),
    AvgGatewayOverheadMs = avg(TotalTime - BackendTime)
  by DeploymentName, ResponseCode, AppId, bin(TimeGenerated, 1h)
```

(`AppId` added to the `by` clause too — drop it here if you don't need
per-app breakdown at this aggregated grain and would rather keep the
summary smaller.)

Name this query `GatewayPerformanceLog` (the DAX measures below assume
that name).

## Important limitation — read before building the page

**This table cannot be row-joined to `llm-usage-container`.** The Cosmos
pipeline has no `CorrelationId` (or any shared per-request key) with the
Log Analytics tables — they're two independent pipelines fed from the
same gateway, not one. You can still relate them at a coarser grain
(`DeploymentName` + day, via a Power BI relationship on those two
columns together) to eyeball "did this deployment get slower around the
same time cost went up" — but you cannot say "this specific $0.003
request also took 340ms." Getting true per-request join would mean
threading `CorrelationId` through the existing ingestion Logic App into
Cosmos — a real change to the pipeline itself, out of scope here; flag it
if you want that properly scoped later.
