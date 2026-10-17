# FND-DEP-010

## SYNOPSIS

Locks do not block change

## DESCRIPTION

Resource locks at or above the deployment scope must not block the changes the deployment will make. A ReadOnly lock blocks creating, updating and deleting in its scope, a CanNotDelete lock blocks deletes, and locks are inherited by child scopes.

## RECOMMENDATION

Remove or relax the lock for the duration of the deployment through your change process, deploy to a scope without a ReadOnly lock, or scope locks to the specific resources that need protection and reapply them after the deployment.

## EXAMPLES

```
az lock list --resource-group <rg> --output table
az lock delete --name <lock-name> --resource-group <rg>   # then redeploy and re-create the lock
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: ARM Microsoft.Authorization locks lists and the deployment history count.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://learn.microsoft.com/azure/azure-resource-manager/management/lock-resources](https://learn.microsoft.com/azure/azure-resource-manager/management/lock-resources) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Authorization/locks/stable/2020-05-01/locks.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Authorization/locks/stable/2020-05-01/locks.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/permissions/management-and-governance](https://learn.microsoft.com/azure/role-based-access-control/permissions/management-and-governance) (last verified 2026-10-04)
