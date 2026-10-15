# FND-DEP-002

## SYNOPSIS

Required providers registered

## DESCRIPTION

The target subscription must be registered for the resource providers a Foundry deployment relies on that ARM will not register by itself. ARM registers providers that the template defines, but a provider that only supplies supporting resources (for example the providers behind a network-secured Agent Service setup) has to be registered manually.

## RECOMMENDATION

Register the missing providers once per subscription with a principal that has the <provider>/register/action permission (Contributor or Owner), and wait until the state is Registered, or at least Registering for the regions in use.

## EXAMPLES

```
az provider register --namespace Microsoft.App
az provider register --namespace Microsoft.ContainerService
az provider show --namespace Microsoft.App --query registrationState
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: ARM Providers_Get or Providers_List registrationState.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/resources/stable/2025-04-01/resources.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/resources/stable/2025-04-01/resources.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/azure-resource-manager/management/resource-providers-and-types](https://learn.microsoft.com/azure/azure-resource-manager/management/resource-providers-and-types) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks](https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/permissions/management-and-governance](https://learn.microsoft.com/azure/role-based-access-control/permissions/management-and-governance) (last verified 2026-10-04)
