// Compiled with Bicep CLI 0.48.1: bicep build main.bicep --outfile main.json
// AVM-style layout: a search module whose output ID feeds a connection module.
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

module s1 'modules/search.bicep' = {
  name: 's1'
  params: { name: 'srch-${uniq}', location: location }
}

module conn 'modules/connection.bicep' = {
  name: 'conn'
  dependsOn: [ ai ]
  params: { accountName: 'ai-${uniq}', searchResourceId: s1.outputs.id, connName: 'searchconn' }
}
