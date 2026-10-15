# FND-NET-006

## SYNOPSIS

Private address space and overlap

## DESCRIPTION

The agent subnet must use a supported private address space, and the VNet (and any peered VNet) must not overlap existing networks or the ranges the platform reserves. Documented supported ranges are RFC 1918 (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16); public ranges such as 44.x.x.x are not supported. Documented reserved ranges are 169.254.0.0/16, 172.30.0.0/16, 172.31.0.0/16, 192.0.2.0/24, 0.0.0.0/8, 127.0.0.0/8, 100.100.0.0/17, 100.100.192.0/19 and 100.100.224.0/19. The Foundry virtual network limitations page lists RFC 1918 and RFC 6598 (100.64.0.0/10, CGNAT), excluding the three 100.100 ranges above, so the rule evaluates a prefix in 100.64.0.0/10 like any other: it passes unless it intersects a reserved range. One deep-dive article still says CGNAT is not supported; the rule follows the limitations page.

## RECOMMENDATION

Put the agent subnet in 10.0.0.0/8, 172.16.0.0/12 (avoiding 172.30.0.0/16 and 172.31.0.0/16) or 192.168.0.0/16 and keep the VNet and its peers disjoint from each other and from the reserved ranges. If your IP plan overlaps or you cannot manage ranges, use a Microsoft managed virtual network instead.

## EXAMPLES

```
resource vnet 'Microsoft.Network/virtualNetworks@2024-05-01' = {
  name: vnetName
  location: location
  properties: {
    // avoid 169.254/16, 172.30/16, 172.31/16, 192.0.2/24, 0/8, 127/8 and 100.100.* reserved ranges
    addressSpace: { addressPrefixes: ['10.20.0.0/16'] }
  }
}
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Needs address spaces of peered virtual networks (control plane, live mode).
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks](https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/networking-options](https://learn.microsoft.com/azure/foundry/agents/concepts/networking-options) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/agents-networking-deep-dive](https://learn.microsoft.com/azure/foundry/agents/concepts/agents-networking-deep-dive) (last verified 2026-10-04)
