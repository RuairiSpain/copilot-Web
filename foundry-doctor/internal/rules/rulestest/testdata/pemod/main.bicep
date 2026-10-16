// Compiled with Bicep CLI 0.48.1: bicep build main.bicep --outfile main.json
// AVM-style layout: the private endpoint lives in a module and receives the
// target resource ID and the region as parameters.
targetScope = 'resourceGroup'

param location string = resourceGroup().location
var uniq = uniqueString(resourceGroup().id)

resource vnet 'Microsoft.Network/virtualNetworks@2024-05-01' = {
  name: 'vnet-${uniq}'
  location: location
  properties: {
    addressSpace: { addressPrefixes: [ '10.0.0.0/16' ] }
    subnets: [ { name: 'pe', properties: { addressPrefix: '10.0.1.0/24' } } ]
  }
}

resource ai 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'ai-${uniq}'
  location: location
  kind: 'AIServices'
  identity: { type: 'SystemAssigned' }
  sku: { name: 'S0' }
  properties: { customSubDomainName: 'ai-${uniq}', publicNetworkAccess: 'Disabled' }
}

resource proj 'Microsoft.CognitiveServices/accounts/projects@2025-06-01' = {
  parent: ai
  name: 'proj'
  location: location
  identity: { type: 'SystemAssigned' }
  properties: {}
}

module pe 'modules/pe.bicep' = {
  name: 'pe'
  params: {
    name: 'pe-ai'
    location: location
    privateLinkServiceId: ai.id
    subnetId: vnet.properties.subnets[0].id
    groupId: 'account'
  }
}
