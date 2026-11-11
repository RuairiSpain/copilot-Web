// Calendar mode: only the bot registration. No Foundry, ACS, VNet or private endpoint is needed,
// because the agent reaches Teams through Microsoft Graph, not through a phone number.
targetScope = 'resourceGroup'

@minLength(3)
@maxLength(12)
param prefix string
param botAppId string
@description('Public HTTPS URL that Graph posts call notifications to, for example https://bot.contoso.com/api/calling')
param callbackUrl string

module bot 'modules/bot.bicep' = {
  name: 'bot'
  params: {
    name: '${prefix}-bot-${take(uniqueString(resourceGroup().id), 5)}'
    botAppId: botAppId
    callingWebhook: callbackUrl
  }
}

output botName string = bot.outputs.name
