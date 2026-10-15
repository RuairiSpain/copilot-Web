# FND-DEP-007

## SYNOPSIS

Globally unique names available

## DESCRIPTION

Names that must be unique across Azure (Foundry custom subdomain, Storage account, Key Vault, AI Search service, API Management service, Cosmos DB account) must be available when planned. A name already held by the resource being redeployed in the same place is not a conflict.

## RECOMMENDATION

Add a deterministic unique suffix to globally unique names (for example with the Bicep uniqueString function), keep the Foundry custom subdomain and account naming rules, and avoid reusing a name whose resource was recently deleted (see FND-DEP-006).

## EXAMPLES

```
var resourceToken = toLower(uniqueString(subscription().id, environmentName, location))
var foundryName = 'aif-${resourceToken}'
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Per-provider name availability calls (POST checks and one HEAD).
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- Related PSRule for Azure rules from the catalogue research: Azure.KeyVault.Name, Azure.Storage.Name, Azure.Search.Name, Azure.APIM.Name, Azure.Cosmos.AccountName, Azure.ACR.Name.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/keyvault/resource-manager/Microsoft.KeyVault/KeyVault/stable/2026-05-15/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/keyvault/resource-manager/Microsoft.KeyVault/KeyVault/stable/2026-05-15/openapi.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/storage/resource-manager/Microsoft.Storage/stable/2026-09-01/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/storage/resource-manager/Microsoft.Storage/stable/2026-09-01/openapi.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement/stable/2024-05-01](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement/stable/2024-05-01) (last verified 2026-10-04)
- [https://learn.microsoft.com/en-us/rest/api/container-registry/registries/check-name-availability?view=rest-container-registry-2025-11-01](https://learn.microsoft.com/en-us/rest/api/container-registry/registries/check-name-availability?view=rest-container-registry-2025-11-01) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2026-03-15/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2026-03-15/openapi.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/includes/create-project-cli](https://learn.microsoft.com/azure/foundry/includes/create-project-cli) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/permissions/security](https://learn.microsoft.com/azure/role-based-access-control/permissions/security) (last verified 2026-10-04)
