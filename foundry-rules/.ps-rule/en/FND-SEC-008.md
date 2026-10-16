# FND-SEC-008

## SYNOPSIS

Relevant Defender plans enabled

## DESCRIPTION

Microsoft Defender for Cloud plans that match the resource types in the Foundry solution should be on the Standard tier on the subscription: AI (when Foundry accounts exist), StorageAccounts, KeyVaults and CosmosDbs. The set of relevant plans is derived from the resources found.

## RECOMMENDATION

Enable the Defender plan for each resource type in use on the subscription (Standard tier), or confirm it is enforced at a parent scope. Enabling Defender plans has a cost; review pricing before enabling in non-production.

## EXAMPLES

```
resource defenderAi 'Microsoft.Security/pricings@2026-07-01' = {
  name: 'AI'
  properties: { pricingTier: 'Standard' }
}
```

## NOTES

- Severity dev/test/prod: info/warning/warning (PSRule: Information/Warning/Warning).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: GET Microsoft.Security/pricings at subscription scope (Defender plans).
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.Defender.Storage, Azure.Defender.KeyVault, Azure.Defender.CosmosDb.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/security/resource-manager/Microsoft.Security/Security/stable/2026-07-01/pricings.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/security/resource-manager/Microsoft.Security/Security/stable/2026-07-01/pricings.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-policy/blob/master/built-in-policies/policyDefinitions/Security%20Center/ASC_Azure_Defender_AI_Full_Features_DINE.json](https://github.com/Azure/azure-policy/blob/master/built-in-policies/policyDefinitions/Security%20Center/ASC_Azure_Defender_AI_Full_Features_DINE.json) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Defender.Storage/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.Defender.Storage/) (last verified 2026-10-04)
