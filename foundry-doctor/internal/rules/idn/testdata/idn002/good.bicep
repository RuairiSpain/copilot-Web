// FND-IDN-002 compiled fixture: a project with a system-assigned identity linked
// to a Search service and a Storage account through project connections, holding
// every catalogue-listed role on them. Compile with Bicep CLI (see README.md).
param location string = resourceGroup().location
param token string = uniqueString(resourceGroup().id)

var searchIndexDataContributor = '8ebe5a00-799e-43f5-93ac-243d3dce84a7'
var searchServiceContributor = '7ca78c08-252a-4471-8644-bb5ff32d4ba0'
var blobDataContributor = 'ba92f5b4-2d11-453d-a403-e96b0029c9fe'
var blobDataOwner = 'b7e6dc6d-f1e8-4753-8033-0f276bb0955b'

resource account 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'ai-${token}'
  location: location
  kind: 'AIServices'
  sku: { name: 'S0' }
  identity: { type: 'SystemAssigned' }
  properties: { allowProjectManagement: true, customSubDomainName: 'ai-${token}' }
}

resource project 'Microsoft.CognitiveServices/accounts/projects@2025-06-01' = {
  parent: account
  name: 'proj'
  location: location
  identity: { type: 'SystemAssigned' }
  properties: {}
}

resource search 'Microsoft.Search/searchServices@2025-05-01' = {
  name: 'srch-${token}'
  location: location
  sku: { name: 'basic' }
  properties: {}
}

resource storage 'Microsoft.Storage/storageAccounts@2023-05-01' = {
  name: 'st${token}'
  location: location
  kind: 'StorageV2'
  sku: { name: 'Standard_LRS' }
}

resource searchConn 'Microsoft.CognitiveServices/accounts/projects/connections@2025-06-01' = {
  parent: project
  name: 'search'
  properties: {
    category: 'CognitiveSearch'
    target: 'https://${search.name}.search.windows.net'
    authType: 'AAD'
  }
}

resource storageConn 'Microsoft.CognitiveServices/accounts/projects/connections@2025-06-01' = {
  parent: project
  name: 'storage'
  properties: {
    category: 'AzureStorageAccount'
    target: storage.properties.primaryEndpoints.blob
    authType: 'AAD'
  }
}

resource searchIndex 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: search
  name: guid(search.id, project.id, searchIndexDataContributor)
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', searchIndexDataContributor)
    principalId: project.identity.principalId
    principalType: 'ServicePrincipal'
  }
}

resource searchService 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: search
  name: guid(search.id, project.id, searchServiceContributor)
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', searchServiceContributor)
    principalId: project.identity.principalId
    principalType: 'ServicePrincipal'
  }
}

resource blobContributor 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: storage
  name: guid(storage.id, project.id, blobDataContributor)
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', blobDataContributor)
    principalId: project.identity.principalId
    principalType: 'ServicePrincipal'
  }
}

resource blobOwner 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: storage
  name: guid(storage.id, project.id, blobDataOwner)
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', blobDataOwner)
    principalId: project.identity.principalId
    principalType: 'ServicePrincipal'
  }
}
