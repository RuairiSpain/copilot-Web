// Foundry project with its connections to bring-your-own resources (standard agent setup).
param accountName string
param name string
param location string
param displayName string = ''
param projectDescription string = ''
param tags object = {}
@description('Objects with name, category, target, resourceId and location.')
param connections array = []

resource account 'Microsoft.CognitiveServices/accounts@2025-06-01' existing = {
  name: accountName
}

resource project 'Microsoft.CognitiveServices/accounts/projects@2025-06-01' = {
  parent: account
  name: name
  location: location
  tags: tags
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    displayName: empty(displayName) ? name : displayName
    description: empty(projectDescription) ? null : projectDescription
  }
}

resource projectConnections 'Microsoft.CognitiveServices/accounts/projects/connections@2025-06-01' = [for c in connections: {
  parent: project
  name: c.name
  properties: {
    category: c.category
    target: c.target
    authType: 'AAD'
    metadata: {
      ApiType: 'Azure'
      ResourceId: c.resourceId
      location: c.location
    }
  }
}]

output id string = project.id
output name string = project.name
output principalId string = project.identity.principalId
// The workspace id formatted as a GUID; Cosmos DB and Storage role conditions use it.
// The type definitions do not list internalId, but the API returns it.
#disable-next-line BCP053
output workspaceId string = project.properties.internalId
