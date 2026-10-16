# FND-DEP-003

## SYNOPSIS

Deployer can create resources and role assignments

## DESCRIPTION

The identity that runs azd provision needs write access on the resources being deployed, access to all operations on Microsoft.Resources/deployments, and, when the template contains role assignments, the ability to write Microsoft.Authorization/roleAssignments. The doctor can show that a required permission is missing. It cannot prove that a permission present in the effective list is enough to succeed.

## RECOMMENDATION

Assign the deployer a role that includes the missing write actions at the narrowest sufficient scope. For templates that create role assignments, Owner or User Access Administrator (or a custom role with Microsoft.Authorization/roleAssignments/write) is needed in addition to a resource-write role.

## EXAMPLES

```
az role assignment create --assignee <deployer-object-id> --role "Contributor" --scope /subscriptions/<sub-id>/resourceGroups/<rg>
az role assignment create --assignee <deployer-object-id> --role "Role Based Access Control Administrator" --scope /subscriptions/<sub-id>/resourceGroups/<rg>
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: ARM Permissions_ListForResourceGroup and role assignment lists for the caller.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/authorization/resource-manager/Microsoft.Authorization/Authorization/stable/2022-04-01/authorization-RoleDefinitionsCalls.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/authorization/resource-manager/Microsoft.Authorization/Authorization/stable/2022-04-01/authorization-RoleDefinitionsCalls.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/authorization/resource-manager/Microsoft.Authorization/Authorization/stable/2022-04-01/authorization-RoleAssignmentsCalls.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/authorization/resource-manager/Microsoft.Authorization/Authorization/stable/2022-04-01/authorization-RoleAssignmentsCalls.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/azure-resource-manager/templates/deploy-what-if](https://learn.microsoft.com/azure/azure-resource-manager/templates/deploy-what-if) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/permissions/management-and-governance](https://learn.microsoft.com/azure/role-based-access-control/permissions/management-and-governance) (last verified 2026-10-04)
- [https://github.com/Azure/azure-dev/blob/main/cli/azd/docs/design/provision-validation.md](https://github.com/Azure/azure-dev/blob/main/cli/azd/docs/design/provision-validation.md) (last verified 2026-10-04)
