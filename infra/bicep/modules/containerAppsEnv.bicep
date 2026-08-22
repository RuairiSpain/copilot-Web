@description('Location for the environment.')
param location string

@description('Name of the Container Apps managed environment.')
param name string

@description('Log Analytics customer ID (from the logAnalytics module).')
param logAnalyticsCustomerId string

@description('Log Analytics shared key.')
@secure()
param logAnalyticsSharedKey string

resource env 'Microsoft.App/managedEnvironments@2024-03-01' = {
  name: name
  location: location
  properties: {
    appLogsConfiguration: {
      destination: 'log-analytics'
      logAnalyticsConfiguration: {
        customerId: logAnalyticsCustomerId
        sharedKey: logAnalyticsSharedKey
      }
    }
    // Workload profiles default to "Consumption" when none are declared,
    // which is what keeps this on pay-per-use pricing rather than a
    // Dedicated plan's fixed node cost.
  }
}

output environmentId string = env.id
output defaultDomain string = env.properties.defaultDomain
