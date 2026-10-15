# FND-OPS-009

## SYNOPSIS

Data-residency regions and deployment SKU

## DESCRIPTION

When the profile declares a data-residency requirement (global, a data zone of US, EU or APAC, or an Azure geography), every Foundry model deployment must use a deployment SKU whose documented processing scope satisfies it, and the resource regions must be inside the declared allowed regions. Microsoft documents that Global SKUs may process data in any Azure region, Data Zone SKUs only within the data zone, and Standard and Regional Provisioned SKUs within the customer-specified geography; data at rest stays in the designated geography for all types.

## RECOMMENDATION

Choose a deployment SKU that matches the residency requirement - Data Zone Standard or Data Zone Provisioned for a data zone, Standard or Regional Provisioned for a geography - in a region that belongs to that zone or geography, and keep the region list in the profile current.

## EXAMPLES

```
# azure.yaml
services:
  ai-project:
    host: azure.ai.project
    deployments:
      - name: chat
        model: { format: OpenAI, name: gpt-5.4-mini, version: "2026-03-17" }
        sku: { name: DataZoneStandard, capacity: 10 }
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs a declared data-residency requirement (scope and allowed regions) from the profile; without it the catalogue skips the rule.
- How to feed it: A PSRule configuration value could carry it; not implemented.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.Resource.AllowedRegions.

## LINKS

- [https://learn.microsoft.com/azure/foundry/foundry-models/concepts/deployment-types](https://learn.microsoft.com/azure/foundry/foundry-models/concepts/deployment-types) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference](https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/concepts/deployments-overview](https://learn.microsoft.com/azure/foundry/concepts/deployments-overview) (last verified 2026-10-04)
- [https://github.com/MicrosoftDocs/azure-ai-docs/blob/e8d0ac6f7ebd7fd000e287657ab9beda5bccc1fa/articles/foundry/foundry-models/includes/concepts-deployment-types-content-6.md](https://github.com/MicrosoftDocs/azure-ai-docs/blob/e8d0ac6f7ebd7fd000e287657ab9beda5bccc1fa/articles/foundry/foundry-models/includes/concepts-deployment-types-content-6.md) (last verified 2026-10-05)
