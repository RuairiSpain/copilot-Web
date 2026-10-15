# FND-IDN-003

## SYNOPSIS

Role assignments use object IDs and principalType

## DESCRIPTION

Role assignments in the template must target a principal object ID, not an application (client) ID, and must set principalType. The RBAC documentation states that assigning a role to a newly created service principal or managed identity in the same template can fail because of directory replication delay, and that setting principalType to ServicePrincipal (with API version 2018-09-01-preview or later; 2022-04-01 is the first stable version) avoids it. The Foundry documentation also shows the object-ID versus client-ID confusion for managed identities (the managed identity object ID must be searched in enterprise applications to find the client ID). The rule checks the role assignment resources in compiled Bicep or ARM JSON.

## RECOMMENDATION

Pass the object ID of the identity (identity.principalId or the user-assigned identity's properties.principalId), never the client ID, and set principalType explicitly. Use ServicePrincipal for managed identities and service principals created in the same deployment, and use API version 2022-04-01 or later.

## EXAMPLES

```
resource blobRole 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: storage
  name: guid(storage.id, uami.id, 'ba92f5b4-2d11-453d-a403-e96b0029c9fe')
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'ba92f5b4-2d11-453d-a403-e96b0029c9fe')
    principalId: uami.properties.principalId // object ID, not uami.properties.clientId
    principalType: 'ServicePrincipal'
  }
}
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: principalType present on every role assignment. The ServicePrincipal check for identities created in the same template, the object-ID check and the apiVersion check need correlation and are not evaluated. The catalogue marks as unverified whether omitting principalType always fails.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/role-based-access-control/role-assignments-template](https://learn.microsoft.com/azure/role-based-access-control/role-assignments-template) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/troubleshooting](https://learn.microsoft.com/azure/role-based-access-control/troubleshooting) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/authorization/resource-manager/Microsoft.Authorization/Authorization/stable/2022-04-01/authorization-RoleAssignmentsCalls.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/authorization/resource-manager/Microsoft.Authorization/Authorization/stable/2022-04-01/authorization-RoleAssignmentsCalls.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/msi/resource-manager/Microsoft.ManagedIdentity/ManagedIdentity/stable/2024-11-30/ManagedIdentity.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/msi/resource-manager/Microsoft.ManagedIdentity/ManagedIdentity/stable/2024-11-30/ManagedIdentity.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/how-to/ai-gateway](https://learn.microsoft.com/azure/foundry/agents/how-to/ai-gateway) (last verified 2026-10-04)
