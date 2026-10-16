# FND-OPS-006

## SYNOPSIS

Log retention meets policy

## DESCRIPTION

Log retention on the Log Analytics workspace that receives Foundry diagnostics and Application Insights traces must meet the minimum retention that the active profile declares. The Azure platform defaults to 30 days (90 days for Application Insights tables and the Usage and AzureActivity tables); any longer period is an organisational requirement, not a platform one.

## RECOMMENDATION

Set workspace retentionInDays to at least the value your policy requires, or set totalRetentionInDays on the tables that hold Foundry diagnostics and traces to keep older data in low-cost long-term retention. Retention beyond the free period is billed.

## EXAMPLES

```
resource law 'Microsoft.OperationalInsights/workspaces@2025-07-01' = {
  name: lawName
  location: location
  properties: {
    retentionInDays: 90
  }
}
```

## NOTES

- Severity dev/test/prod: info/warning/error (PSRule: Information/Warning/Error).
- Catalogue status: product-opinion. Catalogue decision: adapt.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs workspaces, tables, diagnostic settings and a profile minimum retention.
- How to feed it: Needs a resource-set wrapper object and a profile value; not implemented.
- This rule never reports Pass in PSRule.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/azure-monitor/logs/data-retention-configure](https://learn.microsoft.com/azure/azure-monitor/logs/data-retention-configure) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/operationalinsights/resource-manager/Microsoft.OperationalInsights/OperationalInsights/stable/2026-03-01](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/operationalinsights/resource-manager/Microsoft.OperationalInsights/OperationalInsights/stable/2026-03-01) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/observability/concepts/trace-agent-concept](https://learn.microsoft.com/azure/foundry/observability/concepts/trace-agent-concept) (last verified 2026-10-04)
