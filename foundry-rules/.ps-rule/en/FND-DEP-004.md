# FND-DEP-004

## SYNOPSIS

Model/version/SKU offered in target region

## DESCRIPTION

Each model deployment (model format, name, version and SKU name) declared in azure.yaml or the compiled template must appear in the list of models offered in the target location, with the declared SKU, and must not be retired. Offered in the list is necessary, not sufficient: gated models and quota are separate conditions.

## RECOMMENDATION

Choose a model, version and SKU that the target region lists, or move the Foundry resource to a region that offers it. To list what a region offers, query the models API for that location. If the model is deprecating, plan the move to the replacement version.

## EXAMPLES

```
az cognitiveservices model list --location <region> --query "[?model.name=='gpt-4o'].{version:model.version,sku:model.skus[].name}"
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: ARM Models_List for the target location (Microsoft.CognitiveServices).
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/permissions/ai-machine-learning](https://learn.microsoft.com/azure/role-based-access-control/permissions/ai-machine-learning) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/limits-quotas-regions](https://learn.microsoft.com/azure/foundry/agents/concepts/limits-quotas-regions) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/how-to/create-resource-template](https://learn.microsoft.com/azure/foundry/how-to/create-resource-template) (last verified 2026-10-04)
