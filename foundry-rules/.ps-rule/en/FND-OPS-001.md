# FND-OPS-001

## SYNOPSIS

Diagnostic settings to Log Analytics

## DESCRIPTION

Each Foundry-stack resource that emits resource logs (Foundry/AI Services account and project, AI Search, API Management, the Storage blob service used by the project, Cosmos DB account) should have an Azure Monitor diagnostic setting that sends logs to a Log Analytics workspace, using a category group (allLogs or audit) or an explicit list of categories that the resource type supports. Without a diagnostic setting the resource logs are not collected and cannot be queried during an incident.

## RECOMMENDATION

Add a Microsoft.Insights/diagnosticSettings resource per resource, scoped to the resource, that sends the audit category group (or allLogs when you need request and response diagnostics) to your Log Analytics workspace. Use audit as a low-cost start because several categories (for example AzureOpenAIRequestUsage and ManagedNetworkEvent on accounts, and all project categories) are marked as having a cost to export in the Azure Monitor reference.

## EXAMPLES

```
resource diag 'Microsoft.Insights/diagnosticSettings@2021-05-01-preview' = {
  name: 'to-law'
  scope: foundryAccount
  properties: {
    workspaceId: logAnalytics.id
    logs: [
      { categoryGroup: 'audit', enabled: true }
    ]
  }
}
```

## NOTES

- Severity dev/test/prod: info/warning/error (PSRule: Information/Warning/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs diagnostic settings correlated with the resources they scope, and workspaces.
- How to feed it: Needs a resource-set wrapper object; not implemented.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/azure-monitor/reference/supported-logs/microsoft-cognitiveservices-accounts-logs](https://learn.microsoft.com/azure/azure-monitor/reference/supported-logs/microsoft-cognitiveservices-accounts-logs) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/azure-monitor/data-collection/diagnostic-settings](https://learn.microsoft.com/azure/azure-monitor/data-collection/diagnostic-settings) (last verified 2026-10-04)
- [https://github.com/Azure/azure-policy/blob/master/built-in-policies/policyDefinitions/Monitoring/DS_LA_cognitiveservices-accounts_DINE.json](https://github.com/Azure/azure-policy/blob/master/built-in-policies/policyDefinitions/Monitoring/DS_LA_cognitiveservices-accounts_DINE.json) (last verified 2026-10-04)
