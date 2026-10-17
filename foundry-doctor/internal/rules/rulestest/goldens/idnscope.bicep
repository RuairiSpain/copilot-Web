// IDN-004 resource scope: Owner and User Access Administrator granted to the
// project identity with `scope: account`. Compiled with Bicep CLI 0.48.1, the
// scope is [resourceId('Microsoft.CognitiveServices/accounts', 'aiservices')].
targetScope = 'resourceGroup'

param location string = resourceGroup().location

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

resource ownerAtAccount 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(account.id, 'owner')
  scope: account
  properties: {
    principalId: proj.identity.principalId
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '8e3af657-a8ff-443c-a75c-2fe8c4bcb635')
    principalType: 'ServicePrincipal'
  }
}

resource uaaAtAccount 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(account.id, 'uaa')
  scope: account
  properties: {
    principalId: proj.identity.principalId
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '18d7d88d-d35e-4fb5-a5c3-7773c20a72d9')
    principalType: 'ServicePrincipal'
  }
}

resource contribAtAccount 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(account.id, 'contrib')
  scope: account
  properties: {
    principalId: proj.identity.principalId
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'b24988ac-6180-42a0-ab88-20f7382dd24c')
    principalType: 'ServicePrincipal'
  }
}
