# FND-OPS-003

## SYNOPSIS

Baseline alerts exist

## DESCRIPTION

A production Foundry deployment should define a baseline set of Azure Monitor alerts: a Service Health alert (activity log alert on service health events) and metric alerts on the Foundry model-deployment metrics that detect regressions (availability, error status codes, latency). The baseline set is a Foundry Doctor recommendation built from the signals the Foundry documentation lists; it is not a platform requirement.

## RECOMMENDATION

Add a Service Health alert and metric alerts on ModelAvailabilityRate, ModelRequests filtered by StatusCode (429 and 5xx) and TimeToResponse for the Foundry account, each with an action group. Choose thresholds from your own traffic and latency targets.

## EXAMPLES

```
resource availabilityAlert 'Microsoft.Insights/metricAlerts@2026-01-01' = {
  name: 'foundry-model-availability'
  location: 'global'
  properties: {
    severity: 2
    enabled: true
    scopes: [ foundryAccount.id ]
    evaluationFrequency: 'PT5M'
    windowSize: 'PT15M'
    criteria: {
      'odata.type': 'Microsoft.Azure.Monitor.SingleResourceMultipleMetricCriteria'
      allOf: [ { name: 'avail', metricName: 'ModelAvailabilityRate', operator: 'LessThan', threshold: 99, timeAggregation: 'Average', criterionType: 'StaticThresholdCriterion' } ]
    }
    actions: [ { actionGroupId: actionGroup.id } ]
  }
}
```

## NOTES

- Severity dev/test/prod: info/info/warning (PSRule: Information/Information/Warning).
- Catalogue status: product-opinion. Catalogue decision: adapt.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs alert rules, scopes and action groups correlated.
- How to feed it: Needs a resource-set wrapper object; not implemented.
- This rule never reports Pass in PSRule.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.Monitor.ServiceHealth.

## LINKS

- [https://learn.microsoft.com/azure/foundry/how-to/stay-informed-service-health](https://learn.microsoft.com/azure/foundry/how-to/stay-informed-service-health) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/azure-monitor/reference/supported-metrics/microsoft-cognitiveservices-accounts-metrics](https://learn.microsoft.com/azure/azure-monitor/reference/supported-metrics/microsoft-cognitiveservices-accounts-metrics) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/azure-resource-manager/management/resource-name-rules](https://learn.microsoft.com/azure/azure-resource-manager/management/resource-name-rules) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/monitor/resource-manager/Microsoft.Insights/Insights](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/monitor/resource-manager/Microsoft.Insights/Insights) (last verified 2026-10-04)
