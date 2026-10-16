# FND-DEP-005

## SYNOPSIS

Quota covers requested capacity

## DESCRIPTION

The capacity requested by each model deployment (sku.capacity) must fit within the subscription's remaining quota for that model and deployment type in the target region, and the region must report deployable capacity for it. Capacity pressure that is not a quota problem (temporary rate-limit adjustments, 429s) is a runtime concern outside this rule.

## RECOMMENDATION

Reduce sku.capacity, move the deployment to a region or deployment type with remaining quota, free quota from unused deployments, or request a quota increase before provisioning.

## EXAMPLES

```
# lower the requested capacity in the model deployment or choose another location
deployments:
  - name: chat
    model: { format: OpenAI, name: gpt-4o, version: "2024-08-06" }
    sku: { name: GlobalStandard, capacity: 50 }
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: ARM Usages_List and ModelCapacities_List (quota and available capacity).
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://learn.microsoft.com/azure/foundry/openai/how-to/quota](https://learn.microsoft.com/azure/foundry/openai/how-to/quota) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/concepts/rbac-foundry](https://learn.microsoft.com/azure/foundry/concepts/rbac-foundry) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/permissions/ai-machine-learning](https://learn.microsoft.com/azure/role-based-access-control/permissions/ai-machine-learning) (last verified 2026-10-04)
