# FND-REL-008

## SYNOPSIS

Provisioned throughput spillover recommendation

## DESCRIPTION

A provisioned-throughput model deployment (sku.name ProvisionedManaged, GlobalProvisionedManaged or DataZoneProvisionedManaged) should set spilloverDeploymentName to a standard deployment of the same model in the same account, or the application should send the x-ms-spillover-deployment header per request. Learn recommends spillover for global and data zone provisioned deployments. Spillover is optional and adds pay-as-you-go cost, so this is a recommendation.

## RECOMMENDATION

Create a standard (pay-as-you-go) deployment of the same model and version in the same account, then set spilloverDeploymentName on the provisioned deployment to its name. Spillover initiates on 429, 500 and 503 (and on 400 for long-context requests) and is billed at standard rates for the overflow; monitor with the IsSpillover split in Azure Monitor.

## EXAMPLES

```
resource ptu 'Microsoft.CognitiveServices/accounts/deployments@2026-05-01' = {
  parent: foundry
  name: 'gpt-4o-mini-ptu'
  sku: { name: 'GlobalProvisionedManaged', capacity: 100 }
  properties: {
    model: { format: 'OpenAI', name: 'gpt-4o-mini', version: '2024-07-18' }
    spilloverDeploymentName: 'gpt-4o-mini-standard'
  }
  dependsOn: [ standard ]
}
```

## NOTES

- Severity dev/test/prod: info/info/warning (PSRule: Information/Information/Warning).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Provisioned deployments (ProvisionedManaged, GlobalProvisionedManaged, DataZoneProvisionedManaged): properties.spilloverDeploymentName is set. The check that the named deployment exists and is a standard deployment of the same model is not evaluated.

## LINKS

- [https://learn.microsoft.com/azure/foundry/openai/how-to/spillover-traffic-management](https://learn.microsoft.com/azure/foundry/openai/how-to/spillover-traffic-management) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-05-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-05-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/CognitiveServices/accounts/recommendations.yaml](https://github.com/Azure/Azure-Proactive-Resiliency-Library-v2/blob/main/azure-resources/CognitiveServices/accounts/recommendations.yaml) (last verified 2026-10-04)
