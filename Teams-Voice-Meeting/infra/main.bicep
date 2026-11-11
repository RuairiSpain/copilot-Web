// Teams voice agent MVP. Resource-group scope. Deployed by deploy.sh.
//
// Public by necessity: Azure Communication Services (Teams/PSTN signalling), the Azure Bot Service
// Teams channel, and (until you verify a call with it off) the Foundry endpoint, because Event Grid
// delivers IncomingCall events to a Foundry URL. Everything else is private.

targetScope = 'resourceGroup'

@description('Short name prefix, 3-12 lowercase letters or digits.')
@minLength(3)
@maxLength(12)
param prefix string

param location string = resourceGroup().location

@description('ACS data location. Must match the Teams tenant data location.')
param acsDataLocation string = 'Europe'

@description('Application (client) ID of the single-tenant Entra app used by the bot and the Teams resource account.')
param botAppId string

@description('Object ID of the person or principal that runs the CLI. Gets the Azure AI User data role on Foundry.')
param operatorPrincipalId string

@description('Set Disabled once a test call works. Event Grid reaches Foundry over the public endpoint, so Disabled may block incoming calls: verify before relying on it.')
@allowed(['Enabled', 'Disabled'])
param foundryPublicNetworkAccess string = 'Enabled'

param vnetAddressPrefix string = '10.40.0.0/24'
param peSubnetPrefix string = '10.40.0.0/26'

var suffix = uniqueString(resourceGroup().id)
var foundryName = '${prefix}-foundry-${take(suffix, 5)}'
var acsName = '${prefix}-acs-${take(suffix, 5)}'
var botName = '${prefix}-bot-${take(suffix, 5)}'
var projectName = 'teams-voice'
var connectionName = 'acs'
var azureAiUserRole = '53ca6127-db72-4b80-b1b0-d745d6d5456d'

// ---------- network ----------
resource nsg 'Microsoft.Network/networkSecurityGroups@2024-05-01' = {
  name: '${prefix}-pe-nsg'
  location: location
  properties: {}
}

resource vnet 'Microsoft.Network/virtualNetworks@2024-05-01' = {
  name: '${prefix}-vnet'
  location: location
  properties: {
    addressSpace: { addressPrefixes: [vnetAddressPrefix] }
    subnets: [
      {
        name: 'private-endpoints'
        properties: {
          addressPrefix: peSubnetPrefix
          networkSecurityGroup: { id: nsg.id }
        }
      }
    ]
  }
}

var dnsZones = [
  'privatelink.cognitiveservices.azure.com'
  'privatelink.openai.azure.com'
  'privatelink.services.ai.azure.com'
]

resource zones 'Microsoft.Network/privateDnsZones@2024-06-01' = [for z in dnsZones: {
  name: z
  location: 'global'
}]

resource zoneLinks 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2024-06-01' = [for (z, i) in dnsZones: {
  parent: zones[i]
  name: '${prefix}-link'
  location: 'global'
  properties: {
    registrationEnabled: false
    virtualNetwork: { id: vnet.id }
  }
}]

// ---------- Foundry ----------
resource foundry 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: foundryName
  location: location
  kind: 'AIServices'
  sku: { name: 'S0' }
  identity: { type: 'SystemAssigned' }
  properties: {
    customSubDomainName: foundryName
    allowProjectManagement: true
    disableLocalAuth: true
    publicNetworkAccess: foundryPublicNetworkAccess
    networkAcls: { defaultAction: foundryPublicNetworkAccess == 'Enabled' ? 'Allow' : 'Deny' }
  }
}

resource project 'Microsoft.CognitiveServices/accounts/projects@2025-06-01' = {
  parent: foundry
  name: projectName
  location: location
  identity: { type: 'SystemAssigned' }
  properties: {
    displayName: 'Teams voice'
    description: 'Voice agent answering Teams Phone calls'
  }
}

resource foundryPe 'Microsoft.Network/privateEndpoints@2024-05-01' = {
  name: '${foundryName}-pe'
  location: location
  properties: {
    subnet: { id: vnet.properties.subnets[0].id }
    privateLinkServiceConnections: [
      {
        name: 'foundry'
        properties: { privateLinkServiceId: foundry.id, groupIds: ['account'] }
      }
    ]
  }
  dependsOn: [project]
}

resource foundryPeDns 'Microsoft.Network/privateEndpoints/privateDnsZoneGroups@2024-05-01' = {
  parent: foundryPe
  name: 'default'
  properties: {
    privateDnsZoneConfigs: [for (z, i) in dnsZones: {
      name: replace(z, '.', '-')
      properties: { privateDnsZoneId: zones[i].id }
    }]
  }
}

resource operatorRole 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: foundry
  name: guid(foundry.id, operatorPrincipalId, azureAiUserRole)
  properties: {
    principalId: operatorPrincipalId
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', azureAiUserRole)
    principalType: 'User'
  }
}

// ---------- Azure Communication Services ----------
resource acs 'Microsoft.Communication/communicationServices@2023-04-01' = {
  name: acsName
  location: 'global'
  properties: { dataLocation: acsDataLocation }
}

// Project connection the voice agent binding refers to (category AzureCommunicationServices).
// The ACS access key is stored in the connection. Compare this with a connection created in the
// Foundry portal if the binding call fails (see README, "Unverified").
resource acsConnection 'Microsoft.CognitiveServices/accounts/projects/connections@2025-06-01' = {
  parent: project
  name: connectionName
  properties: {
    category: 'AzureCommunicationServices'
    target: 'https://${acs.properties.hostName}'
    authType: 'ApiKey'
    credentials: { key: acs.listKeys().primaryKey }
    metadata: { ResourceId: acs.id }
  }
}

// ---------- Azure Bot (Teams calling channel) ----------
resource bot 'Microsoft.BotService/botServices@2022-09-15' = {
  name: botName
  location: 'global'
  kind: 'azurebot'
  sku: { name: 'S1' }
  properties: {
    displayName: botName
    endpoint: 'https://eventgrid.azure.net'
    msaAppId: botAppId
    msaAppTenantId: tenant().tenantId
    msaAppType: 'SingleTenant'
  }
}

resource teamsChannel 'Microsoft.BotService/botServices/channels@2022-09-15' = {
  parent: bot
  name: 'MsTeamsChannel'
  location: 'global'
  properties: {
    channelName: 'MsTeamsChannel'
    properties: {
      isEnabled: true
      enableCalling: true
      callingWebhook: 'https://eventgrid.azure.net'
    }
  }
}

output foundryName string = foundry.name
output foundryProjectEndpoint string = 'https://${foundry.name}.services.ai.azure.com/api/projects/${project.name}'
output acsName string = acs.name
output acsId string = acs.id
output acsHost string = acs.properties.hostName
output acsImmutableId string = acs.properties.immutableResourceId
output connectionName string = connectionName
output botName string = bot.name
