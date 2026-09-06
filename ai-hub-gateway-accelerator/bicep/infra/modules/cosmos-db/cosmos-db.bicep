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
@description('The manual (fixed) throughput for the container, used only when enableAutoscale is false.')
param throughput int = 400

@description('When true (default), every container in this module uses Cosmos autoscale throughput instead of a fixed manual RU/s. Fixes this session\'s own scalability review finding: previously every container shared one fixed, manually-provisioned RU/s value with no ability to absorb the bursty per-record writes the usage-ingestion Logic Apps do in each batch run. Set false only if you have a specific reason to manage RU/s manually (e.g. a committed reserved-capacity purchase already sized for your load).')
param enableAutoscale bool = true

@minValue(1000)
@maxValue(1000000)
@description('Autoscale ceiling (RU/s) when enableAutoscale is true — Cosmos scales each container automatically between this value ÷ 10 and this value based on load. 1000 is the Cosmos-enforced minimum for autoscale; size this per your expected peak ingestion burst, not your steady-state average. Applied uniformly to every container in this module for simplicity — if your containers have meaningfully different load profiles, consider giving the highest-volume ones (llmUsageContainer, mcpUsageContainer, agentUsageContainer) their own ceiling in a follow-up, since this module intentionally keeps one shared value rather than guessing per-container numbers with no real traffic data to size them against.')
param maxAutoscaleThroughput int = 4000

// Shared `options` block every container below uses — keeps the
// autoscale-vs-manual choice in exactly one place rather than repeated
// per container.
var containerThroughputOptions = enableAutoscale
  ? { autoscaleSettings: { maxThroughput: maxAutoscaleThroughput } }
  : { throughput: throughput }

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

// SCOPED OUT of this session's own partition-key fix (see
// llmUsageContainer's comment below for the fix applied to this fork's
// three new usage containers): /productName has the identical
// low-cardinality problem here, but this is the base accelerator's
// original legacy container (fed by ai-usage-ingestion, a workflow this
// fork didn't touch or trace in full) — repartitioning it would need
// coordinated changes to that upstream writer too, outside what this
// pass verified end to end. Flagged here rather than silently left
// unaddressed; treat as a real follow-up, not a non-issue.
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
    options: containerThroughputOptions
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
    options: containerThroughputOptions
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
    options: containerThroughputOptions
  }
}

// SCOPED OUT of this session's own partition-key fix, and actually the
// WORST case of the pattern flagged there: /type isn't just
// low-cardinality, frag-pii-state-saving.xml sets it to the single
// hardcoded constant "PII_Processing" for every document — every write
// to this container lands in ONE logical partition, always, with no
// per-request variation at all. Left as-is here because this container
// carries raw/deanonymized PII (a materially higher-stakes write path to
// touch without full end-to-end tracing of every reader), and its writer
// (frag-pii-state-saving.xml, an APIM policy fragment, not a Logic App
// this session edited) needs the same fix in lockstep with any container
// change. Flagged here rather than silently left unaddressed; treat as
// a real, higher-priority follow-up, not a non-issue.
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
    options: containerThroughputOptions
  }
}

// Partitioned by /pkShard, NOT /productName — this session's own
// scalability review flagged /productName as a low-cardinality key
// (tens of distinct values at most, per citadel-access-contracts'
// one-subscription-per-use-case model): a single logical partition's
// throughput is capped regardless of the container's total provisioned
// RU/s, so the busiest product's writes throttle long before the
// account's RU budget is exhausted, while the rest sits idle.
// `pkShard` (written by llm-usage-ingestion's Create_Usage_Log, e.g.
// "AcmeCorp-Prod_20260831") is `{productName}_{yyyyMMdd}` — spreads a
// single popular product's writes across many logical partitions
// (bounded further by date, so old partitions naturally stop taking
// writes) while every existing analytical query pattern (Power BI
// already filters/aggregates by time range and product) is unaffected,
// since nothing here relies on a point-read keyed on productName alone.
//
// BREAKING CHANGE ON UPGRADE, stated plainly: Cosmos partition keys are
// immutable — this only takes effect for a NEW container. An
// already-deployed account keeps its existing /productName-partitioned
// container as-is; adopting this fix there needs a real data migration
// (a new container + a backfill copy, e.g. via Azure Data Factory or the
// Cosmos DB change feed), not just a redeploy of this template.
resource llmUsageContainer 'Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers@2024-02-15-preview' = {
  parent: database
  name: llmUsageContainerName
  properties: {
    resource: {
      id: llmUsageContainerName
      partitionKey: {
        paths: [
          '/pkShard'
        ]
        kind: 'Hash'
      }
      indexingPolicy: {
        indexingMode: 'consistent'
        automatic: true
      }
    }
    options: containerThroughputOptions
  }
}

// Published Tools (MCP) usage. Fed by the mcp-usage-ingestion Logic App which aggregates the
// 'mcp-usage' App Insights custom metrics (mirrors the LLM usage pipeline).
// Partitioned by /pkShard ({productName}_{yyyyMMdd}) — see llmUsageContainer's
// comment above for the full scalability rationale and the breaking-change
// note; the same reasoning applies here.
resource mcpUsageContainer 'Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers@2024-02-15-preview' = {
  parent: database
  name: mcpUsageContainerName
  properties: {
    resource: {
      id: mcpUsageContainerName
      partitionKey: {
        paths: [
          '/pkShard'
        ]
        kind: 'Hash'
      }
      indexingPolicy: {
        indexingMode: 'consistent'
        automatic: true
      }
    }
    options: containerThroughputOptions
  }
}

// Published Agents (A2A) usage. Fed by the agent-usage-ingestion Logic App which aggregates the
// 'a2a-usage' App Insights custom metrics.
// Partitioned by /pkShard ({productName}_{yyyyMMdd}) — see llmUsageContainer's
// comment above for the full scalability rationale and the breaking-change
// note; the same reasoning applies here.
resource agentUsageContainer 'Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers@2024-02-15-preview' = {
  parent: database
  name: agentUsageContainerName
  properties: {
    resource: {
      id: agentUsageContainerName
      partitionKey: {
        paths: [
          '/pkShard'
        ]
        kind: 'Hash'
      }
      indexingPolicy: {
        indexingMode: 'consistent'
        automatic: true
      }
    }
    options: containerThroughputOptions
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
    options: containerThroughputOptions
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
    options: containerThroughputOptions
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
