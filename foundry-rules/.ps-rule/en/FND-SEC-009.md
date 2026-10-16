# FND-SEC-009

## SYNOPSIS

Policy coverage and compliance evidence

## DESCRIPTION

Evidence report on whether Azure Policy assignments at the subscription or resource group scope cover the built-in definitions that Foundry Doctor maps in SEC-001 to SEC-006 and SEC-010, whether any assignment is in DoNotEnforce mode, and what the current compliance state is. Informational in dev, warning in prod. The expectation that an environment assigns these policies is a product opinion.

## RECOMMENDATION

Assign the built-in policies that correspond to the Foundry Doctor security rules (for example local-auth, public network access, CMK) at subscription or management-group scope, in Default enforcement mode once existing resources are compliant.

## EXAMPLES

```
resource assign 'Microsoft.Authorization/policyAssignments@2025-01-01' = {
  name: 'foundry-no-local-auth'
  properties: {
    policyDefinitionId: tenantResourceId('Microsoft.Authorization/policyDefinitions', '71ef260a-8f18-47b7-abcb-62d0673d94dc')
    enforcementMode: 'Default'
  }
}
```

## NOTES

- Severity dev/test/prod: info/info/warning (PSRule: Information/Information/Warning).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Policy assignments and PolicyInsights policy states at subscription and resource group scope.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Authorization/policy/stable/2025-01-01/policyAssignments.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Authorization/policy/stable/2025-01-01/policyAssignments.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/policyinsights/resource-manager/Microsoft.PolicyInsights/PolicyInsights/stable/2024-10-01/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/policyinsights/resource-manager/Microsoft.PolicyInsights/PolicyInsights/stable/2024-10-01/openapi.json) (last verified 2026-10-04)
