// Source of main.json (compiled with Bicep CLI 0.48.1:
//   bicep build main.bicep --outfile main.json).
// Only structure matters; API versions are those the Bicep type library knows.
// The names use format()/uniqueString() the way azd templates do, so the
// compiled ARM carries real expression shapes.
targetScope = 'resourceGroup'

param location string = resourceGroup().location
param deployPe bool = true
param appIdentityPrincipalId string
param uamiClientId string
param principalId string

var token = uniqueString(resourceGroup().id)

resource ai 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'ai-${token}'
  location: resourceGroup().location
  kind: 'AIServices'
  identity: { type: 'SystemAssigned' }
  sku: { name: 'S0' }
  properties: {
    customSubDomainName: 'ai-${token}'
    publicNetworkAccess: 'Disabled'
    disableLocalAuth: false
  }
}

resource proj 'Microsoft.CognitiveServices/accounts/projects@2025-06-01' = {
  parent: ai
  name: 'proj'
  location: location
  identity: { type: 'SystemAssigned' }
  properties: {}
}

resource speech 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'speech-${token}'
  location: location
  kind: 'SpeechServices'
  sku: { name: 'S0' }
  properties: { publicNetworkAccess: 'Enabled' }
}

resource search 'Microsoft.Search/searchServices@2025-05-01' = {
  name: 'srch-${token}'
  location: location
  sku: { name: 'standard' }
  properties: { disableLocalAuth: false }
}

resource otherSearch 'Microsoft.Search/searchServices@2025-05-01' = {
  name: 'other-${token}'
  location: location
  sku: { name: 'standard' }
  properties: { disableLocalAuth: false }
}

resource condSearch 'Microsoft.Search/searchServices@2025-05-01' = if (deployPe) {
  name: 'cond-${token}'
  location: location
  sku: { name: 'standard' }
  properties: { disableLocalAuth: false }
}

resource cosmos 'Microsoft.DocumentDB/databaseAccounts@2024-11-15' = {
  name: 'cosmos-${token}'
  location: location
  kind: 'GlobalDocumentDB'
  properties: {
    databaseAccountOfferType: 'Standard'
    locations: [ { locationName: location, failoverPriority: 0 } ]
    ipRules: [ { ipAddressOrRange: '0.0.0.0' } ]
  }
}

resource otherCosmos 'Microsoft.DocumentDB/databaseAccounts@2024-11-15' = {
  name: 'unrelated-${token}'
  location: location
  kind: 'GlobalDocumentDB'
  properties: {
    databaseAccountOfferType: 'Standard'
    locations: [ { locationName: location, failoverPriority: 0 } ]
  }
}

resource storage 'Microsoft.Storage/storageAccounts@2023-05-01' = {
  name: 'st${token}'
  location: location
  kind: 'StorageV2'
  sku: { name: 'Standard_LRS' }
  properties: { allowSharedKeyAccess: true, minimumTlsVersion: 'TLS1_2' }
}

resource kvOther 'Microsoft.KeyVault/vaults@2023-07-01' = {
  name: 'kv-${token}'
  location: location
  properties: {
    tenantId: subscription().tenantId
    sku: { family: 'A', name: 'standard' }
    enableRbacAuthorization: false
  }
}

resource searchConn 'Microsoft.CognitiveServices/accounts/connections@2025-06-01' = {
  parent: ai
  name: 'searchconn'
  properties: {
    category: 'CognitiveSearch'
    target: 'https://${search.name}.search.windows.net'
    authType: 'AAD'
    metadata: { ResourceId: search.id }
  }
}

resource storageConn 'Microsoft.CognitiveServices/accounts/connections@2025-06-01' = {
  parent: ai
  name: 'storageconn'
  properties: {
    category: 'AzureStorageAccount'
    target: storage.properties.primaryEndpoints.blob
    authType: 'AccountKey'
    metadata: { ResourceId: storage.id }
  }
}

resource cosmosConn 'Microsoft.CognitiveServices/accounts/connections@2025-06-01' = {
  parent: ai
  name: 'cosmosconn'
  properties: {
    category: 'CosmosDB'
    target: cosmos.properties.documentEndpoint
    authType: 'AAD'
    metadata: { ResourceId: cosmos.id }
  }
}

resource vnet 'Microsoft.Network/virtualNetworks@2024-05-01' = {
  name: 'vnet-${token}'
  location: location
  properties: {
    addressSpace: { addressPrefixes: [ '10.0.0.0/16' ] }
    subnets: [
      { name: 'pe', properties: { addressPrefix: '10.0.1.0/24' } }
      {
        name: 'agents'
        properties: {
          addressPrefix: '10.0.2.0/24'
          delegations: [ { name: 'd', properties: { serviceName: 'Microsoft.App/environments' } } ]
        }
      }
    ]
  }
}

resource peAi 'Microsoft.Network/privateEndpoints@2024-05-01' = if (deployPe) {
  name: 'pe-ai'
  location: location
  properties: {
    subnet: { id: vnet.properties.subnets[0].id }
    privateLinkServiceConnections: [ { name: 'c', properties: { privateLinkServiceId: ai.id, groupIds: [ 'account' ] } } ]
  }
}

resource peSearch 'Microsoft.Network/privateEndpoints@2024-05-01' = {
  name: 'pe-search'
  location: location
  properties: {
    subnet: { id: vnet.properties.subnets[0].id }
    privateLinkServiceConnections: [ { name: 'c', properties: { privateLinkServiceId: search.id, groupIds: [ 'searchService' ] } } ]
  }
}

// A private endpoint for a storage account that Foundry does not use.
resource stOther 'Microsoft.Storage/storageAccounts@2023-05-01' = {
  name: 'other${token}'
  location: location
  kind: 'StorageV2'
  sku: { name: 'Standard_LRS' }
  properties: {}
}

resource peOther 'Microsoft.Network/privateEndpoints@2024-05-01' = {
  name: 'pe-other'
  location: location
  properties: {
    subnet: { id: vnet.properties.subnets[0].id }
    privateLinkServiceConnections: [ { name: 'c', properties: { privateLinkServiceId: stOther.id, groupIds: [ 'blob' ] } } ]
  }
}

resource roleParamPrincipal 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(ai.id, principalId, 'a')
  scope: ai
  properties: {
    principalId: appIdentityPrincipalId
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'b24988ac-6180-42a0-ab88-20f7382dd24c')
    principalType: 'ServicePrincipal'
  }
}

resource roleClientId 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(ai.id, principalId, 'b')
  scope: ai
  properties: {
    principalId: uamiClientId
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'ba92f5b4-2d11-453d-a403-e96b0029c9fe')
    principalType: 'ServicePrincipal'
  }
}

resource roleOwnerProject 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(proj.id, 'owner')
  properties: {
    principalId: proj.identity.principalId
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '8e3af657-a8ff-443c-a75c-2fe8c4bcb635')
    principalType: 'ServicePrincipal'
  }
}
