# FND-NET-011

## SYNOPSIS

NSG/DDoS recommendations

## DESCRIPTION

Subnets in the virtual network that hosts a private Foundry design should have a network security group (NSG), and a design that exposes public IP addresses should have DDoS protection beyond the default infrastructure protection. These are Well-Architected style recommendations, not Foundry platform requirements. The rule reports subnets without an NSG, with a caveat for the special purpose subnets that do not support one, and reports public IP addresses in the design without DDoS Protection. A fully private design with no public IP address gets no DDoS finding. The Foundry sources read do not state NSG requirements for the agent subnet, so that subnet is reported as a lower-confidence recommendation.

## RECOMMENDATION

Associate an NSG with each subnet that supports one and review inbound allow rules. If the design exposes public IP addresses, enable Azure DDoS Protection (IP Protection on the public IP, or Network Protection on the VNet with a plan). For a fully private design, rely on the default infrastructure protection and do not buy a plan for it.

## EXAMPLES

```
resource nsg 'Microsoft.Network/networkSecurityGroups@2024-05-01' = { name: 'nsg-pe', location: location, properties: { securityRules: [] } }
resource vnet 'Microsoft.Network/virtualNetworks@2024-05-01' = {
  name: vnetName
  location: location
  properties: {
    addressSpace: { addressPrefixes: ['10.0.0.0/16'] }
    subnets: [ { name: 'pe', properties: { addressPrefix: '10.0.1.0/24', networkSecurityGroup: { id: nsg.id } } } ]
  }
}
```

## NOTES

- Severity dev/test/prod: info/warning/warning (PSRule: Information/Warning/Warning).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs virtual network subnets, NSG associations, public IPs and DDoS plans correlated.
- How to feed it: Needs a resource-set wrapper object; not implemented.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.VNET.UseNSGs, Azure.NSG.AnyInboundSource.

## LINKS

- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.VNET.UseNSGs/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.VNET.UseNSGs/) (last verified 2026-10-04)
- [https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.NSG.AnyInboundSource/](https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.NSG.AnyInboundSource/) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/ddos-protection/ddos-protection-sku-comparison](https://learn.microsoft.com/azure/ddos-protection/ddos-protection-sku-comparison) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/network/resource-manager/Microsoft.Network/Network/stable/2026-01-01/virtualNetwork.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/network/resource-manager/Microsoft.Network/Network/stable/2026-01-01/virtualNetwork.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/openai/how-to/troubleshoot-errors](https://learn.microsoft.com/azure/foundry/openai/how-to/troubleshoot-errors) (last verified 2026-10-04)
