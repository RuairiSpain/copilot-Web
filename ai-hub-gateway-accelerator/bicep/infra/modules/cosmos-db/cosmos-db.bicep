@description('Azure Cosmos DB account name, max length 44 characters')
param accountName string

@description('Location for the Azure Cosmos DB account.')
param location string = resourceGroup().location

@description('The primary region for the Azure Cosmos DB account.')
param primaryRegion string = location

param tags object = {}

@allowed([
  'Eventual'
  'ConsistentPrefix'
  'Session'
  'BoundedStaleness'
  'Strong'
])
@description('The default consistency level of the Cosmos DB account.')
param defaultConsistencyLevel string = 'Session'

@minValue(10)
@maxValue(2147483647)
@description('Max stale requests. Required for BoundedStaleness. Valid ranges, Single Region: 10 to 2147483647. Multi Region: 100000 to 2147483647.')
param maxStalenessPrefix int = 100000

@minValue(5)
@maxValue(86400)
@description('Max lag time (minutes). Required for BoundedStaleness. Valid ranges, Single Region: 5 to 84600. Multi Region: 300 to 86400.')
param maxIntervalInSeconds int = 300

@allowed([
  true
  false
])
@description('Enable system managed failover for regions')
param systemManagedFailover bool = true

@description('The name for the database')
param databaseName string = 'ai-usage-db'

@description('The name for the container')
param containerName string = 'ai-usage-container'

@description('The name for the container')
param pricingContainerName string = 'model-pricing'

@description('The name for the container')
param piiUsageContainerName string = 'pii-usage-container'

@description('The name for the container')
param llmUsageContainerName string = 'llm-usage-container'

@description('The name for the published Tools (MCP) usage container')
param mcpUsageContainerName string = 'mcp-usage-container'

@description('The name for the published Agents (A2A) usage container')
param agentUsageContainerName string = 'agent-usage-container'

@description('The name for the container')
param streamingExportConfigContainerName string = 'streaming-export-config'

@description('The name for the quota-overrides container (current effective per-scope quota state — see guides/quota-override-approval.md §3)')
param quotaOverridesContainerName string = 'quota-overrides'

@description('The name for the quota-override-requests container (append-only request/approval audit trail — see guides/quota-override-approval.md §3)')
param quotaOverrideRequestsContainerName string = 'quota-override-requests'

@minValue(400)
@maxValue(1000000)
@description('The throughput for the container')
param throughput int = 400

var consistencyPolicy = {
  Eventual: {
    defaultConsistencyLevel: 'Eventual'
  }
  ConsistentPrefix: {
    defaultConsistencyLevel: 'ConsistentPrefix'
  }
  Session: {
    defaultConsistencyLevel: 'Session'
  }
  BoundedStaleness: {
    defaultConsistencyLevel: 'BoundedStaleness'
    maxStalenessPrefix: maxStalenessPrefix
    maxIntervalInSeconds: maxIntervalInSeconds
  }
  Strong: {
    defaultConsistencyLevel: 'Strong'
  }
}
var locations = [
  {
    locationName: primaryRegion
    failoverPriority: 0
    isZoneRedundant: false
  }
]

// Networking
param cosmosPrivateEndpointName string
param vNetName string
param privateEndpointSubnetName string
param cosmosDnsZoneName string
param publicAccess string = 'Disabled'

// Use existing network/dns zone - Legacy parameters (used when dnsZoneResourceId is not provided)
param dnsZoneRG string = ''
param dnsSubscriptionId string = ''

// New parameter: Direct DNS zone resource ID (preferred over dnsZoneRG/dnsSubscriptionId)
param dnsZoneResourceId string = ''

param vNetRG string
resource vnet 'Microsoft.Network/virtualNetworks@2022-01-01' existing = {
  name: vNetName
  scope: resourceGroup(vNetRG)
}

// Get existing subnet
resource subnet 'Microsoft.Network/virtualNetworks/subnets@2022-01-01' existing = {
  name: privateEndpointSubnetName
  parent: vnet
}

