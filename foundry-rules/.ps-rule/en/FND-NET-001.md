# FND-NET-001

## SYNOPSIS

Foundry public access/private endpoint posture

## DESCRIPTION

A Foundry (Microsoft.CognitiveServices/accounts, AI Services) account must have a deliberate inbound network posture. The rule reads properties.publicNetworkAccess (Enabled or Disabled) and properties.networkAcls (defaultAction Allow or Deny, ipRules, virtualNetworkRules) and correlates them with the private endpoints that target the account. It reports an account that is open to all public networks, and an account whose public access is disabled but that has no approved private endpoint (group ID account) in the analysed scope, because nothing can then reach it. Foundry projects inherit the account posture, so the finding is raised once on the account and names the projects it affects.

## RECOMMENDATION

For test and production set publicNetworkAccess to Disabled and add a private endpoint with group ID account in the VNet that hosts your clients and agents, with its private DNS zones (see FND-NET-003). If you need limited public access, use restricted mode (defaultAction Deny plus an explicit allow list). Public access is acceptable for dev prototyping.

## EXAMPLES

```
resource account 'Microsoft.CognitiveServices/accounts@2026-09-01' = {
  name: accountName
  location: location
  kind: 'AIServices'
  sku: { name: 'S0' }
  identity: { type: 'SystemAssigned' }
  properties: {
    customSubDomainName: accountName
    publicNetworkAccess: 'Disabled'
    networkAcls: { defaultAction: 'Deny' }
  }
}
resource pe 'Microsoft.Network/privateEndpoints@2024-05-01' = {
  name: '${accountName}-pe'
  location: location
  properties: {
    subnet: { id: peSubnetId }
    privateLinkServiceConnections: [{
      name: 'account'
      properties: { privateLinkServiceId: account.id, groupIds: ['account'] }
    }]
  }
}
```

## NOTES

- Severity dev/test/prod: info/warning/error (PSRule: Information/Warning/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Finding A (open posture) on accounts of kind AIServices or OpenAI: passes when publicNetworkAccess is Disabled or networkAcls.defaultAction is Deny. Finding B (private endpoint missing) needs correlation with Microsoft.Network/privateEndpoints and is not evaluated. Accounts are not limited to those that host a Foundry project.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.AI.PublicAccess, Azure.AI.PrivateEndpoints.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/concepts/networking-options](https://learn.microsoft.com/azure/foundry/agents/concepts/networking-options) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/how-to/configure-private-link](https://learn.microsoft.com/azure/foundry/how-to/configure-private-link) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks](https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks) (last verified 2026-10-04)
