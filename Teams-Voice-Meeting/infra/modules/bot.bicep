// Azure Bot with the Teams calling channel. Shared by both modes; only the calling webhook differs.
param name string
param botAppId string
@description('Invite mode: https://eventgrid.azure.net. Calendar mode: your public https://<host>/api/calling URL.')
param callingWebhook string

resource bot 'Microsoft.BotService/botServices@2022-09-15' = {
  name: name
  location: 'global'
  kind: 'azurebot'
  sku: { name: 'S1' }
  properties: {
    displayName: name
    endpoint: callingWebhook
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
      callingWebhook: callingWebhook
    }
  }
}

output name string = bot.name
