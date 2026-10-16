# FND-NET-004

## SYNOPSIS

Agent subnet delegation and sizing

## DESCRIPTION

A Foundry account that injects Agent Service into your VNet (properties.networkInjections with scenario agent and a subnetArmId) needs a dedicated agent subnet that is delegated to Microsoft.App/environments and is no smaller than /27. The subnet cannot be shared by more than one Foundry resource, and for Azure Container Apps-backed deployments its name must not exceed 63 UTF-8 bytes. The size cannot be changed after the subnet is assigned. The rule is skipped when useMicrosoftManagedNetwork is true (Microsoft manages the network). Capacity advice (/24 for production hosted agents) is not part of this rule; see the proposed capacity rule in the overlap fragment.

## RECOMMENDATION

Create a dedicated subnet for each Foundry account, delegate it to Microsoft.App/environments, size it /27 or larger (the Foundry docs recommend /24 for production hosted agents), keep the name at 63 bytes or fewer, and do not reuse it for another Foundry resource or other services. Set the network injection when you create the account; it cannot be added to an existing account for hosted agents.

## EXAMPLES

```
resource agentSubnet 'Microsoft.Network/virtualNetworks/subnets@2024-05-01' = {
  parent: vnet
  name: 'agent-subnet'
  properties: {
    addressPrefix: '10.0.0.0/24'
    delegations: [{
      name: 'Microsoft.App.environments'
      properties: { serviceName: 'Microsoft.App/environments' }
    }]
  }
}
// on the account:
//   networkInjections: [{ scenario: 'agent', subnetArmId: agentSubnet.id, useMicrosoftManagedNetwork: false }]
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs the account networkInjections subnet resolved to the subnet resource and other accounts compared.
- How to feed it: Needs a resource-set wrapper object; not implemented.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks](https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/networking-options](https://learn.microsoft.com/azure/foundry/agents/concepts/networking-options) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/agents-networking-deep-dive](https://learn.microsoft.com/azure/foundry/agents/concepts/agents-networking-deep-dive) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/network/resource-manager/Microsoft.Network/Network/stable/2024-05-01/virtualNetwork.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/network/resource-manager/Microsoft.Network/Network/stable/2024-05-01/virtualNetwork.json) (last verified 2026-10-04)
