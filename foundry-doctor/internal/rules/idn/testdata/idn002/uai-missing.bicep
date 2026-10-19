// FND-IDN-002 compiled fixture: a project with a user-assigned identity linked to a
// Search service; the identity holds Search Index Data Contributor only, so Search
// Service Contributor is missing. Compile with Bicep CLI (see README.md).
param location string = resourceGroup().location
param token string = uniqueString(resourceGroup().id)

var searchIndexDataContributor = '8ebe5a00-799e-43f5-93ac-243d3dce84a7'

resource uai 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  name: 'id-${token}'
  location: location
}

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
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: { '${uai.id}': {} }
  }
  properties: {}
}

resource search 'Microsoft.Search/searchServices@2025-05-01' = {
  name: 'srch-${token}'
  location: location
  sku: { name: 'basic' }
  properties: {}
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

resource searchIndex 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: search
  name: guid(search.id, uai.id, searchIndexDataContributor)
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', searchIndexDataContributor)
    principalId: uai.properties.principalId
    principalType: 'ServicePrincipal'
  }
}
