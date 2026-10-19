targetScope = 'resourceGroup'

// Intentionally defective sample: see ../README.md for the rule each defect triggers.

resource search 'Microsoft.Search/searchServices@2025-05-01' = {
  name: 'fdbadprodsrch'
  location: 'eastus-typo'
  sku: {
    name: 'standard'
  }
  properties: {
    replicaCount: 2
    partitionCount: 1
  }
}

resource storage 'Microsoft.Storage/storageAccounts@2023-01-01' = {
  name: 'fdbadprodstore'
  location: 'swedencentral'
  sku: {
    name: 'Standard_GZRS'
  }
  kind: 'StorageV2'
  properties: {
    minimumTlsVersion: 'TLS1_2'
    supportsHttpsTrafficOnly: true
  }
}

resource cosmos 'Microsoft.DocumentDB/databaseAccounts@2025-05-01-preview' = {
  name: 'fdbadprodcosmos'
  location: 'swedencentral'
  kind: 'GlobalDocumentDB'
  properties: {
    databaseAccountOfferType: 'Standard'
    locations: [
      {
        locationName: 'swedencentral'
        failoverPriority: 0
        isZoneRedundant: true
      }
    ]
  }
}

resource account 'Microsoft.CognitiveServices/accounts@2026-07-15-preview' = {
  name: 'fdbadprod-acct'
  location: 'swedencentral'
  kind: 'AIServices'
  sku: {
    name: 'S0'
  }
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    allowProjectManagement: true
    customSubDomainName: 'fdbadprod-acct'
    capabilitySettings: {
      documentStore: cosmos.id
      vectorStore: search.id
      blobStore: storage.id
    }
  }
}

resource project 'Microsoft.CognitiveServices/accounts/projects@2026-07-15-preview' = {
  parent: account
  name: 'fdbadprod-project'
  location: 'swedencentral'
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    displayName: 'fdbadprod-project'
  }
}

// REL-008: provisioned deployment without spilloverDeploymentName.
// SEC-007: raiPolicyName names a policy that is not declared on the account.
resource ptuDeployment 'Microsoft.CognitiveServices/accounts/deployments@2026-09-01' = {
  parent: account
  name: 'chat-ptu'
  sku: {
    name: 'ProvisionedManaged'
    capacity: 50
  }
  properties: {
    model: {
      format: 'OpenAI'
      name: 'gpt-4.1-mini'
      version: '2026-03-17'
    }
    versionUpgradeOption: 'NoAutoUpgrade'
    raiPolicyName: 'undeclared-strict-policy'
  }
}

// IDN-002: the only role assignment is for an unrelated group; the project
// identity has no role on Search, Storage or Cosmos DB. A template with no role
// assignment at all would make the rule skip instead of fail.
resource readerForOperators 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(account.id, 'operators', 'Reader')
  scope: account
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'acdd72a7-3385-48ef-bd42-f606fba81ae7')
    principalId: '00000000-0000-0000-0000-000000000001'
    principalType: 'Group'
  }
}
