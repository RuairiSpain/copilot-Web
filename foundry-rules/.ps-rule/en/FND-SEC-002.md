# FND-SEC-002

## SYNOPSIS

AI Search key authentication disabled or RBAC-only

## DESCRIPTION

An Azure AI Search service used by a Foundry project should not accept admin or query API keys. The rule passes when properties.disableLocalAuth is true. properties.authOptions.aadOrApiKey (keys and Entra both accepted) and authOptions.apiKeyOnly (keys only) are findings; the platform rejects setting disableLocalAuth true together with authOptions.

## RECOMMENDATION

Set disableLocalAuth to true and remove authOptions, then assign Entra roles (for example index data roles) to the Foundry project identity. If a migration needs keys temporarily, use authOptions.aadOrApiKey with aadAuthFailureMode and track removal with a suppression expiry.

## EXAMPLES

```
resource search 'Microsoft.Search/searchServices@2025-05-01' = {
  name: searchName
  location: location
  sku: { name: 'standard' }
  properties: {
    disableLocalAuth: true
  }
}
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Search services: properties.disableLocalAuth is true. The authOptions sub-state and the Foundry-scope test are not evaluated, so every Search service is checked.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/preview/2026-09-01-preview/search.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/preview/2026-09-01-preview/search.json) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.AI.DisableLocalAuth/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.AI.DisableLocalAuth/) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/c415ef063cd4c1833ecceff7f856295115d80c0d/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/c415ef063cd4c1833ecceff7f856295115d80c0d/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-05)
- [https://github.com/MicrosoftDocs/azure-ai-docs/blob/e8d0ac6f7ebd7fd000e287657ab9beda5bccc1fa/articles/foundry/agents/includes/concepts-capability-hosts-content.md](https://github.com/MicrosoftDocs/azure-ai-docs/blob/e8d0ac6f7ebd7fd000e287657ab9beda5bccc1fa/articles/foundry/agents/includes/concepts-capability-hosts-content.md) (last verified 2026-10-05)
