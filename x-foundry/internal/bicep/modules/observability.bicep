// Log Analytics workspace and, optionally, a workspace-based Application Insights component.
// Private ingestion needs an Azure Monitor Private Link Scope, which x-foundry does not create
// (see docs/phase-2.md); ingestion stays public so telemetry is not lost.
param workspaceName string
param appInsightsName string = ''
param location string
param tags object = {}
param retentionDays int = 90

resource workspace 'Microsoft.OperationalInsights/workspaces@2023-09-01' = {
  name: workspaceName
  location: location
  tags: tags
  properties: {
    sku: {
      name: 'PerGB2018'
    }
    retentionInDays: retentionDays
  }
}

resource appInsights 'Microsoft.Insights/components@2020-02-02' = if (!empty(appInsightsName)) {
  name: empty(appInsightsName) ? 'unused' : appInsightsName
  location: location
  kind: 'web'
  tags: tags
  properties: {
    Application_Type: 'web'
    WorkspaceResourceId: workspace.id
    publicNetworkAccessForIngestion: 'Enabled'
    publicNetworkAccessForQuery: 'Enabled'
  }
}

output workspaceId string = workspace.id
output workspaceName string = workspace.name
output appInsightsId string = empty(appInsightsName) ? '' : appInsights.id
output appInsightsName string = empty(appInsightsName) ? '' : appInsights.name
