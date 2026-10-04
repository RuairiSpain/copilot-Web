// Key Vault with Azure RBAC authorisation.
param name string
param location string
param tags object = {}
param sku string = 'standard'
param softDeleteDays int = 90
param purgeProtection bool = true
param publicNetworkAccess bool = false
@description('Public IP ranges allowed when public access is on. Empty means unrestricted.')
param ipRules array = []
param deleteLock bool = false
@description('Log Analytics workspace resource ID. Empty disables diagnostic settings.')
param workspaceId string = ''

resource vault 'Microsoft.KeyVault/vaults@2024-11-01' = {
  name: name
  location: location
  tags: tags
  properties: {
    tenantId: tenant().tenantId
    sku: {
      family: 'A'
      name: sku
    }
    enableRbacAuthorization: true
    enableSoftDelete: true
    softDeleteRetentionInDays: softDeleteDays
    // Purge protection cannot be switched off once on, so it is only ever sent as true.
    enablePurgeProtection: purgeProtection ? true : null
    publicNetworkAccess: publicNetworkAccess ? 'Enabled' : 'Disabled'
    networkAcls: {
      bypass: 'AzureServices'
      defaultAction: publicNetworkAccess && empty(ipRules) ? 'Allow' : 'Deny'
      ipRules: [for ip in ipRules: {
        value: ip
      }]
    }
  }
}

resource diagnostics 'Microsoft.Insights/diagnosticSettings@2021-05-01-preview' = if (!empty(workspaceId)) {
  scope: vault
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
  scope: vault
  name: 'delete-lock'
  properties: {
    level: 'CanNotDelete'
    notes: 'Created by x-foundry (governance.resourceLocks).'
  }
}

output id string = vault.id
output name string = vault.name
output uri string = vault.properties.vaultUri
