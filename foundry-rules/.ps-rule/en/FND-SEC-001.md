# FND-SEC-001

## SYNOPSIS

Foundry local authentication disabled

## DESCRIPTION

A Foundry (Microsoft.CognitiveServices) account should reject API-key authentication so that only Microsoft Entra ID tokens are accepted. The rule reads properties.disableLocalAuth on the account that hosts Foundry projects and deployments. A missing value is treated as keys enabled.

## RECOMMENDATION

Set properties.disableLocalAuth to true on the Foundry account and grant callers Entra ID roles instead of distributing keys. Keep keys enabled only in dev with a recorded suppression (reason and expiry).

## EXAMPLES

```
resource account 'Microsoft.CognitiveServices/accounts@2026-09-01' = {
  name: accountName
  location: location
  kind: 'AIServices'
  sku: { name: 'S0' }
  properties: {
    customSubDomainName: accountName
    disableLocalAuth: true
  }
}
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Accounts of kind AIServices: properties.disableLocalAuth is true. The catalogue limits the rule to accounts that own a project or deployment child; that correlation is not evaluated, so every AIServices account is checked.
- Related PSRule for Azure rules from the catalogue research: Azure.AI.DisableLocalAuth.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.AI.DisableLocalAuth/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.AI.DisableLocalAuth/) (last verified 2026-10-04)
