@description('Name of the existing Cosmos DB (SQL API) account.')
param cosmosDbAccountName string

@description('Principal ID (object ID) of the identity to grant the Cosmos DB built-in data contributor role.')
param principalId string

// Cosmos DB built-in "Cosmos DB Built-in Data Contributor" native (data-plane) role.
var docDbAccNativeContributorRoleDefinitionId = '00000000-0000-0000-0000-000000000002'

resource cosmosDbAccount 'Microsoft.DocumentDB/databaseAccounts@2024-02-15-preview' existing = {
  name: cosmosDbAccountName
}

// Cosmos DB validates the principal against AAD synchronously when the SQL role assignment is
// created and does NOT support a principalType hint. This module is intentionally deployed in a
// separate stage from the managed identity creation so the new principal has time to replicate in
// AAD before this assignment runs, avoiding the transient
// "principal ID ... was not found in the AAD tenant" BadRequest.
resource sqlRoleAssignment 'Microsoft.DocumentDB/databaseAccounts/sqlRoleAssignments@2023-04-15' = {
  name: guid(docDbAccNativeContributorRoleDefinitionId, principalId, cosmosDbAccount.id)
  parent: cosmosDbAccount
  properties: {
    principalId: principalId
    roleDefinitionId: '/${cosmosDbAccount.id}/sqlRoleDefinitions/${docDbAccNativeContributorRoleDefinitionId}'
    scope: cosmosDbAccount.id
  }
}
