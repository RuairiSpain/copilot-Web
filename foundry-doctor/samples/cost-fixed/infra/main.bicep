// ILLUSTRATIVE SAMPLE for `foundry-doctor cost`. It is not a deployable reference:
// model names, model versions and the PTU count below are placeholders that were not
// verified against model availability or the per-model minimum PTU (minimums vary by
// model; see https://learn.microsoft.com/azure/foundry/foundry-models/concepts/deployment-types).
targetScope = 'resourceGroup'

param location string = resourceGroup().location

resource search 'Microsoft.Search/searchServices@2025-05-01' = {
  name: 'fd-cost-search'
  location: location
  sku: {
    name: 'standard2'
  }
  properties: {
    replicaCount: 2
    partitionCount: 1
  }
}

resource apim 'Microsoft.ApiManagement/service@2024-05-01' = {
  name: 'fd-cost-apim'
  location: location
  sku: {
    name: 'Premium'
    capacity: 1
  }
  properties: {
    publisherEmail: 'sample@example.com'
    publisherName: 'Foundry Doctor Sample'
  }
}

resource cosmosAccount 'Microsoft.DocumentDB/databaseAccounts@2025-04-15' = {
  name: 'fd-cost-cosmos'
  location: location
  properties: {
    databaseAccountOfferType: 'Standard'
    locations: [
      {
        locationName: location
        failoverPriority: 0
        isZoneRedundant: false
      }
    ]
  }
}

// Dedicated database throughput set through options.throughput; the estimator
// takes the region from the parent account's locations.
resource cosmosDb 'Microsoft.DocumentDB/databaseAccounts/sqlDatabases@2025-04-15' = {
  parent: cosmosAccount
  name: 'default'
  properties: {
    resource: {
      id: 'default'
    }
    options: {
      throughput: 4000
    }
  }
}

resource tokenDeployment 'Microsoft.CognitiveServices/accounts/deployments@2026-09-01' = {
  name: 'fdacct/chat'
  sku: {
    name: 'GlobalStandard'
    capacity: 10
  }
  properties: {
    model: {
      format: 'OpenAI'
      name: 'gpt-4.1-mini'
      version: '2026-03-17'
    }
  }
}

resource ptuDeployment 'Microsoft.CognitiveServices/accounts/deployments@2026-09-01' = {
  name: 'fdacct/ptu'
  sku: {
    name: 'ProvisionedManaged'
    capacity: 5
  }
  properties: {
    model: {
      format: 'OpenAI'
      name: 'gpt-4.1'
      version: '2026-03-17'
    }
  }
}
