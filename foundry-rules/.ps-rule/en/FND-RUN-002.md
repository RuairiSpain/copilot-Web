# FND-RUN-002

## SYNOPSIS

Private DNS resolves from an appropriate vantage point

## DESCRIPTION

For each private-endpoint-backed dependency of a Foundry project (Foundry account, Azure AI Search, Azure Cosmos DB, Azure Storage blob), the service FQDN must resolve to a private IP address when resolved from inside the virtual network that hosts the agent and private endpoint subnets. Resolution from outside the VNet is not evidence either way, so the rule is Skipped there and is never Passed.

## RECOMMENDATION

Link each required privatelink zone to the VNet that hosts the agent client and private endpoint subnets, configure conditional forwarders to 168.63.129.16 where custom DNS is used, and re-run the doctor from a machine inside the VNet.

## EXAMPLES

```
resource searchDnsLink 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2018-09-01' = {
  parent: searchPrivateDnsZone // privatelink.search.windows.net
  name: 'link-to-agent-vnet'
  location: 'global'
  properties: {
    virtualNetwork: { id: agentVnet.id }
    registrationEnabled: false
  }
}
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: DNS resolution from an operator-declared in-VNet vantage point plus private DNS zone link reads.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.AI.PrivateEndpoints.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks](https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/privatedns/resource-manager/Microsoft.Network/PrivateDns/stable/2018-09-01/privatedns.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/privatedns/resource-manager/Microsoft.Network/PrivateDns/stable/2018-09-01/privatedns.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/agents-networking-deep-dive](https://learn.microsoft.com/azure/foundry/agents/concepts/agents-networking-deep-dive) (last verified 2026-10-04)
