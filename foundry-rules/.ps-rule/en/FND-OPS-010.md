# FND-OPS-010

## SYNOPSIS

Model allow/deny lists remain consistent

## DESCRIPTION

The model allow and deny lists that the active profile declares must be internally consistent, must agree with the models the project actually deploys, and must agree with any Azure Policy assignment of the built-in definition "Foundry model deployments should only use approved models" that is declared in the repository (allowedPublishers and allowedAssetIds). The goal is to catch, before deployment, a model that a policy would block or a list that silently contradicts itself.

## RECOMMENDATION

Keep one source of truth for approved models, mirror it in the policy assignment parameters allowedPublishers and allowedAssetIds, and remove any model from azure.yaml that the lists exclude. Assign the policy with Audit first and move to Deny after the compliance dashboard is clean.

## EXAMPLES

```
# profile excerpt (allow list) - mirror the same values in the policy assignment parameters
models:
  allow:
    publishers: ["OpenAI"]
  deny: []
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: product-opinion. Catalogue decision: adapt.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs profile allow and deny lists and the policy assignment parameters.
- How to feed it: A PSRule configuration value could carry the lists; not implemented.
- This rule never reports Pass in PSRule.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/foundry/how-to/model-deployment-policy](https://learn.microsoft.com/azure/foundry/how-to/model-deployment-policy) (last verified 2026-10-04)
- [https://github.com/Azure/azure-policy/blob/master/built-in-policies/policyDefinitions/Cognitive%20Services/AllowDeployRegistryModels_Audit.json](https://github.com/Azure/azure-policy/blob/master/built-in-policies/policyDefinitions/Cognitive%20Services/AllowDeployRegistryModels_Audit.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/how-to/model-router-policy](https://learn.microsoft.com/azure/foundry/how-to/model-router-policy) (last verified 2026-10-04)
