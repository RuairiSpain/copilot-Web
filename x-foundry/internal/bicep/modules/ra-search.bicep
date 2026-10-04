// Role assignment on a Search service.
param searchName string
param principalId string
@allowed([
  'User'
  'Group'
  'ServicePrincipal'
])
param principalType string = 'ServicePrincipal'
@description('Built-in role definition GUID.')
param roleId string

resource target 'Microsoft.Search/searchServices@2024-06-01-preview' existing = {
  name: searchName
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
    roleDefinitionId: roleDefinition.id
  }
}
