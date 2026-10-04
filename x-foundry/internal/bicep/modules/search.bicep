// Azure AI Search service.
param name string
param location string
param tags object = {}
@allowed([
  'free'
  'basic'
  'standard'
  'standard2'
  'standard3'
  'storage_optimized_l1'
  'storage_optimized_l2'
])
param sku string = 'standard'
param replicas int = 1
param partitions int = 1
param semanticRanking bool = true
param localAuthentication bool = false
param publicNetworkAccess bool = false
@description('Public IP ranges allowed when public access is on. Empty means unrestricted.')
param ipRules array = []
param systemIdentity bool = true
param deleteLock bool = false
@description('Log Analytics workspace resource ID. Empty disables diagnostic settings.')
param workspaceId string = ''

resource search 'Microsoft.Search/searchServices@2024-06-01-preview' = {
  name: name
  location: location
  tags: tags
  sku: {
    name: sku
  }
  identity: {
    type: systemIdentity ? 'SystemAssigned' : 'None'
  }
  properties: {
    hostingMode: 'default'
    replicaCount: replicas
    partitionCount: partitions
    semanticSearch: sku == 'free' ? 'free' : (semanticRanking ? 'standard' : 'disabled')
    disableLocalAuth: !localAuthentication
    authOptions: localAuthentication ? {
      aadOrApiKey: {
        aadAuthFailureMode: 'http401WithBearerChallenge'
      }
    } : null
    publicNetworkAccess: publicNetworkAccess ? 'enabled' : 'disabled'
    networkRuleSet: {
      bypass: 'None'
      ipRules: [for ip in ipRules: {
        value: ip
      }]
    }
  }
}

resource diagnostics 'Microsoft.Insights/diagnosticSettings@2021-05-01-preview' = if (!empty(workspaceId)) {
  scope: search
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
  scope: search
  name: 'delete-lock'
  properties: {
    level: 'CanNotDelete'
    notes: 'Created by x-foundry (governance.resourceLocks).'
  }
}

output id string = search.id
output name string = search.name
output endpoint string = 'https://${search.name}.search.windows.net'
output location string = search.location
output principalId string = systemIdentity ? search.identity.principalId : ''
