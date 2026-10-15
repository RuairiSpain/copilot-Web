# FND-COST-001

## SYNOPSIS

Budget and alerts exist

## DESCRIPTION

The subscription or resource group that hosts a test or production Foundry deployment should have an Azure Cost Management budget with at least one enabled notification that reaches a person or an action group. Budgets notify; they do not stop spending or affect resources. Whether a budget is required is a Foundry Doctor recommendation, not a platform rule.

## RECOMMENDATION

Add a Microsoft.Consumption/budgets resource for the subscription or resource group with an actual-cost notification (for example 80 percent) and a forecasted-cost notification, sent to a mailbox or an action group that someone monitors.

## EXAMPLES

```
targetScope = 'subscription'

resource budget 'Microsoft.Consumption/budgets@2023-11-01' = {
  name: 'foundry-monthly'
  properties: {
    category: 'Cost'
    amount: 500
    timeGrain: 'Monthly'
    timePeriod: { startDate: '2026-11-01T00:00:00Z' }
    notifications: {
      actual80: {
        enabled: true
        operator: 'GreaterThan'
        threshold: 80
        thresholdType: 'Actual'
        contactEmails: [ 'finops@contoso.example' ]
      }
    }
  }
}
```

## NOTES

- Severity dev/test/prod: info/info/warning (PSRule: Information/Information/Warning).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Offline, a template with no budget is reported as skipped by the catalogue (budgets are usually created outside the repository). The positive check lists budgets with the Budgets API.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/consumption/resource-manager/Microsoft.Consumption/Consumption/stable/2023-11-01](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/consumption/resource-manager/Microsoft.Consumption/Consumption/stable/2023-11-01) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/cost-management-billing/costs/quick-create-budget-bicep](https://learn.microsoft.com/azure/cost-management-billing/costs/quick-create-budget-bicep) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/cost-management-billing/costs/tutorial-acm-create-budgets](https://learn.microsoft.com/azure/cost-management-billing/costs/tutorial-acm-create-budgets) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/concepts/manage-costs](https://learn.microsoft.com/azure/foundry/concepts/manage-costs) (last verified 2026-10-04)
