# FND-RUN-004

## SYNOPSIS

Model deployment state and throttling signal

## DESCRIPTION

Every model deployment that a project, agent or gateway route depends on must be in a succeeded provisioning state, and recent Azure Monitor metrics must not show HTTP 429 responses for it. The 429 threshold is a product opinion; the meaning of the deployment state and of the 429 status code is documented.

## RECOMMENDATION

For a failed or disabled deployment, review the deployment error in the Foundry portal and recreate it. For 429 signals, raise the deployment capacity or quota, spread load across deployments or regions, or add client back-off and retry.

## EXAMPLES

```
resource gpt 'Microsoft.CognitiveServices/accounts/deployments@2026-09-01' = {
  parent: account
  name: 'gpt-4o'
  sku: { name: 'GlobalStandard', capacity: 100 }
  properties: {
    model: { format: 'OpenAI', name: 'gpt-4o', version: '2024-11-20' }
  }
}
```

## NOTES

- Severity dev/test/prod: warning/warning/error (PSRule: Warning/Warning/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: ARM deployment provisioning state and Azure Monitor metrics.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/monitor/resource-manager/Microsoft.Insights/Insights/stable/2024-02-01/metrics.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/monitor/resource-manager/Microsoft.Insights/Insights/stable/2024-02-01/metrics.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/openai/monitor-openai-reference](https://learn.microsoft.com/azure/foundry/openai/monitor-openai-reference) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/openai/how-to/latency](https://learn.microsoft.com/azure/foundry/openai/how-to/latency) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/foundry-models/how-to/monitor-models](https://learn.microsoft.com/azure/foundry/foundry-models/how-to/monitor-models) (last verified 2026-10-04)
