# FND-OPS-005

## SYNOPSIS

Resource-name provider constraints

## DESCRIPTION

Resource names in the Foundry stack must satisfy the length and character rules of their resource provider, otherwise the deployment is rejected. The rule checks the resolved name of each in-scope resource against a dated table taken from the Azure naming-rules reference and, for Foundry resources, from the Microsoft.CognitiveServices specification. Global uniqueness and reserved-word checks are out of scope (they need the control plane).

## RECOMMENDATION

Choose names that satisfy the provider limits for every environment name azd can produce, constrain name parameters with @minLength and @maxLength, and keep a short resource token for globally scoped resources (Storage 24, Key Vault 24, Cosmos DB 44, APIM 50, Search 60).

## EXAMPLES

```
@minLength(3)
@maxLength(24)
param storageName string

resource sa 'Microsoft.Storage/storageAccounts@2024-01-01' = {
  name: toLower(replace(storageName, '-', ''))
  location: location
  sku: { name: 'Standard_LRS' }
  kind: 'StorageV2'
}
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Names that are literals in the expanded resource for accounts, API Management, Storage, Cosmos DB, Key Vault, Container Registry, Log Analytics workspaces, Application Insights components, container apps and Search. Foundry projects are not evaluated because the expanded child name form was not verified. The accounts pattern enforces only the violations the catalogue lists.
- Related PSRule for Azure rules from the catalogue research: Azure.Storage.Name, Azure.APIM.Name, Azure.Search.Name, Azure.Cosmos.AccountName, Azure.KeyVault.Name, Azure.ACR.Name, Azure.Log.Name, Azure.AppInsights.Name, Azure.ContainerApp.Name, Azure.Group.Name, Azure.AI.FoundryNaming.

## LINKS

- [https://learn.microsoft.com/azure/azure-resource-manager/management/resource-name-rules](https://learn.microsoft.com/azure/azure-resource-manager/management/resource-name-rules) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/search-create-service-portal](https://learn.microsoft.com/azure/search/search-create-service-portal) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/azure-resource-manager/bicep/bicep-core-diagnostics](https://learn.microsoft.com/azure/azure-resource-manager/bicep/bicep-core-diagnostics) (last verified 2026-10-04)
