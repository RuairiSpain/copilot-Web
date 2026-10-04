// Project capability host: binds the project to the bring-your-own thread store (Cosmos DB),
// file store (Storage) and vector store (AI Search). The account-level capability host is
// created by the platform when the account is created with agent network injection.
param accountName string
param projectName string
param name string = 'caphost'
param threadStorageConnection string
param storageConnection string
param vectorStoreConnection string

resource account 'Microsoft.CognitiveServices/accounts@2025-06-01' existing = {
  name: accountName
}

resource project 'Microsoft.CognitiveServices/accounts/projects@2025-06-01' existing = {
  parent: account
  name: projectName
}

resource capabilityHost 'Microsoft.CognitiveServices/accounts/projects/capabilityHosts@2025-06-01' = {
  parent: project
  name: name
  properties: {
    // The Bicep type definitions lag the API; the API requires this property.
    #disable-next-line BCP037
    capabilityHostKind: 'Agents'
    threadStorageConnections: [
      threadStorageConnection
    ]
    storageConnections: [
      storageConnection
    ]
    vectorStoreConnections: [
      vectorStoreConnection
    ]
  }
}

output name string = capabilityHost.name
