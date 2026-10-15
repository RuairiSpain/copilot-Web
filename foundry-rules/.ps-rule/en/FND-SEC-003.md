# FND-SEC-003

## SYNOPSIS

Cosmos DB local authentication disabled

## DESCRIPTION

A Cosmos DB account used by Foundry (for example agent thread storage reached through a capability host) should disable key-based authentication. The rule reads properties.disableLocalAuth on Microsoft.DocumentDB/databaseAccounts. PSRule applies its equivalent only to the NoSQL API; this rule applies the same scope.

## RECOMMENDATION

Set properties.disableLocalAuth to true and grant the Foundry project managed identity a Cosmos DB data-plane role assignment. Check that clients use Entra ID before enforcing in an existing environment.

## EXAMPLES

```
resource cosmos 'Microsoft.DocumentDB/databaseAccounts@2026-03-15' = {
  name: cosmosName
  location: location
  kind: 'GlobalDocumentDB'
  properties: {
    databaseAccountOfferType: 'Standard'
    locations: [ { locationName: location, failoverPriority: 0 } ]
    disableLocalAuth: true
  }
}
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Cosmos DB accounts: properties.disableLocalAuth is true. The Foundry-scope test is not evaluated, so every Cosmos DB account is checked. The catalogue notes that PSRule limits its equivalent rule to the NoSQL API and that the API-kind detection was not verified.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.Cosmos.NoSQLLocalAuth.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2026-03-15/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2026-03-15/openapi.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Cosmos.NoSQLLocalAuth/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Cosmos.NoSQLLocalAuth/) (last verified 2026-10-04)
- [https://github.com/MicrosoftDocs/azure-ai-docs/blob/e8d0ac6f7ebd7fd000e287657ab9beda5bccc1fa/articles/foundry/agents/includes/concepts-capability-hosts-content.md](https://github.com/MicrosoftDocs/azure-ai-docs/blob/e8d0ac6f7ebd7fd000e287657ab9beda5bccc1fa/articles/foundry/agents/includes/concepts-capability-hosts-content.md) (last verified 2026-10-05)
