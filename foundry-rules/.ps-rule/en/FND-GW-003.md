# FND-GW-003

## SYNOPSIS

Routes resolve to existing backends

## DESCRIPTION

Every set-backend-service policy that uses backend-id, and every backend pool member, must name an existing Microsoft.ApiManagement/service/backends entity in the same API Management service; APIs without a routing policy must have a serviceUrl. For AI gateway routes the backend entity's url should address the Foundry/Azure OpenAI account the project provisions.

## RECOMMENDATION

Define the backend entity (or pool) in the same service and reference it by its exact name, or set the API serviceUrl. Keep backend URLs pointing at the Foundry account endpoint the project provisions.

## EXAMPLES

```
resource backend 'Microsoft.ApiManagement/service/backends@2024-05-01' = {
  parent: apim
  name: 'foundry-openai'
  properties: { url: 'https://${foundryName}.openai.azure.com/openai', protocol: 'http' }
}
// policy
<set-backend-service backend-id="foundry-openai" />
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs policy XML set-backend-service values correlated with backend resources.
- How to feed it: None implemented.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/api-management/set-backend-service-policy](https://learn.microsoft.com/azure/api-management/set-backend-service-policy) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/backends](https://learn.microsoft.com/azure/api-management/backends) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/azure-ai-foundry-api](https://learn.microsoft.com/azure/api-management/azure-ai-foundry-api) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement) (last verified 2026-10-04)
