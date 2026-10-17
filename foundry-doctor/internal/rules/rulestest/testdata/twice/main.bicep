// Compiled with Bicep CLI 0.48.1: bicep build main.bicep --outfile main.json
// One module deployed twice with different names, and a connection module
// that reuses the parameter name `name`.
targetScope = 'resourceGroup'

param location string = resourceGroup().location
var uniq = uniqueString(resourceGroup().id)

resource ai 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'ai-${uniq}'
  location: location
  kind: 'AIServices'
  identity: { type: 'SystemAssigned' }
  sku: { name: 'S0' }
  properties: { customSubDomainName: 'ai-${uniq}', disableLocalAuth: false }
}

module searchF 'modules/search.bicep' = {
  name: 'search-f'
  params: { name: 'srch-f-${uniq}', location: location }
}

module searchX 'modules/search.bicep' = {
  name: 'search-x'
  params: { name: 'srch-x-${uniq}', location: location }
}

module conn 'modules/connection.bicep' = {
  name: 'conn'
  dependsOn: [ ai ]
  params: { name: 'searchconn', accountName: 'ai-${uniq}', searchName: 'srch-f-${uniq}' }
}
