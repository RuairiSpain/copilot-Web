# FND-NET-002

## SYNOPSIS

Dependent services public access/default-deny/private endpoints

## DESCRIPTION

For the documented standard-agent private-network setup, the Storage, Azure Cosmos DB and Azure AI Search resources referenced by capability settings must have public network access disabled and the documented private endpoints. The rule is deliberately limited to those three state stores. Key Vault, container registry, Application Insights, firewall-default semantics and other connection types require separate, service-specific evidence.

## RECOMMENDATION

Disable public network access on the Storage, Cosmos DB and AI Search resources that back the agents and give each a private endpoint in the private endpoint subnet, with its private DNS zone (FND-NET-003). Do not rely on the Foundry template to create them; the Foundry documentation states it does not.

## EXAMPLES

```
resource search 'Microsoft.Search/searchServices@2025-05-01' = {
  name: searchName
  location: location
  sku: { name: 'standard' }
  properties: { publicNetworkAccess: 'Disabled' }
}
resource searchPe 'Microsoft.Network/privateEndpoints@2024-05-01' = {
  name: '${searchName}-pe'
  location: location
  properties: {
    subnet: { id: peSubnetId }
    privateLinkServiceConnections: [{
      name: 'search'
      properties: { privateLinkServiceId: search.id, groupIds: ['searchService'] }
    }]
  }
}
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs capabilitySettings resolution and private endpoints correlated with Storage, Cosmos DB and Search resources.
- How to feed it: Needs a resource-set wrapper object; not implemented.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.Storage.Firewall, Azure.Cosmos.PublicAccess.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks](https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/networking-options](https://learn.microsoft.com/azure/foundry/agents/concepts/networking-options) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/standard-agent-setup](https://learn.microsoft.com/azure/foundry/agents/concepts/standard-agent-setup) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/preview/2026-07-15-preview/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/preview/2026-07-15-preview/cognitiveservices.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/storage/resource-manager/Microsoft.Storage/stable/2026-09-01](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/storage/resource-manager/Microsoft.Storage/stable/2026-09-01) (last verified 2026-10-04)
