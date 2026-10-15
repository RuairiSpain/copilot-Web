# FND-NET-010

## SYNOPSIS

Azure Monitor private-link choice is explicit

## DESCRIPTION

In a private Foundry design, the telemetry path (Application Insights component and its Log Analytics workspace) must be a deliberate choice: either connected through an Azure Monitor Private Link Scope (AMPLS) with explicit ingestion and query access modes, or declared as public telemetry. The Foundry documentation states that Application Insights reaches a private Foundry setup through an AMPLS. The documentation also warns that a Private Only access mode blocks traffic to Azure Monitor resources outside the AMPLS for every network that shares the same DNS, and that several AMPLS objects on one DNS override each other. The platform does not require AMPLS, so the rule asks for an explicit decision and checks that the chosen option is internally consistent. It does not claim that public telemetry is a platform error.

## RECOMMENDATION

Decide the telemetry path. For private telemetry, create one AMPLS per DNS boundary, add the Application Insights component and its workspace as scoped resources, create the private endpoint with group ID azuremonitor and link the Azure Monitor private DNS zones, and set both access modes explicitly. Use Open while resources are still being added and switch to Private Only only when all Azure Monitor resources are in the scope. If you accept public telemetry, record that decision in the Foundry Doctor config so the finding is suppressed with a reason.

## EXAMPLES

```
resource ampls 'Microsoft.Insights/privateLinkScopes@2021-09-01' = {
  name: 'ampls-${name}'
  location: 'global'
  properties: {
    accessModeSettings: { ingestionAccessMode: 'Open', queryAccessMode: 'Open' } // explicit; move to PrivateOnly once every resource is scoped
  }
}
resource scoped 'Microsoft.Insights/privateLinkScopes/scopedResources@2021-09-01' = {
  parent: ampls
  name: 'appi'
  properties: { linkedResourceId: appInsights.id }
}
// private endpoint with groupIds: ['azuremonitor'] and privateLinkServiceId: ampls.id
```

## NOTES

- Severity dev/test/prod: info/warning/warning (PSRule: Information/Warning/Warning).
- Catalogue status: product-opinion. Catalogue decision: adapt.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs Application Insights, Azure Monitor private link scopes, scoped resources and private endpoints correlated, plus a profile declaration.
- How to feed it: Needs a resource-set wrapper object; not implemented.
- This rule never reports Pass in PSRule.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/azure-monitor/fundamentals/private-link-design](https://learn.microsoft.com/azure/azure-monitor/fundamentals/private-link-design) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks](https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/includes/how-to-managed-virtual-network-content](https://learn.microsoft.com/azure/foundry/includes/how-to-managed-virtual-network-content) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/monitor/resource-manager/Microsoft.Insights/Insights/stable/2021-09-01/privateLinkScopes_API.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/monitor/resource-manager/Microsoft.Insights/Insights/stable/2021-09-01/privateLinkScopes_API.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/applicationinsights/resource-manager/Microsoft.Insights/ApplicationInsights/Components/models.tsp](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/applicationinsights/resource-manager/Microsoft.Insights/ApplicationInsights/Components/models.tsp) (last verified 2026-10-04)
