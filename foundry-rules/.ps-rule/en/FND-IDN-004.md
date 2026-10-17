# FND-IDN-004

## SYNOPSIS

No broad Owner/Contributor for application identities

## DESCRIPTION

Identities that run the workload (the Foundry project and account managed identities, agent identities, and managed identities of the workloads in the design) should not hold the privileged administrator roles Owner, Contributor, User Access Administrator or Role Based Access Control Administrator, especially at subscription or resource group scope. The Azure RBAC documentation lists these as privileged administrator roles and advises avoiding broad roles at broad scopes. The Foundry documentation gives application identities narrower roles (Foundry User, data-plane roles) and notes that Owner and Contributor alone do not grant Foundry data-plane access, so they are not needed to run agents. Deployment pipeline identities may legitimately need Contributor on a project or resource group and are not treated as application identities unless configured so.

## RECOMMENDATION

Replace Owner, Contributor, User Access Administrator and Role Based Access Control Administrator on application identities with the narrow roles the workload needs, such as Foundry User on the project, Foundry Agent Consumer for callers, and the data-plane roles in FND-IDN-002. Keep any unavoidable privileged role at the narrowest scope and add an ABAC condition when the role can assign roles. Give pipelines a separate deployment identity.

## EXAMPLES

```
// Instead of Contributor on the resource group, grant the project identity only what the agent needs:
resource projectUser 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: account
  name: guid(account.id, project.id, '53ca6127-db72-4b80-b1b0-d745d6d5456d')
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '53ca6127-db72-4b80-b1b0-d745d6d5456d') // Foundry User
    principalId: project.identity.principalId
    principalType: 'ServicePrincipal'
  }
}
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs role assignments correlated with the identities of Foundry accounts, projects and workloads, and scope evaluation.
- How to feed it: Needs a resource-set wrapper object; not implemented.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/role-based-access-control/role-assignments-steps](https://learn.microsoft.com/azure/role-based-access-control/role-assignments-steps) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/best-practices](https://learn.microsoft.com/azure/role-based-access-control/best-practices) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/delegate-role-assignments-overview](https://learn.microsoft.com/azure/role-based-access-control/delegate-role-assignments-overview) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/concepts/rbac-foundry](https://learn.microsoft.com/azure/foundry/concepts/rbac-foundry) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/hosted-agent-permissions](https://learn.microsoft.com/azure/foundry/agents/concepts/hosted-agent-permissions) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/quickstarts/set-up-cicd-hosted-agent](https://learn.microsoft.com/azure/foundry/agents/quickstarts/set-up-cicd-hosted-agent) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/built-in-roles](https://learn.microsoft.com/azure/role-based-access-control/built-in-roles) (last verified 2026-10-04)
