// Compiled with Bicep CLI 0.48.1: bicep build main.bicep --outfile main.json
// Parameter-driven conditions, names, regions, SKUs and tags (ADR-015).
targetScope = 'resourceGroup'

param location string = resourceGroup().location
param deploySearch bool = false
param publicNetworkAccess string = 'Enabled'
param prefix string
param capacity int = 1
@secure()
param secretTagValue string = 'placeholder'
param tags object = {}

var ownerRoleId = '8e3af657-a8ff-443c-a75c-2fe8c4bcb635'
var ownerRoleDefinitionId = subscriptionResourceId('Microsoft.Authorization/roleDefinitions', ownerRoleId)
var searchName = '${prefix}-srch'

resource ai 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: '${prefix}-ai'
  location: location
  kind: 'AIServices'
  sku: { name: 'S0' }
  tags: tags
  properties: {
    customSubDomainName: '${prefix}-ai'
    publicNetworkAccess: publicNetworkAccess
  }
}

resource search 'Microsoft.Search/searchServices@2025-05-01' = if (deploySearch) {
  name: searchName
  location: location
  sku: { name: 'basic' }
  tags: { owner: 'platform' }
  properties: {
    publicNetworkAccess: publicNetworkAccess
    replicaCount: capacity
  }
}

resource secretTagged 'Microsoft.Storage/storageAccounts@2023-05-01' = {
  name: '${prefix}st'
  location: location
  kind: 'StorageV2'
  sku: { name: 'Standard_LRS' }
  tags: { token: secretTagValue }
}

resource untagged 'Microsoft.Storage/storageAccounts@2023-05-01' = {
  name: '${prefix}plain'
  location: location
  kind: 'StorageV2'
  sku: { name: 'Standard_LRS' }
}

module searchModule 'modules/search.bicep' = if (deploySearch) {
  name: 'search-module'
  params: {
    name: '${prefix}-msrch'
    location: location
    publicNetworkAccess: publicNetworkAccess
  }
}
