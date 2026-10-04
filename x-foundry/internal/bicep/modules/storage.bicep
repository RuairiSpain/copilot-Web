// Storage account with blob containers. Shared-key access is off unless requested.
param name string
param location string
param tags object = {}
param sku string = 'Standard_ZRS'
param hierarchicalNamespace bool = false
param publicNetworkAccess bool = false
@description('Public IP ranges allowed when public access is on. Empty means unrestricted.')
param ipRules array = []
param allowSharedKeyAccess bool = false
param retentionDays int = 30
param containers array = []
param deleteLock bool = false
@description('Log Analytics workspace resource ID. Empty disables diagnostic settings.')
param workspaceId string = ''

resource storage 'Microsoft.Storage/storageAccounts@2023-05-01' = {
  name: name
  location: location
  tags: tags
  kind: 'StorageV2'
  sku: {
    name: sku
  }
  properties: {
    isHnsEnabled: hierarchicalNamespace
    minimumTlsVersion: 'TLS1_2'
    supportsHttpsTrafficOnly: true
    allowBlobPublicAccess: false
    allowSharedKeyAccess: allowSharedKeyAccess
    publicNetworkAccess: publicNetworkAccess ? 'Enabled' : 'Disabled'
    networkAcls: {
      bypass: 'AzureServices'
      defaultAction: publicNetworkAccess && empty(ipRules) ? 'Allow' : 'Deny'
      ipRules: [for ip in ipRules: {
        value: ip
        action: 'Allow'
      }]
    }
  }
}

resource blobService 'Microsoft.Storage/storageAccounts/blobServices@2023-05-01' = {
  parent: storage
  name: 'default'
  properties: {
    deleteRetentionPolicy: {
      enabled: retentionDays > 0
      days: retentionDays > 0 ? retentionDays : null
    }
  }
}

resource blobContainers 'Microsoft.Storage/storageAccounts/blobServices/containers@2023-05-01' = [for container in containers: {
  parent: blobService
  name: container
  properties: {
    publicAccess: 'None'
  }
}]

resource accountMetrics 'Microsoft.Insights/diagnosticSettings@2021-05-01-preview' = if (!empty(workspaceId)) {
  scope: storage
  name: 'to-log-analytics'
  properties: {
    workspaceId: workspaceId
    metrics: [
      {
        category: 'Transaction'
        enabled: true
      }
    ]
  }
}

resource blobLogs 'Microsoft.Insights/diagnosticSettings@2021-05-01-preview' = if (!empty(workspaceId)) {
  scope: blobService
  name: 'to-log-analytics'
  properties: {
    workspaceId: workspaceId
    logs: [
      {
        categoryGroup: 'allLogs'
        enabled: true
      }
    ]
  }
}

resource lock 'Microsoft.Authorization/locks@2020-05-01' = if (deleteLock) {
  scope: storage
  name: 'delete-lock'
  properties: {
    level: 'CanNotDelete'
    notes: 'Created by x-foundry (governance.resourceLocks).'
  }
}

output id string = storage.id
output name string = storage.name
output blobEndpoint string = storage.properties.primaryEndpoints.blob
output location string = storage.location
