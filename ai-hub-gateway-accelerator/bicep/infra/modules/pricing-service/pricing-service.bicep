/**
 * @module modules/pricing-service
 * @description Provisions the pricing-service Function App (Node 20,
 *              Linux Consumption) that backs per-record cost attribution:
 *              refreshPricingCache (daily timer) and enrichPricing (HTTP,
 *              called once per llm-usage-ingestion run). See
 *              src/pricing-service/README.md and
 *              guides/cost-attribution-guide.md for the full design.
 *
 *              Deployable standalone; wiring its outputs into your main
 *              orchestration (supporting-services.bicep or equivalent)
 *              alongside the existing Cosmos DB and Logic App modules is
 *              left to you — see the "Not attempted here" note in
 *              src/pricing-service/README.md for why.
 */

@description('Name for the Function App (must be globally unique).')
param functionAppName string

param location string = resourceGroup().location

param tags object = {}

@description('Existing storage account name used for the Functions runtime (AzureWebJobsStorage) and, unless overridden, the pricing-cache blob container.')
param storageAccountName string

@description('Existing Cosmos DB account NAME (not just the endpoint — needed to grant this Function App'
  + ' data-plane RBAC via the existing modules/cosmos-db/cosmos-sql-role-assignment.bicep module).')
param cosmosDbAccountName string

@description('Existing Cosmos DB account endpoint (from the cosmos-db module output cosmosDbEndpoint).')
param cosmosDbEndpoint string

@description('Existing Cosmos DB database name (from the cosmos-db module output cosmosDbDatabaseName).')
param cosmosDbDatabaseName string

@description('Existing Cosmos DB pricing container name (from the cosmos-db module output cosmosDbPricingContainerName).')
param cosmosDbPricingContainerName string

@description('NCRONTAB schedule for the daily price refresh. Default: 03:00 UTC every day.')
param pricingRefreshSchedule string = '0 0 3 * * *'

@description('Blob container name the price page reads from.')
param pricingCacheContainerName string = 'pricing-cache'

@description('Blob name the price page reads from.')
param pricingCacheBlobName string = 'current-pricing.json'

@description('Application Insights connection string, if you want the Function App emitting to the same workspace as the rest of the gateway.')
param applicationInsightsConnectionString string = ''

@description('Resource ID of an existing subnet (delegated to Microsoft.Web/serverFarms — see guides/network-approach.md\'s "Logic App Subnet") to VNet-integrate this Function App into. Empty (the default) deploys with no VNet integration, unchanged from before this param existed. Required for either of the accelerator\'s private-networking topologies — without it, this is the only compute in the platform still reachable/reaching out over the public network. See guides/enterprise-hardening-checklist.md §2.')
param functionAppSubnetId string = ''

var vnetIntegrated = !empty(functionAppSubnetId)

resource storageAccount 'Microsoft.Storage/storageAccounts@2023-01-01' existing = {
  name: storageAccountName
}

resource hostingPlan 'Microsoft.Web/serverfarms@2023-01-01' = {
  name: '${functionAppName}-plan'
  location: location
  tags: tags
  // EP1/ElasticPremium by default — Y1/Consumption does not reliably
  // support regional VNet integration on Linux, and this is already
  // what this module's own prior comment recommended. Same precedent as
  // bicep/infra/modules/functionapp/functionapp.bicep (an existing,
  // working VNet-integrated Function App in this repo).
  sku: {
    name: 'EP1'
    tier: 'ElasticPremium'
    family: 'EP'
  }
  properties: {
    reserved: true // Linux
    maximumElasticWorkerCount: 10
  }
}

