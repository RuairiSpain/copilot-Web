// Reuses the parameter name `name` that modules/search.bicep also uses.
param name string
param accountName string
param searchName string

resource acct 'Microsoft.CognitiveServices/accounts@2025-06-01' existing = {
  name: accountName
}

resource conn 'Microsoft.CognitiveServices/accounts/connections@2025-06-01' = {
  parent: acct
  name: name
  properties: {
    category: 'CognitiveSearch'
    target: 'https://${searchName}.search.windows.net'
    authType: 'AAD'
    metadata: { ResourceId: resourceId('Microsoft.Search/searchServices', searchName) }
  }
}
