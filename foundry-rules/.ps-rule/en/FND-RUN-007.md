# FND-RUN-007

## SYNOPSIS

Diagnostic logs are arriving

## DESCRIPTION

A Foundry account that has a diagnostic setting sending resource logs to Log Analytics should show recent log rows in the destination workspace when traffic exists. The rule confirms the setting exists, then runs one bounded, count-only query. Logs can take up to two hours to appear, so an empty result for a recently changed setting or an idle resource is Uncertain, not Fail.

## RECOMMENDATION

Enable a diagnostic setting for the Foundry account that sends Audit and Request and Response logs to a Log Analytics workspace, wait up to two hours, and confirm rows with a count query. Check workspace access and ingestion errors if rows stay at zero while the account has traffic.

## EXAMPLES

```
resource diag 'Microsoft.Insights/diagnosticSettings@2021-05-01-preview' = {
  name: 'to-law'
  scope: account
  properties: {
    workspaceId: law.id
    logs: [
      { category: 'Audit', enabled: true }
      { category: 'RequestResponse', enabled: true }
    ]
  }
}
```

## NOTES

- Severity dev/test/prod: info/warning/warning (PSRule: Information/Warning/Warning).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Diagnostic settings, Azure Monitor metrics and a Log Analytics query.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/foundry/how-to/diagnostic-logging](https://learn.microsoft.com/azure/foundry/how-to/diagnostic-logging) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/monitor/resource-manager/Microsoft.Insights/Insights/preview/2021-05-01-preview/diagnosticsSettings_API.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/monitor/resource-manager/Microsoft.Insights/Insights/preview/2021-05-01-preview/diagnosticsSettings_API.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-sdk-for-go/blob/main/sdk/monitor/azquery/logs_client.go](https://github.com/Azure/azure-sdk-for-go/blob/main/sdk/monitor/azquery/logs_client.go) (last verified 2026-10-04)
