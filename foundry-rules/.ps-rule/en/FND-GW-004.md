# FND-GW-004

## SYNOPSIS

Route paths unique

## DESCRIPTION

The API URL suffix (ARM properties.path) must be unique across the APIs of an API Management service, because the gateway distinguishes and routes APIs by that suffix. Two distinct APIs (not revisions of the same API) that share a path are a conflict.

## RECOMMENDATION

Give every API a distinct URL suffix (properties.path), for example one prefix per model family or team, and keep revisions under the same API instead of duplicating it.

## EXAMPLES

```
resource chat 'Microsoft.ApiManagement/service/apis@2024-05-01' = {
  parent: apim
  name: 'foundry-chat'
  properties: { displayName: 'Foundry chat', path: 'foundry/chat', protocols: ['https'] }
}
resource embeddings 'Microsoft.ApiManagement/service/apis@2024-05-01' = {
  parent: apim
  name: 'foundry-embeddings'
  properties: { displayName: 'Foundry embeddings', path: 'foundry/embeddings', protocols: ['https'] }
}
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs all APIs of one service compared (path uniqueness across resources and version sets). PSRule evaluates one object at a time.
- How to feed it: Needs a resource-set wrapper object; not implemented.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/api-management/import-and-publish](https://learn.microsoft.com/azure/api-management/import-and-publish) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/graphql-api](https://learn.microsoft.com/azure/api-management/graphql-api) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement) (last verified 2026-10-04)
