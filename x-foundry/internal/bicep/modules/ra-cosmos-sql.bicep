// Cosmos DB built-in Data Contributor for a project identity, scoped to the enterprise_memory
// database the agent service creates. It must be assigned after the project capability host exists.
param cosmosName string
param principalId string
@description('The project workspace id: 32 hexadecimal characters.')
param workspaceId string

resource cosmos 'Microsoft.DocumentDB/databaseAccounts@2024-11-15' existing = {
  name: cosmosName
}

var dataContributor = resourceId('Microsoft.DocumentDB/databaseAccounts/sqlRoleDefinitions', cosmosName, '00000000-0000-0000-0000-000000000002')

resource assignment 'Microsoft.DocumentDB/databaseAccounts/sqlRoleAssignments@2024-11-15' = {
  parent: cosmos
  name: guid(workspaceId, cosmosName, dataContributor, principalId)
  properties: {
    principalId: principalId
    roleDefinitionId: dataContributor
    scope: '${cosmos.id}/dbs/enterprise_memory'
  }
}
