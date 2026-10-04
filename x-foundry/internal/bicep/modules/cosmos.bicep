// Cosmos DB for NoSQL account that stores agent state (threads, messages, files metadata).
// Foundry creates the database and containers when the capability host is set up.
param name string
param location string
param tags object = {}
@allowed([
  'provisioned'
  'serverless'
])
param capacityMode string = 'provisioned'
param zoneRedundant bool = false
param continuousBackup bool = false
param publicNetworkAccess bool = false
@description('Public IP ranges allowed when public access is on. Empty means unrestricted.')
param ipRules array = []
param localAuthentication bool = false
param deleteLock bool = false
@description('Log Analytics workspace resource ID. Empty disables diagnostic settings.')
param workspaceId string = ''

resource account 'Microsoft.DocumentDB/databaseAccounts@2024-11-15' = {
  name: name
  location: location
  tags: tags
  kind: 'GlobalDocumentDB'
  properties: {
    databaseAccountOfferType: 'Standard'
    consistencyPolicy: {
      defaultConsistencyLevel: 'Session'
    }
    locations: [
      {
        locationName: location
        failoverPriority: 0
        isZoneRedundant: zoneRedundant
      }
    ]
    capabilities: capacityMode == 'serverless' ? [
      {
        name: 'EnableServerless'
      }
    ] : []
    backupPolicy: continuousBackup ? {
      type: 'Continuous'
      continuousModeProperties: {
        tier: 'Continuous7Days'
      }
    } : {
      type: 'Periodic'
      periodicModeProperties: {
        backupIntervalInMinutes: 240
        backupRetentionIntervalInHours: 8
        backupStorageRedundancy: 'Geo'
      }
    }
    enableAutomaticFailover: false
    enableMultipleWriteLocations: false
    enableFreeTier: false
    disableLocalAuth: !localAuthentication
    publicNetworkAccess: publicNetworkAccess ? 'Enabled' : 'Disabled'
    isVirtualNetworkFilterEnabled: false
    ipRules: [for ip in ipRules: {
      ipAddressOrRange: ip
    }]
  }
}

resource diagnostics 'Microsoft.Insights/diagnosticSettings@2021-05-01-preview' = if (!empty(workspaceId)) {
  scope: account
  name: 'to-log-analytics'
  properties: {
    workspaceId: workspaceId
    logs: [
      {
        categoryGroup: 'allLogs'
        enabled: true
      }
    ]
    metrics: [
      {
        category: 'AllMetrics'
        enabled: true
      }
    ]
  }
}

resource lock 'Microsoft.Authorization/locks@2020-05-01' = if (deleteLock) {
  scope: account
  name: 'delete-lock'
  properties: {
    level: 'CanNotDelete'
    notes: 'Created by x-foundry (governance.resourceLocks).'
  }
}

output id string = account.id
output name string = account.name
output endpoint string = account.properties.documentEndpoint
output location string = account.location
