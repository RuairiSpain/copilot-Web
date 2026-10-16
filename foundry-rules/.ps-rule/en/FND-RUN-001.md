# FND-RUN-001

## SYNOPSIS

Effective project identity access

## DESCRIPTION

The Foundry project managed identity (system-assigned or user-assigned) must hold the roles the documented standard agent setup requires on the Azure AI Search, Azure Storage and Azure Cosmos DB resources bound to the project capability host. The rule resolves the bound resource IDs from the capability host connections, reads the project identity principal from the project ARM resource, and lists role assignments for that principal at or above each resource scope. Static phase (2) checks role assignments declared in compiled ARM JSON; live phase (3) checks the effective assignments. Only role assignment metadata is read; no data-plane content is read.

## RECOMMENDATION

Assign the project managed identity the roles the Foundry standard agent setup lists on each bound resource, at the narrowest scope that contains the resources. Assign Cosmos DB Built-in Data Contributor with a Cosmos SQL role assignment, not the Access control (IAM) blade.

## EXAMPLES

```
resource searchIndexData 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(search.id, project.id, '8ebe5a00-799e-43f5-93ac-243d3dce84a7')
  scope: search
  properties: {
    principalId: project.identity.principalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '8ebe5a00-799e-43f5-93ac-243d3dce84a7') // Search Index Data Contributor
  }
}
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Runtime probes: project, capability host, connections, role assignments and Cosmos SQL role assignments.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/foundry/agents/concepts/standard-agent-setup](https://learn.microsoft.com/azure/foundry/agents/concepts/standard-agent-setup) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/concepts/administrator-guide](https://learn.microsoft.com/azure/foundry/concepts/administrator-guide) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/search-security-rbac](https://learn.microsoft.com/azure/search/search-security-rbac) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/authorization/resource-manager/Microsoft.Authorization/Authorization/stable/2022-04-01/authorization-RoleAssignmentsCalls.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/authorization/resource-manager/Microsoft.Authorization/Authorization/stable/2022-04-01/authorization-RoleAssignmentsCalls.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2025-10-15/cosmos-db.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2025-10-15/cosmos-db.json) (last verified 2026-10-04)
- [https://github.com/Azure/azure-dev/blob/main/cli/azd/extensions/azure.ai.agents/internal/cmd/doctor/checks_storage_rbac.go](https://github.com/Azure/azure-dev/blob/main/cli/azd/extensions/azure.ai.agents/internal/cmd/doctor/checks_storage_rbac.go) (last verified 2026-10-04)
