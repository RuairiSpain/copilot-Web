# FND-NET-005

## SYNOPSIS

Private-mode VNet/region consistency

## DESCRIPTION

In a private deployment the Foundry account must be in the same Azure region as the virtual network that hosts its agent subnet, and every private endpoint must be in the same region and subscription as the VNet that hosts it. The private-link target (the Foundry account, Storage, Cosmos DB, Search) may be in a different region than that VNet, and the dependent data resources may be in other regions than the Foundry account; those cases are not failures but carry cross-region cost and latency, which the rule reports as informational context.

## RECOMMENDATION

Create the VNet, agent subnet and Foundry account in the same region, and create each private endpoint in the region and subscription of the VNet that hosts it. Cross-region data resources are allowed but cost more in data transfer; keep them in the Foundry region unless you have a reason.

## EXAMPLES

```
param location string = 'swedencentral'
resource vnet 'Microsoft.Network/virtualNetworks@2024-05-01' = { name: vnetName, location: location, properties: { addressSpace: { addressPrefixes: ['10.0.0.0/16'] } } }
resource account 'Microsoft.CognitiveServices/accounts@2026-09-01' = { name: accountName, location: location, kind: 'AIServices', sku: { name: 'S0' }, properties: { /* networkInjections -> subnet in vnet */ } }
resource pe 'Microsoft.Network/privateEndpoints@2024-05-01' = { name: '${accountName}-pe', location: location /* same region as the VNet */, properties: { subnet: { id: peSubnetId } } }
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs the account, subnet, virtual network and private endpoint resources correlated.
- How to feed it: Needs a resource-set wrapper object; not implemented.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks](https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/networking-options](https://learn.microsoft.com/azure/foundry/agents/concepts/networking-options) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/private-link/private-endpoint-overview](https://learn.microsoft.com/azure/private-link/private-endpoint-overview) (last verified 2026-10-04)
