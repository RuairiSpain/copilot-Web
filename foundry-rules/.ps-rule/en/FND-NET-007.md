# FND-NET-007

## SYNOPSIS

Restricted mode has allowed IP range

## DESCRIPTION

"Restricted mode" is the Foundry account state where public access stays enabled but is limited to selected networks (portal: Enabled from selected IP addresses). In ARM this is publicNetworkAccess Enabled together with networkAcls.defaultAction Deny. A restricted account must name who is allowed: at least one ipRules entry (an IPv4 address or CIDR) or one virtualNetworkRules entry. Without any, no public caller matches a rule, so the account is restricted for everybody and the configuration is almost certainly not what the author intended. The PRD does not define "restricted mode"; this definition is derived from the Foundry network isolation article and the ARM schema and should be confirmed by the product owner.

## RECOMMENDATION

If you want restricted mode, list the egress IPs of your build agents and developer networks as ipRules (or subnets as virtualNetworkRules). If you want fully private access, set publicNetworkAccess to Disabled and use a private endpoint instead.

## EXAMPLES

```
properties: {
  publicNetworkAccess: 'Enabled'
  networkAcls: {
    defaultAction: 'Deny'
    bypass: 'AzureServices'
    ipRules: [{ value: '203.0.113.0/24' }]
  }
}
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: The catalogue skips this rule when an approved private endpoint targets the account; that needs resource correlation. Without it the rule would report an intentional private-only posture as a failure.
- How to feed it: Needs a resource-set wrapper object; not implemented.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.AI.PublicAccess.

## LINKS

- [https://learn.microsoft.com/azure/foundry/how-to/configure-private-link](https://learn.microsoft.com/azure/foundry/how-to/configure-private-link) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/networking-options](https://learn.microsoft.com/azure/foundry/agents/concepts/networking-options) (last verified 2026-10-04)
