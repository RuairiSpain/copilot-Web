// AVM-style connection: the search service arrives as a resource ID, which the
// caller takes from the search module's output.
param accountName string
param searchResourceId string
param connName string

resource acct 'Microsoft.CognitiveServices/accounts@2025-06-01' existing = {
  name: accountName
}

resource conn 'Microsoft.CognitiveServices/accounts/connections@2025-06-01' = {
  parent: acct
  name: connName
  properties: {
    category: 'CognitiveSearch'
    target: 'https://example.search.windows.net'
    authType: 'AAD'
    metadata: { ResourceId: searchResourceId }
  }
}
