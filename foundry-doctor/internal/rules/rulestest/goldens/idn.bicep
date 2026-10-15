// IDN-004 shapes: role assignments without principalType whose principal is a
// Foundry identity, a role definition ID held in a variable, and Contributor at
// the account scope. Compiled with Bicep CLI 0.48.1.
targetScope = 'resourceGroup'

param location string = resourceGroup().location
param someUser string

var ownerRoleId = '8e3af657-a8ff-443c-a75c-2fe8c4bcb635'
var contributorRoleId = 'b24988ac-6180-42a0-ab88-20f7382dd24c'

resource account 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'aiservices'
  location: location
  kind: 'AIServices'
  identity: { type: 'SystemAssigned' }
  sku: { name: 'S0' }
  properties: { customSubDomainName: 'aiservices' }
}

resource proj 'Microsoft.CognitiveServices/accounts/projects@2025-06-01' = {
  parent: account
  name: 'proj'
  location: location
  identity: { type: 'SystemAssigned' }
  properties: {}
}

// Owner for the project identity, no principalType.
resource ownerNoType 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(resourceGroup().id, 'owner-notype')
  properties: {
    principalId: proj.identity.principalId
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '8e3af657-a8ff-443c-a75c-2fe8c4bcb635')
  }
}

// Owner through a variable role id.
resource ownerVar 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(resourceGroup().id, 'owner-var')
  properties: {
    principalId: account.identity.principalId
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', ownerRoleId)
    principalType: 'ServicePrincipal'
  }
}

// Contributor at the account scope.
resource contribAccount 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(account.id, 'contrib')
  scope: account
  properties: {
    principalId: proj.identity.principalId
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', contributorRoleId)
    principalType: 'ServicePrincipal'
  }
}

// Owner for an unrelated principal supplied by a parameter, no principalType.
resource ownerUnknown 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(resourceGroup().id, 'owner-unknown')
  properties: {
    principalId: someUser
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '8e3af657-a8ff-443c-a75c-2fe8c4bcb635')
  }
}
