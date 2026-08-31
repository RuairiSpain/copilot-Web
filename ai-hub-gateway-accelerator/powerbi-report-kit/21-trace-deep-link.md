# Linking Power BI rows to Application Insights traces

## What I verified, and what I didn't

**`ApiManagementGatewayLogs.CorrelationId` is NOT the same identifier as
Application Insights' `operation_Id`.** These are two different
correlation systems. Per Microsoft's own guidance: APIM's `CorrelationId`
(= `RequestId`) is logged *into* Application Insights as
`requests.customDimensions["RequestId"]` (a custom property on the
`requests` telemetry row, not the row's own `operation_Id`). So linking a
`GatewayPerformanceLog` row (file 18) to its Application Insights
transaction means matching on that custom property, not assuming the IDs
are interchangeable — I corrected this before building anything on the
wrong assumption.

**I could not verify a stable, officially documented URL format for
deep-linking straight into one specific trace's Gantt-chart transaction
view.** Multiple sources (including a Microsoft Q&A thread) confirm there
is no "Copy link" for that exact view, and the formats that do exist
online are reverse-engineered from the portal's internal routes (gzip +
base64-encoded KQL embedded in the URL) — not stable, not officially
supported, and not something I could test against a real workspace in
this environment. I'm not shipping Power Query code that constructs that
URL: a plausible-looking encoding that's subtly wrong produces a link
that silently 404s, which is worse than no link.

## What I'm shipping instead — verified, simple, will actually work

A **resource-level Logs blade link** (the standard, well-known "open this
Azure resource in the portal" URL pattern — not a private/internal route)
plus the `CorrelationId` displayed as plain copyable text, plus a ready
KQL snippet. One extra paste instead of one click, on solid ground
instead of guessed ground.

### Add to the `GatewayPerformanceLog` query (file 18)

```m
// Add as a custom column on GatewayPerformanceLog
= "https://portal.azure.com/#@<your-tenant-id>/resource<your-app-insights-resource-id>/logs"
```

Replace `<your-tenant-id>` and `<your-app-insights-resource-id>` with your
actual values (Tenant ID from Entra ID; the App Insights resource ID from
its portal Overview blade — the `/subscriptions/.../resourceGroups/.../providers/microsoft.insights/components/...`
path). Mark this column's **Data category: Web URL** (Column tools ->
Data Category) so it renders as a clickable link in any table visual.

### The KQL snippet to paste once the Logs blade opens

```kql
requests
| where customDimensions["RequestId"] == "<paste the row's CorrelationId here>"
| union (dependencies | where customDimensions["RequestId"] == "<same value>")
| order by timestamp asc
```

This surfaces the correlated `requests`/`dependencies` rows for that
exact gateway call. From there, the portal's own **"View timeline"**
button (on the `requests` row) opens the full transaction Gantt chart —
the view you actually wanted, reached through the officially supported UI
path rather than a guessed URL.

### If you want the one-click version anyway

It's likely achievable — gzip-compress the KQL text, base64-encode it,
URL-encode the result, assemble into the blade URL — but I'd want to
build and test it against a real Log Analytics workspace before handing
it to you, since Power Query's `Binary.Compress` output needs to match
byte-for-byte what the portal's internal deep-link parser expects, and I
have no way to verify that without one. Say the word if you have a test
workspace to validate against and want me to attempt it properly rather
than guess.
