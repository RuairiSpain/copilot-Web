// Role assignment on a Cosmos DB account (control plane).
param cosmosName string
param principalId string
@allowed([
  'User'
  'Group'
  'ServicePrincipal'
])
param principalType string = 'ServicePrincipal'
@description('Built-in role definition GUID.')
param roleId string
@description('Shown on the role assignment in the portal.')
param assignmentDescription string = ''

resource target 'Microsoft.DocumentDB/databaseAccounts@2024-11-15' existing = {
  name: cosmosName
}

resource roleDefinition 'Microsoft.Authorization/roleDefinitions@2022-04-01' existing = {
  scope: subscription()
  name: roleId
}

resource assignment 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: target
  name: guid(target.id, principalId, roleId)
  properties: {
    principalId: principalId
    principalType: principalType
    description: empty(assignmentDescription) ? null : assignmentDescription
    roleDefinitionId: roleDefinition.id
  }
}
