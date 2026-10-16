// Shape of Microsoft sample 15 (private network standard agent setup):
// networkInjections is a ternary over a parameter, so the compiled template
// holds an ARM expression string instead of an array. Compiled with
//   bicep build netinj.bicep --outfile netinj.json   (Bicep CLI 0.48.1)
targetScope = 'resourceGroup'

param location string = resourceGroup().location
@allowed([ 'true', 'false' ])
param networkInjection string = 'true'

resource vnet 'Microsoft.Network/virtualNetworks@2024-05-01' = {
  name: 'agent-vnet'
  location: location
  properties: {
    addressSpace: { addressPrefixes: [ '192.168.0.0/16' ] }
    subnets: [
      {
        name: 'agent-subnet'
        properties: {
          addressPrefix: '192.168.0.0/28'
          delegations: [ { name: 'd', properties: { serviceName: 'Microsoft.App/environments' } } ]
        }
      }
    ]
  }
}

resource account 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'aiservices'
  location: location
  kind: 'AIServices'
  identity: { type: 'SystemAssigned' }
  sku: { name: 'S0' }
  properties: {
    customSubDomainName: 'aiservices'
    publicNetworkAccess: 'Disabled'
    allowProjectManagement: true
    networkInjections: (networkInjection == 'true') ? [
      {
        scenario: 'agent'
        subnetArmId: vnet.properties.subnets[0].id
        useMicrosoftManagedNetwork: false
      }
    ] : null
  }
}
