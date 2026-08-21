@description('Location for the workspace.')
param location string

@description('Name of the Log Analytics workspace.')
param name string

@description('Days to retain logs. Keep this short — retention drives cost, and this is a low-traffic personal app.')
param retentionInDays int = 30

resource workspace 'Microsoft.OperationalInsights/workspaces@2023-09-01' = {
  name: name
  location: location
  properties: {
    sku: {
      name: 'PerGB2018'
    }
    retentionInDays: retentionInDays
  }
}

output workspaceId string = workspace.id
output customerId string = workspace.properties.customerId
@secure()
output sharedKey string = workspace.listKeys().primarySharedKey