resource functionApp 'Microsoft.Web/sites@2023-01-01' = {
  name: functionAppName
  location: location
  tags: tags
  kind: 'functionapp,linux'
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    serverFarmId: hostingPlan.id
    httpsOnly: true
    virtualNetworkSubnetId: vnetIntegrated ? functionAppSubnetId : null
    siteConfig: {
      linuxFxVersion: 'Node|20'
      vnetRouteAllEnabled: vnetIntegrated
      functionsRuntimeScaleMonitoringEnabled: vnetIntegrated
      minimumElasticInstanceCount: vnetIntegrated ? 1 : 0
      appSettings: concat([
        { name: 'AzureWebJobsStorage__accountName', value: storageAccount.name }
        { name: 'FUNCTIONS_EXTENSION_VERSION', value: '~4' }
        { name: 'FUNCTIONS_WORKER_RUNTIME', value: 'node' }
        { name: 'WEBSITE_NODE_DEFAULT_VERSION', value: '~20' }
        { name: 'CosmosDB_Endpoint', value: cosmosDbEndpoint }
        { name: 'CosmosDB_Database', value: cosmosDbDatabaseName }
        { name: 'CosmosDB_PricingContainer', value: cosmosDbPricingContainerName }
        { name: 'PricingCache_StorageAccountUrl', value: 'https://${storageAccount.name}.blob.${environment().suffixes.storage}' }
        { name: 'PricingCache_ContainerName', value: pricingCacheContainerName }
        { name: 'PricingCache_BlobName', value: pricingCacheBlobName }
        { name: 'PricingRefresh_Schedule', value: pricingRefreshSchedule }
        {
          name: 'APPLICATIONINSIGHTS_CONNECTION_STRING'
          value: applicationInsightsConnectionString
        }
      ], vnetIntegrated ? [
        { name: 'WEBSITE_VNET_ROUTE_ALL', value: '1' }
        { name: 'WEBSITE_CONTENTOVERVNET', value: '1' }
      ] : [])
    }
  }
}

// Swift-connects the Function App into the subnet — the same
// Microsoft.Web/sites/networkConfig sub-resource
// bicep/infra/modules/functionapp/functionapp.bicep already uses for an
// existing, working VNet-integrated Function App in this repo.
// virtualNetworkSubnetId above alone is not sufficient on its own for
// regional VNet Integration; this sub-resource is what actually performs
// it.
resource networkConfig 'Microsoft.Web/sites/networkConfig@2023-01-01' = if (vnetIntegrated) {
  parent: functionApp
  name: 'virtualNetwork'
  properties: {
    subnetResourceId: functionAppSubnetId
    swiftSupported: true
  }
}

// Data-plane RBAC via the accelerator's own existing reusable module
// (bicep/infra/modules/cosmos-db/cosmos-sql-role-assignment.bicep) — same
// "Cosmos DB Built-in Data Contributor" role and same module
// quota-service.bicep uses for its own two containers.
//
// CORRECTED (this session's own review found the grant this comment used
// to only describe was never actually created): every Cosmos call in
// enrichPricing.ts/refreshPricingCache.ts (loadAllSnapshots, upsertSnapshot,
// upsertCurrentPointer) authenticates via DefaultAzureCredential() against
// this Function App's managed identity — without this module call, that
// identity held zero Cosmos data-plane RBAC and every one of those calls
// would fail with an authorization error on first real use.
//
// Same account-wide-not-container-scoped caveat as quota-service.bicep's
// identical grant — see that module's own comment and
// guides/enterprise-hardening-checklist.md for the reasoning; not
// re-duplicated here.
//
// Also per that module's own comment: deploy this in a separate stage
// from functionApp's own creation so the new managed identity has time
// to replicate in Entra ID first (Cosmos validates the principal
// synchronously and does not accept a principalType hint) — same
// ordering constraint as every other identity this accelerator grants
// Cosmos access to.
module cosmosRoleAssignment '../cosmos-db/cosmos-sql-role-assignment.bicep' = {
  name: '${functionAppName}-cosmos-rbac'
  params: {
    cosmosDbAccountName: cosmosDbAccountName
    principalId: functionApp.identity.principalId
  }
}

// Grant below uses the built-in Storage Blob Data Contributor role, for
// the pricing-cache blob container (the customer-facing price page's
// cached read path) — a separate concern from the Cosmos grant above.
resource storageBlobDataContributorRole 'Microsoft.Authorization/roleDefinitions@2022-04-01' existing = {
  scope: subscription()
  name: 'ba92f5b4-2d11-453d-a403-e96b0029c9fe' // Storage Blob Data Contributor
}

resource storageRoleAssignment 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(storageAccount.id, functionApp.id, storageBlobDataContributorRole.id)
  scope: storageAccount
  properties: {
    roleDefinitionId: storageBlobDataContributorRole.id
    principalId: functionApp.identity.principalId
    principalType: 'ServicePrincipal'
  }
}

output functionAppName string = functionApp.name
output functionAppPrincipalId string = functionApp.identity.principalId
output functionAppDefaultHostName string = functionApp.properties.defaultHostName
