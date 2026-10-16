// IDN-004 Contributor at a single-resource scope. The info downgrade applies
// only to Foundry resources whose account has disableLocalAuth true.
// Compiled with Bicep CLI 0.48.1.
targetScope = 'resourceGroup'

param location string = resourceGroup().location

// Foundry account with local auth provably disabled.
resource lockedAccount 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'locked'
  location: location
  kind: 'AIServices'
  identity: { type: 'SystemAssigned' }
  sku: { name: 'S0' }
  properties: { customSubDomainName: 'locked', disableLocalAuth: true }
}

resource lockedProj 'Microsoft.CognitiveServices/accounts/projects@2025-06-01' = {
  parent: lockedAccount
  name: 'proj'
  location: location
  identity: { type: 'SystemAssigned' }
  properties: {}
}

// Foundry account that leaves key authentication on.
resource openAccount 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'open'
  location: location
  kind: 'AIServices'
  identity: { type: 'SystemAssigned' }
  sku: { name: 'S0' }
  properties: { customSubDomainName: 'open' }
}

resource store 'Microsoft.Storage/storageAccounts@2023-05-01' = {
  name: 'stgdemo'
  location: location
  kind: 'StorageV2'
  sku: { name: 'Standard_LRS' }
}

resource vault 'Microsoft.KeyVault/vaults@2023-07-01' = {
  name: 'kvdemo'
  location: location
  properties: {
    tenantId: subscription().tenantId
    sku: { family: 'A', name: 'standard' }
    enableRbacAuthorization: true
  }
}

resource contribLockedAccount 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(lockedAccount.id, 'contrib')
  scope: lockedAccount
  properties: {
    principalId: lockedProj.identity.principalId
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'b24988ac-6180-42a0-ab88-20f7382dd24c')
    principalType: 'ServicePrincipal'
  }
}

resource contribOpenAccount 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(openAccount.id, 'contrib')
  scope: openAccount
  properties: {
    principalId: lockedProj.identity.principalId
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'b24988ac-6180-42a0-ab88-20f7382dd24c')
    principalType: 'ServicePrincipal'
  }
}

resource contribStore 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(store.id, 'contrib')
  scope: store
  properties: {
    principalId: lockedProj.identity.principalId
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'b24988ac-6180-42a0-ab88-20f7382dd24c')
    principalType: 'ServicePrincipal'
  }
}

resource contribVault 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(vault.id, 'contrib')
  scope: vault
  properties: {
    principalId: lockedProj.identity.principalId
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'b24988ac-6180-42a0-ab88-20f7382dd24c')
    principalType: 'ServicePrincipal'
  }
}
