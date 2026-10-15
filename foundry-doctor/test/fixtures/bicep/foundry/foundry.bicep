// Foundry Doctor test fixture (original, not copied). Four accounts that differ only in how
// disableLocalAuth is written, a looped model deployment, a nested project and a conditional
// private endpoint. Property names and API versions are the ones used by the Azure/azure-dev
// synthesis template (see ../synthetic).
targetScope = 'resourceGroup'

param location string = resourceGroup().location

@description('Literal default: the normaliser may use it as a fallback value.')
param disableAuthWithDefault bool = true

@description('No default: the value is only known at deployment time.')
param disableAuthNoDefault bool

param createPrivateEndpoint bool = false
param peSubnetId string = ''

param deployments array = [
  {
    name: 'gpt-fixture'
    model: { format: 'OpenAI', name: 'gpt-4o', version: '2024-11-20' }
    sku: { name: 'GlobalStandard', capacity: 10 }
  }
]

resource acctLiteral 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'fdacctliteral'
  location: location
  sku: { name: 'S0' }
  kind: 'AIServices'
  identity: { type: 'SystemAssigned' }
  properties: {
    allowProjectManagement: true
    customSubDomainName: 'fdacctliteral'
    publicNetworkAccess: 'Disabled'
    disableLocalAuth: true
  }

  @batchSize(1)
  resource modelDeployments 'deployments' = [for d in deployments: {
    name: d.name
    properties: { model: d.model }
    sku: d.sku
  }]

  resource project 'projects' = {
    name: 'fdproject'
    location: location
    identity: { type: 'SystemAssigned' }
    properties: { description: 'fixture project', displayName: 'fdproject' }
    dependsOn: [ modelDeployments ]
  }
}

resource acctDefaultParam 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'fdacctdefault'
  location: location
  sku: { name: 'S0' }
  kind: 'AIServices'
  properties: {
    customSubDomainName: 'fdacctdefault'
    disableLocalAuth: disableAuthWithDefault
  }
}

resource acctNoDefaultParam 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'fdacctnodefault'
  location: location
  sku: { name: 'S0' }
  kind: 'AIServices'
  properties: {
    customSubDomainName: 'fdacctnodefault'
    disableLocalAuth: disableAuthNoDefault
  }
}

resource acctAbsent 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'fdacctabsent'
  location: location
  sku: { name: 'S0' }
  kind: 'AIServices'
  properties: {
    customSubDomainName: 'fdacctabsent'
  }
}

resource pe 'Microsoft.Network/privateEndpoints@2024-05-01' = if (createPrivateEndpoint) {
  name: 'fdacctliteral-private-endpoint'
  location: location
  properties: {
    subnet: { id: peSubnetId }
    privateLinkServiceConnections: [
      {
        name: 'fdacctliteral-plsc'
        properties: {
          privateLinkServiceId: acctLiteral.id
          groupIds: [ 'account' ]
        }
      }
    ]
  }
}

module peDns 'modules/dns.bicep' = if (createPrivateEndpoint) {
  name: 'pe-dns'
  params: { zoneName: 'privatelink.cognitiveservices.azure.com' }
}

output accountId string = acctLiteral.id