resource account 'Microsoft.DocumentDB/databaseAccounts@2024-02-15-preview' = {
  name: toLower(accountName)
  location: location
  tags: union(tags, { 'azd-service-name': accountName })
  kind: 'GlobalDocumentDB'
  properties: {
    consistencyPolicy: consistencyPolicy[defaultConsistencyLevel]
    locations: locations
    databaseAccountOfferType: 'Standard'
    enableAutomaticFailover: systemManagedFailover
    disableKeyBasedMetadataWriteAccess: true
    publicNetworkAccess: publicAccess
  }
}

resource database 'Microsoft.DocumentDB/databaseAccounts/sqlDatabases@2024-02-15-preview' = {
  parent: account
  name: databaseName
  properties: {
    resource: {
      id: databaseName
    }
  }
}

resource container 'Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers@2024-02-15-preview' = {
  parent: database
  name: containerName
  properties: {
    resource: {
      id: containerName
      partitionKey: {
        paths: [
          '/productName'
        ]
        kind: 'Hash'
      }
      indexingPolicy: {
        indexingMode: 'consistent'
        automatic: true
      }
    }
    options: {
      throughput: throughput
    }
  }
}

// Holds two document shapes (see src/pricing-service for what writes them):
//   docType: "priceSnapshot"        - append-only, dated price history
//                                      (id: "{model}-v{n}", never edited
//                                      once written)
//   docType: "currentPricePointer"  - one mutable "latest" pointer per
//                                      model (id: "current::{model}"),
//                                      refreshed daily
// The composite index below makes the point-in-time lookup
// (model + effectiveFrom range) used by enrichPricing.ts efficient instead
// of a full-partition scan; default automatic indexing already covers
// every field for point reads and the docType filter.
resource modelPricingContainer 'Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers@2024-02-15-preview' = {
  parent: database
  name: pricingContainerName
  properties: {
    resource: {
      id: pricingContainerName
      partitionKey: {
        paths: [
          '/model'
        ]
        kind: 'Hash'
      }
      indexingPolicy: {
        indexingMode: 'consistent'
        automatic: true
        compositeIndexes: [
          [
            { path: '/model', order: 'ascending' }
            { path: '/effectiveFrom', order: 'descending' }
          ]
        ]
      }
    }
    options: {
      throughput: throughput
    }
  }
}

resource streamingExportConfigContainer 'Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers@2024-02-15-preview' = {
  parent: database
  name: streamingExportConfigContainerName
  properties: {
    resource: {
      id: streamingExportConfigContainerName
      partitionKey: {
        paths: [
          '/type'
        ]
        kind: 'Hash'
      }
      indexingPolicy: {
        indexingMode: 'consistent'
        automatic: true
      }
    }
    options: {
      throughput: throughput
    }
  }
}

resource piiUsageContainer 'Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers@2024-02-15-preview' = {
  parent: database
  name: piiUsageContainerName
  properties: {
    resource: {
      id: piiUsageContainerName
      partitionKey: {
        paths: [
          '/type'
        ]
        kind: 'Hash'
      }
      indexingPolicy: {
        indexingMode: 'consistent'
        automatic: true
      }
    }
    options: {
      throughput: throughput
    }
  }
}

resource llmUsageContainer 'Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers@2024-02-15-preview' = {
  parent: database
  name: llmUsageContainerName
  properties: {
    resource: {
      id: llmUsageContainerName
      partitionKey: {
        paths: [
          '/productName'
        ]
        kind: 'Hash'
      }
      indexingPolicy: {
        indexingMode: 'consistent'
        automatic: true
      }
    }
    options: {
      throughput: throughput
    }
  }
}

// Published Tools (MCP) usage. Fed by the mcp-usage-ingestion Logic App which aggregates the
// 'mcp-usage' App Insights custom metrics (mirrors the LLM usage pipeline).
resource mcpUsageContainer 'Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers@2024-02-15-preview' = {
  parent: database
  name: mcpUsageContainerName
  properties: {
    resource: {
      id: mcpUsageContainerName
      partitionKey: {
        paths: [
          '/productName'
        ]
        kind: 'Hash'
      }
      indexingPolicy: {
        indexingMode: 'consistent'
        automatic: true
      }
    }
    options: {
      throughput: throughput
    }
  }
}

