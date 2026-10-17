# FND-IDN-002

## SYNOPSIS

Project identity has required data roles

## DESCRIPTION

In the standard agent setup the Foundry project managed identity needs data-plane role assignments on the resources named in the account capability settings, otherwise running agents fail with 403. The documented set is: Search Index Data Contributor and Search Service Contributor on Azure AI Search; Storage Blob Data Contributor on the blob store container and Storage Blob Data Owner on the agent container, plus Storage Account Contributor at account level; Cosmos DB Operator at the Cosmos DB account and the Cosmos DB Built-in Data Contributor SQL role at the database level. Hosted agents add Foundry User on the account (created automatically when the project creator can assign it), a Container Registry Repository Reader or AcrPull assignment on the registry, and, for evaluations, Log Analytics Data Reader on the workspace. Phase 1 checks the role assignments declared in the template. Phase 2 reads the live assignments. The rule applies only to the stores and features the project actually uses.

## RECOMMENDATION

Assign the project managed identity the documented data roles on each store before agents run: Search Index Data Contributor and Search Service Contributor on Search, Storage Blob Data Contributor and Storage Blob Data Owner on the storage containers, Cosmos DB Operator on the account plus the Cosmos DB Built-in Data Contributor SQL role on the database. Assign them at the narrowest scope that works. These roles are separate from the roles the deploying identity needs.

## EXAMPLES

```
resource searchIndexData 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: search
  name: guid(search.id, project.id, '8ebe5a00-799e-43f5-93ac-243d3dce84a7')
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '8ebe5a00-799e-43f5-93ac-243d3dce84a7') // Search Index Data Contributor
    principalId: project.identity.principalId
    principalType: 'ServicePrincipal'
  }
}
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Effective permission calculation over role definitions, deny assignments, conditions and Cosmos SQL role assignments (control plane).
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/concepts/standard-agent-setup](https://learn.microsoft.com/azure/foundry/agents/concepts/standard-agent-setup) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/concepts/administrator-guide](https://learn.microsoft.com/azure/foundry/concepts/administrator-guide) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/how-to/configure-capability-settings](https://learn.microsoft.com/azure/foundry/how-to/configure-capability-settings) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/concepts/hosted-agent-permissions](https://learn.microsoft.com/azure/foundry/agents/concepts/hosted-agent-permissions) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/role-based-access-control/built-in-roles](https://learn.microsoft.com/azure/role-based-access-control/built-in-roles) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2025-10-15/rbac.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2025-10-15/rbac.json) (last verified 2026-10-04)
