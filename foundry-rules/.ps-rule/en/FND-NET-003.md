# FND-NET-003

## SYNOPSIS

Private endpoint DNS zone groups and VNet links

## DESCRIPTION

Every private endpoint that serves a Foundry deployment must resolve to its private IP from the VNet that uses it. The rule checks that the endpoint has a privateDnsZoneGroup that references the documented privatelink zones for its group ID, and that each referenced Private DNS zone has a virtualNetworkLink to the VNet that hosts the clients and agents. For the Foundry account the expected set is three zones: privatelink.cognitiveservices.azure.com, privatelink.openai.azure.com and privatelink.services.ai.azure.com. Other expected zones: privatelink.search.windows.net (searchService), privatelink.documents.azure.com (Sql), privatelink.blob.core.windows.net (blob), privatelink.vaultcore.azure.net (vault), privatelink.azurecr.io (registry). Zone names are cloud specific (Azure Government and China use different suffixes), so the expected set is chosen per cloud. A missing zone group or link is the most common cause of private deployments that deploy but cannot connect.

## RECOMMENDATION

Add a privateDnsZoneGroup to each private endpoint with the documented zones (three for a Foundry account) and link every zone to the VNet that resolves the names. If DNS is centralised in a hub or a private resolver, declare that in the Foundry Doctor configuration so the check is skipped with a reason instead of failing.

## EXAMPLES

```
resource zoneGroup 'Microsoft.Network/privateEndpoints/privateDnsZoneGroups@2024-05-01' = {
  parent: pe
  name: 'default'
  properties: {
    privateDnsZoneConfigs: [
      { name: 'cognitiveservices', properties: { privateDnsZoneId: zoneCognitive.id } }
      { name: 'openai',            properties: { privateDnsZoneId: zoneOpenAi.id } }
      { name: 'servicesai',        properties: { privateDnsZoneId: zoneServicesAi.id } }
    ]
  }
}
resource link 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2024-06-01' = {
  parent: zoneCognitive
  name: 'vnet-link'
  location: 'global'
  properties: { virtualNetwork: { id: vnetId }, registrationEnabled: false }
}
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs private endpoints, DNS zone groups, zones and virtual network links correlated.
- How to feed it: Needs a resource-set wrapper object; not implemented.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/private-link/private-endpoint-dns](https://learn.microsoft.com/azure/private-link/private-endpoint-dns) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/private-link/private-endpoint-dns-integration](https://learn.microsoft.com/azure/private-link/private-endpoint-dns-integration) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks](https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/dns/private-dns-virtual-network-links](https://learn.microsoft.com/azure/dns/private-dns-virtual-network-links) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/network/resource-manager/Microsoft.Network/Network/stable/2024-05-01/privateEndpoint.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/network/resource-manager/Microsoft.Network/Network/stable/2024-05-01/privateEndpoint.json) (last verified 2026-10-04)
- [https://github.com/MicrosoftDocs/azure-docs/blob/eb1e05deecc53c7a1a192453a3b8183e45f73c10/articles/private-link/private-endpoint-dns.md](https://github.com/MicrosoftDocs/azure-docs/blob/eb1e05deecc53c7a1a192453a3b8183e45f73c10/articles/private-link/private-endpoint-dns.md) (last verified 2026-10-05)
