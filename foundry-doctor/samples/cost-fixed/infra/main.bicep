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

resource cosmosDb 'Microsoft.DocumentDB/databaseAccounts/sqlDatabases/throughputSettings@2026-03-15' = {
  name: 'fd-cost-cosmos/default/default'
  location: location
  properties: {
    resource: {
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
