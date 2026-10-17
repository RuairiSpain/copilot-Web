# FND-DEP-011

## SYNOPSIS

Subnet delegation/capacity/use is compatible

## DESCRIPTION

When a Foundry Agent Service deployment injects into a customer virtual network, the agent subnet must be delegated to Microsoft.App/environments, sized /27 or larger (the Learn portal step) with /24 recommended, use an allowed private address space that does not overlap reserved ranges, not be shared with another Foundry resource, have a name within the documented length, and be in the same region as the Foundry resource.

## RECOMMENDATION

Use a dedicated subnet of /24 (at least /27) in an allowed private range, delegate it to Microsoft.App/environments, keep it in the same region as the Foundry resource, and give each Foundry resource its own agent subnet.

## EXAMPLES

```
resource agentSubnet 'Microsoft.Network/virtualNetworks/subnets@2025-05-01' = {
  parent: vnet
  name: 'agent-subnet'
  properties: {
    addressPrefix: '192.168.0.0/24'
    delegations: [
      { name: 'Microsoft.App/environments', properties: { serviceName: 'Microsoft.App/environments' } }
    ]
  }
}
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: ARM subnet, serviceAssociationLinks and resourceNavigationLinks reads.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks](https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/limits-quotas-regions](https://learn.microsoft.com/azure/foundry/agents/concepts/limits-quotas-regions) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/network/resource-manager/Microsoft.Network/Network/stable/2025-05-01/virtualNetwork.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/network/resource-manager/Microsoft.Network/Network/stable/2025-05-01/virtualNetwork.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/permissions/networking](https://learn.microsoft.com/azure/role-based-access-control/permissions/networking) (last verified 2026-10-04)