// Published Agents (A2A) usage. Fed by the agent-usage-ingestion Logic App which aggregates the
// 'a2a-usage' App Insights custom metrics.
resource agentUsageContainer 'Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers@2024-02-15-preview' = {
  parent: database
  name: agentUsageContainerName
  properties: {
    resource: {
      id: agentUsageContainerName
      partitionKey: {
        paths: [
          '/productName'
        ]
        kind: 'Hash'
      }
      indexingPolicy: {
        indexingMode: 'consistent'
        automatic: true
      }
    }
    options: {
      throughput: throughput
    }
  }
}

// Current effective quota-override state, one document per scope
// (id: "{scopeType}-{scopeId}"). Read on the synchronous APIM request
// path (via quota-service's getQuotaAllowance, on an APIM cache miss
// only) — a point read by id, so no composite index is needed beyond
// the default automatic indexing. Partitioned by /subscriptionId (the
// access contract) rather than /scopeId so a single contract's overrides
// stay in one partition, matching how quota is already scoped in the
// APIM policy layer (context.Subscription.Id).
resource quotaOverridesContainer 'Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers@2024-02-15-preview' = {
  parent: database
  name: quotaOverridesContainerName
  properties: {
    resource: {
      id: quotaOverridesContainerName
      partitionKey: {
        paths: [
          '/subscriptionId'
        ]
        kind: 'Hash'
      }
      indexingPolicy: {
        indexingMode: 'consistent'
        automatic: true
      }
    }
    options: {
      throughput: throughput
    }
  }
}

// Append-only quota-override request/approval audit trail — statusHistory
// grows via replace(), the document itself is never deleted (see
// src/quota-service/src/functions/decideQuotaRequest.ts). Same partition
// key choice as quotaOverridesContainer, for the same reason.
resource quotaOverrideRequestsContainer 'Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers@2024-02-15-preview' = {
  parent: database
  name: quotaOverrideRequestsContainerName
  properties: {
    resource: {
      id: quotaOverrideRequestsContainerName
      partitionKey: {
        paths: [
          '/subscriptionId'
        ]
        kind: 'Hash'
      }
      indexingPolicy: {
        indexingMode: 'consistent'
        automatic: true
      }
    }
    options: {
      throughput: throughput
    }
  }
}

module privateEndpoint '../networking/private-endpoint.bicep' = {
  name: '${accountName}-pe'
  params: {
    groupIds: [
      'sql'
    ]
    dnsZoneName: cosmosDnsZoneName
    name: cosmosPrivateEndpointName
    privateLinkServiceId: account.id
    location: location
    dnsZoneRG: dnsZoneRG
    privateEndpointSubnetId: subnet.id
    dnsSubId: dnsSubscriptionId
    dnsZoneResourceId: dnsZoneResourceId
    tags: tags
  }
}

output location string = location
output cosmosDbAccountName string = account.name
output cosmosDbDatabaseName string = database.name
output cosmosDbContainerName string = container.name
output cosmosDbPiiUsageContainerName string = piiUsageContainer.name
output cosmosDbLLMUsageContainerName string = llmUsageContainer.name
output cosmosDbMcpUsageContainerName string = mcpUsageContainer.name
output cosmosDbAgentUsageContainerName string = agentUsageContainer.name
output cosmosDbPricingContainerName string = modelPricingContainer.name
output cosmosDbStreamingExportConfigContainerName string = streamingExportConfigContainer.name
output cosmosDbQuotaOverridesContainerName string = quotaOverridesContainer.name
output cosmosDbQuotaOverrideRequestsContainerName string = quotaOverrideRequestsContainer.name
output resourceId string = database.id
output cosmosDbEndpoint string = 'https://${account.name}.documents.azure.com:443/'
