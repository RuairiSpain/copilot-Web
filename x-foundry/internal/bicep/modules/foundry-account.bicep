// Microsoft Foundry resource (Cognitive Services account of kind AIServices) with project
// management enabled. Foundry accounts always use a system-assigned identity.
param name string
param location string
param tags object = {}
param publicNetworkAccess bool = false
@description('Public IP ranges allowed when public access is on. Empty means unrestricted.')
param ipRules array = []
param localAuthentication bool = false
@description('Resource ID of the delegated agent subnet. Empty unless the standard agent setup runs in private mode.')
param agentSubnetId string = ''
param deleteLock bool = false
@description('Log Analytics workspace resource ID. Empty disables diagnostic settings.')
param workspaceId string = ''

resource account 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: name
  location: location
  tags: tags
  kind: 'AIServices'
  sku: {
    name: 'S0'
  }
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    allowProjectManagement: true
    customSubDomainName: name
    disableLocalAuth: !localAuthentication
    publicNetworkAccess: publicNetworkAccess ? 'Enabled' : 'Disabled'
    networkAcls: {
      defaultAction: publicNetworkAccess && empty(ipRules) ? 'Allow' : 'Deny'
      bypass: 'AzureServices'
      ipRules: [for ip in ipRules: {
        value: ip
      }]
      virtualNetworkRules: []
    }
    networkInjections: empty(agentSubnetId) ? null : [
      {
        scenario: 'agent'
        subnetArmId: agentSubnetId
        useMicrosoftManagedNetwork: false
      }
    ]
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
output endpoint string = account.properties.endpoint
output principalId string = account.identity.principalId
