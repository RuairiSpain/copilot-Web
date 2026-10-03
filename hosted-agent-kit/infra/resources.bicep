// Resource-group scoped resources for the pooling service.
targetScope = 'resourceGroup'

param location string
param tags object
@minLength(5)
param resourceToken string
param foundryProjectEndpoint string
param entraTenantId string
param entraAudience string
@secure()
param sessionIdKey string
@secure()
param foundryIsolationKey string
param image string

var acrPullRoleId = '7f951dda-4ed3-4680-a7ca-43fe172d538d'
var hasImage = !empty(image)
// Placeholder image for the first provision. It listens on port 80.
var containerImage = hasImage ? image : 'mcr.microsoft.com/azuredocs/containerapps-helloworld:latest'

resource workspace 'Microsoft.OperationalInsights/workspaces@2023-09-01' = {
  name: 'log-${resourceToken}'
  location: location
  tags: tags
  properties: {
    sku: { name: 'PerGB2018' }
    retentionInDays: 30
  }
}

resource insights 'Microsoft.Insights/components@2020-02-02' = {
  name: 'appi-${resourceToken}'
  location: location
  tags: tags
  kind: 'web'
  properties: {
    Application_Type: 'web'
    WorkspaceResourceId: workspace.id
  }
}

resource identity 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  name: 'id-${resourceToken}'
  location: location
  tags: tags
}

resource registry 'Microsoft.ContainerRegistry/registries@2023-07-01' = {
  name: 'cr${resourceToken}'
  location: location
  tags: tags
  sku: { name: 'Basic' }
  properties: {
    adminUserEnabled: false
  }
}

resource acrPull 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(registry.id, identity.id, acrPullRoleId)
  scope: registry
  properties: {
    principalId: identity.properties.principalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', acrPullRoleId)
  }
}

resource environment 'Microsoft.App/managedEnvironments@2024-03-01' = {
  name: 'cae-${resourceToken}'
  location: location
  tags: tags
  properties: {
    appLogsConfiguration: {
      destination: 'log-analytics'
      logAnalyticsConfiguration: {
        customerId: workspace.properties.customerId
        sharedKey: workspace.listKeys().primarySharedKey
      }
    }
  }
}

resource app 'Microsoft.App/containerApps@2024-03-01' = {
  name: 'ca-api-${resourceToken}'
  location: location
  tags: union(tags, { 'azd-service-name': 'api' })
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: { '${identity.id}': {} }
  }
  dependsOn: [acrPull]
  properties: {
    managedEnvironmentId: environment.id
    configuration: {
      activeRevisionsMode: 'Single'
      ingress: {
        external: true
        targetPort: hasImage ? 8080 : 80
        transport: 'auto'
        allowInsecure: false
      }
      registries: [
        {
          server: registry.properties.loginServer
          identity: identity.id
        }
      ]
      secrets: concat(
        [
          {
            name: 'appinsights-connection-string'
            value: insights.properties.ConnectionString
          }
        ],
        empty(sessionIdKey)
          ? []
          : [
              {
                name: 'session-id-key'
                value: sessionIdKey
              }
            ],
        empty(foundryIsolationKey)
          ? []
          : [
              {
                name: 'foundry-isolation-key'
                value: foundryIsolationKey
              }
            ]
      )
    }
    template: {
      containers: [
        {
          name: 'api'
          image: containerImage
          resources: {
            cpu: json('0.5')
            memory: '1Gi'
          }
          env: concat(
            [
              { name: 'FOUNDRY_PROJECT_ENDPOINT', value: foundryProjectEndpoint }
              { name: 'AZURE_CLIENT_ID', value: identity.properties.clientId }
              { name: 'POOL_AUTH_MODE', value: 'entra' }
              { name: 'POOL_ENTRA_TENANT_ID', value: entraTenantId }
              { name: 'POOL_ENTRA_AUDIENCE', value: entraAudience }
              { name: 'POOL_LOG_LEVEL', value: 'INFO' }
              { name: 'APPLICATIONINSIGHTS_CONNECTION_STRING', secretRef: 'appinsights-connection-string' }
            ],
            empty(sessionIdKey) ? [] : [{ name: 'POOL_SESSION_ID_KEY', secretRef: 'session-id-key' }],
            empty(foundryIsolationKey) ? [] : [{ name: 'POOL_FOUNDRY_ISOLATION_KEY', secretRef: 'foundry-isolation-key' }]
          )
          probes: hasImage
            ? [
                {
                  type: 'Startup'
                  httpGet: { path: '/health/live', port: 8080 }
                  initialDelaySeconds: 3
                  periodSeconds: 5
                  failureThreshold: 24
                }
                {
                  type: 'Liveness'
                  httpGet: { path: '/health/live', port: 8080 }
                  periodSeconds: 30
                  failureThreshold: 3
                }
                {
                  type: 'Readiness'
                  httpGet: { path: '/health/ready', port: 8080 }
                  periodSeconds: 10
                  failureThreshold: 3
                }
              ]
            : []
        }
      ]
      // Exactly one replica. Session leases, affinity and queues are held in process memory,
      // so a second replica would hand out the same session twice. Do not raise these values.
      scale: {
        minReplicas: 1
        maxReplicas: 1
      }
    }
  }
}

output identityPrincipalId string = identity.properties.principalId
output registryLoginServer string = registry.properties.loginServer
output appUrl string = 'https://${app.properties.configuration.ingress.fqdn}'
