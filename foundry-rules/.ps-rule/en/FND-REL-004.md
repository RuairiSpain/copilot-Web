# FND-REL-004

## SYNOPSIS

Delete locks on stateful services

## DESCRIPTION

The Foundry account and the customer-managed state stores it depends on (Cosmos DB, AI Search, Storage) should have a CanNotDelete management lock, directly or inherited from a parent scope. Foundry guidance recommends delete locks on these resources. Locks cover control-plane deletion only, not data-plane operations. WAF/WARA-style recommendation, not a platform constraint.

## RECOMMENDATION

Add a CanNotDelete lock on the Foundry account and on each stateful dependency, or on the resource group that contains only that workload. Creating locks needs Microsoft.Authorization/locks/write (Owner or User Access Administrator, not Contributor). Locks do not protect data-plane content such as individual blobs, indexes or threads; combine with least-privilege RBAC and, optionally, the Azure Policy denyAction effect.

## EXAMPLES

```
resource lock 'Microsoft.Authorization/locks@2020-05-01' = {
  name: 'protect-foundry'
  scope: foundryAccount
  properties: {
    level: 'CanNotDelete'
    notes: 'Foundry account holds projects and connections'
  }
}
```

## NOTES

- Severity dev/test/prod: info/info/warning (PSRule: Information/Information/Warning).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs locks (extension resources with a scope) correlated with the stateful resources.
- How to feed it: Needs a resource-set wrapper object; not implemented.
- This rule never reports Pass in PSRule.

## LINKS

- [https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency](https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/azure-resource-manager/management/lock-resources](https://learn.microsoft.com/azure/azure-resource-manager/management/lock-resources) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Authorization/locks/stable/2020-05-01/locks.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Authorization/locks/stable/2020-05-01/locks.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-policy/blob/master/built-in-policies/policyDefinitions/General/ResourceType_denyActionDelete.json](https://github.com/Azure/azure-policy/blob/master/built-in-policies/policyDefinitions/General/ResourceType_denyActionDelete.json) (last verified 2026-10-04)
