targetScope = 'resourceGroup'

var tags = {
  env: 'prod'
  owner: 'platform'
}

resource workspace 'Microsoft.OperationalInsights/workspaces@2023-09-01' = {
  name: 'fdgoodsafelaw'
  location: 'swedencentral'
  tags: tags
  properties: {
    retentionInDays: 30
  }
}

resource vnet 'Microsoft.Network/virtualNetworks@2023-11-01' = {
  name: 'fdgoodsafe-vnet'
  location: 'swedencentral'
  properties: {
    addressSpace: {
      addressPrefixes: [
        '10.20.0.0/16'
      ]
    }
  }
}

resource agentSubnet 'Microsoft.Network/virtualNetworks/subnets@2023-11-01' = {
  parent: vnet
  name: 'agents'
  properties: {
    addressPrefix: '10.20.0.0/24'
    delegations: [
      {
        name: 'agent-delegation'
        properties: {
          serviceName: 'Microsoft.App/environments'
        }
      }
    ]
  }
}

resource peSubnet 'Microsoft.Network/virtualNetworks/subnets@2023-11-01' = {
  parent: vnet
  name: 'private-endpoints'
  properties: {
    addressPrefix: '10.20.1.0/24'
    privateEndpointNetworkPolicies: 'Disabled'
  }
}

resource search 'Microsoft.Search/searchServices@2025-05-01' = {
  name: 'fdgoodsafesrch'
  location: 'swedencentral'
  sku: {
    name: 'standard'
  }
  tags: tags
  properties: {
    disableLocalAuth: true
    publicNetworkAccess: 'Disabled'
    replicaCount: 2
    partitionCount: 1
  }
}

resource storage 'Microsoft.Storage/storageAccounts@2023-01-01' = {
  name: 'fdgoodsafestore'
  location: 'swedencentral'
  sku: {
    name: 'Standard_GZRS'
  }
  kind: 'StorageV2'
  tags: tags
  properties: {
    allowBlobPublicAccess: false
    allowSharedKeyAccess: false
    minimumTlsVersion: 'TLS1_2'
    publicNetworkAccess: 'Disabled'
    supportsHttpsTrafficOnly: true
  }
}

resource blobService 'Microsoft.Storage/storageAccounts/blobServices@2023-01-01' = {
  parent: storage
  name: 'default'
}

resource cosmos 'Microsoft.DocumentDB/databaseAccounts@2025-05-01-preview' = {
  name: 'fdgoodsafecosmos'
  location: 'swedencentral'
  kind: 'GlobalDocumentDB'
  tags: tags
  properties: {
    databaseAccountOfferType: 'Standard'
    disableLocalAuth: true
    locations: [
      {
        locationName: 'swedencentral'
        failoverPriority: 0
        isZoneRedundant: true
      }
    ]
    backupPolicy: {
      type: 'Continuous'
    }
    publicNetworkAccess: 'Disabled'
  }
}

resource account 'Microsoft.CognitiveServices/accounts@2026-07-15-preview' = {
  name: 'fdgoodsafe-acct'
  location: 'swedencentral'
  kind: 'AIServices'
  tags: tags
  sku: {
    name: 'S0'
  }
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    allowProjectManagement: true
    customSubDomainName: 'fdgoodsafe-acct'
    disableLocalAuth: true
    publicNetworkAccess: 'Disabled'
    networkInjections: [
      {
        scenario: 'agent'
        subnetArmId: agentSubnet.id
        useMicrosoftManagedNetwork: false
      }
    ]
    capabilitySettings: {
      documentStore: cosmos.id
      vectorStore: search.id
      blobStore: storage.id
    }
  }
}

resource project 'Microsoft.CognitiveServices/accounts/projects@2026-07-15-preview' = {
  parent: account
  name: 'fdgoodsafe-project'
  location: 'swedencentral'
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    displayName: 'fdgoodsafe-project'
  }
}

resource deployment 'Microsoft.CognitiveServices/accounts/deployments@2026-09-01' = {
  parent: account
  name: 'chat'
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
    versionUpgradeOption: 'NoAutoUpgrade'
  }
}

resource accountPe 'Microsoft.Network/privateEndpoints@2023-11-01' = {
  name: 'fdgoodsafe-foundry-pe'
  location: 'swedencentral'
  properties: {
    subnet: {
      id: peSubnet.id
    }
    privateLinkServiceConnections: [
      {
        name: 'foundry-account'
        properties: {
          privateLinkServiceId: account.id
          groupIds: [
            'account'
          ]
        }
      }
    ]
  }
}

resource searchPe 'Microsoft.Network/privateEndpoints@2023-11-01' = {
  name: 'fdgoodsafe-search-pe'
  location: 'swedencentral'
  properties: {
    subnet: {
      id: peSubnet.id
    }
    privateLinkServiceConnections: [
      {
        name: 'search'
        properties: {
          privateLinkServiceId: search.id
          groupIds: [
            'searchService'
          ]
        }
      }
    ]
  }
}

resource storagePe 'Microsoft.Network/privateEndpoints@2023-11-01' = {
  name: 'fdgoodsafe-storage-pe'
  location: 'swedencentral'
  properties: {
    subnet: {
      id: peSubnet.id
    }
    privateLinkServiceConnections: [
      {
        name: 'blob'
        properties: {
          privateLinkServiceId: storage.id
          groupIds: [
            'blob'
          ]
        }
      }
    ]
  }
}

resource cosmosPe 'Microsoft.Network/privateEndpoints@2023-11-01' = {
  name: 'fdgoodsafe-cosmos-pe'
  location: 'swedencentral'
  properties: {
    subnet: {
      id: peSubnet.id
    }
    privateLinkServiceConnections: [
      {
        name: 'sql'
        properties: {
          privateLinkServiceId: cosmos.id
          groupIds: [
            'Sql'
          ]
        }
      }
    ]
  }
}

resource searchZone 'Microsoft.Network/privateDnsZones@2020-06-01' = {
  name: 'privatelink.search.windows.net'
  location: 'global'
}

resource blobZone 'Microsoft.Network/privateDnsZones@2020-06-01' = {
  name: 'privatelink.blob.core.windows.net'
  location: 'global'
}

resource accountZone 'Microsoft.Network/privateDnsZones@2020-06-01' = {
  name: 'privatelink.services.ai.azure.com'
  location: 'global'
}

resource cognitiveZone 'Microsoft.Network/privateDnsZones@2020-06-01' = {
  name: 'privatelink.cognitiveservices.azure.com'
  location: 'global'
}

resource openAIZone 'Microsoft.Network/privateDnsZones@2020-06-01' = {
  name: 'privatelink.openai.azure.com'
  location: 'global'
}

resource cosmosZone 'Microsoft.Network/privateDnsZones@2020-06-01' = {
  name: 'privatelink.documents.azure.com'
  location: 'global'
}

resource searchZoneLink 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2020-06-01' = {
  parent: searchZone
  name: 'fdgoodsafe-search-link'
  location: 'global'
  properties: {
    registrationEnabled: false
    virtualNetwork: {
      id: vnet.id
    }
  }
}

resource blobZoneLink 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2020-06-01' = {
  parent: blobZone
  name: 'fdgoodsafe-blob-link'
  location: 'global'
  properties: {
    registrationEnabled: false
    virtualNetwork: {
      id: vnet.id
    }
  }
}

resource accountZoneLink 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2020-06-01' = {
  parent: accountZone
  name: 'fdgoodsafe-account-link'
  location: 'global'
  properties: {
    registrationEnabled: false
    virtualNetwork: {
      id: vnet.id
    }
  }
}

resource cognitiveZoneLink 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2020-06-01' = {
  parent: cognitiveZone
  name: 'fdgoodsafe-cognitive-link'
  location: 'global'
  properties: {
    registrationEnabled: false
    virtualNetwork: {
      id: vnet.id
    }
  }
}

resource openAIZoneLink 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2020-06-01' = {
  parent: openAIZone
  name: 'fdgoodsafe-openai-link'
  location: 'global'
  properties: {
    registrationEnabled: false
    virtualNetwork: {
      id: vnet.id
    }
  }
}

resource cosmosZoneLink 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2020-06-01' = {
  parent: cosmosZone
  name: 'fdgoodsafe-cosmos-link'
  location: 'global'
  properties: {
    registrationEnabled: false
    virtualNetwork: {
      id: vnet.id
    }
  }
}

resource searchZoneGroup 'Microsoft.Network/privateEndpoints/privateDnsZoneGroups@2023-11-01' = {
  parent: searchPe
  name: 'default'
  properties: {
    privateDnsZoneConfigs: [
      {
        name: 'search-zone'
        properties: {
          privateDnsZoneId: searchZone.id
        }
      }
    ]
  }
}

resource storageZoneGroup 'Microsoft.Network/privateEndpoints/privateDnsZoneGroups@2023-11-01' = {
  parent: storagePe
  name: 'default'
  properties: {
    privateDnsZoneConfigs: [
      {
        name: 'blob-zone'
        properties: {
          privateDnsZoneId: blobZone.id
        }
      }
    ]
  }
}

resource accountZoneGroup 'Microsoft.Network/privateEndpoints/privateDnsZoneGroups@2023-11-01' = {
  parent: accountPe
  name: 'default'
  properties: {
    privateDnsZoneConfigs: [
      {
        name: 'foundry-zone'
        properties: {
          privateDnsZoneId: accountZone.id
        }
      }
      {
        name: 'cognitive-zone'
        properties: {
          privateDnsZoneId: cognitiveZone.id
        }
      }
      {
        name: 'openai-zone'
        properties: {
          privateDnsZoneId: openAIZone.id
        }
      }
    ]
  }
}

resource cosmosZoneGroup 'Microsoft.Network/privateEndpoints/privateDnsZoneGroups@2023-11-01' = {
  parent: cosmosPe
  name: 'default'
  properties: {
    privateDnsZoneConfigs: [
      {
        name: 'cosmos-zone'
        properties: {
          privateDnsZoneId: cosmosZone.id
        }
      }
    ]
  }
}

resource accountLogs 'Microsoft.Insights/diagnosticSettings@2021-05-01-preview' = {
  name: 'fdgoodsafe-acct-diag'
  scope: account
  properties: {
    workspaceId: workspace.id
    logs: [
      {
        categoryGroup: 'audit'
        enabled: true
      }
    ]
  }
}

resource projectLogs 'Microsoft.Insights/diagnosticSettings@2021-05-01-preview' = {
  name: 'fdgoodsafe-project-diag'
  scope: project
  properties: {
    workspaceId: workspace.id
    logs: [
      {
        categoryGroup: 'audit'
        enabled: true
      }
    ]
  }
}

resource searchLogs 'Microsoft.Insights/diagnosticSettings@2021-05-01-preview' = {
  name: 'fdgoodsafesrch-diag'
  scope: search
  properties: {
    workspaceId: workspace.id
    logs: [
      {
        category: 'OperationLogs'
        enabled: true
      }
    ]
  }
}

resource blobLogs 'Microsoft.Insights/diagnosticSettings@2021-05-01-preview' = {
  name: 'fdgoodsafestore-blob-diag'
  scope: blobService
  properties: {
    workspaceId: workspace.id
    logs: [
      {
        category: 'StorageRead'
        enabled: true
      }
      {
        category: 'StorageWrite'
        enabled: true
      }
    ]
  }
}

resource cosmosLogs 'Microsoft.Insights/diagnosticSettings@2021-05-01-preview' = {
  name: 'fdgoodsafecosmos-diag'
  scope: cosmos
  properties: {
    workspaceId: workspace.id
    logs: [
      {
        category: 'DataPlaneRequests'
        enabled: true
      }
    ]
  }
}

resource storageBlobContributor 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(storage.id, account.id, 'Storage Blob Data Contributor')
  scope: storage
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'ba92f5b4-2d11-453d-a403-e96b0029c9fe')
    principalId: account.identity.principalId
    principalType: 'ServicePrincipal'
  }
}

resource searchContributor 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(search.id, account.id, 'Search Index Data Contributor')
  scope: search
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '8ebe5a00-799e-43f5-93ac-243d3dce84a7')
    principalId: account.identity.principalId
    principalType: 'ServicePrincipal'
  }
}

resource cosmosOperator 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(cosmos.id, account.id, 'Cosmos DB Operator')
  scope: cosmos
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '230815da-be43-4aae-9cb4-875f7bd000aa')
    principalId: account.identity.principalId
    principalType: 'ServicePrincipal'
  }
}

resource actionGroup 'Microsoft.Insights/actionGroups@2023-01-01' = {
  name: 'fdgoodsafe-ops'
  location: 'global'
  properties: {
    enabled: true
    groupShortName: 'fdops'
    emailReceivers: [
      {
        name: 'platform'
        emailAddress: 'platform@example.com'
      }
    ]
  }
}

resource serviceHealthAlert 'Microsoft.Insights/activityLogAlerts@2023-01-01-preview' = {
  name: 'fdgoodsafe-servicehealth'
  location: 'global'
  properties: {
    enabled: true
    scopes: [
      subscription().id
    ]
    condition: {
      allOf: [
        {
          field: 'category'
          equals: 'ServiceHealth'
        }
      ]
    }
    actions: {
      actionGroups: [
        {
          actionGroupId: actionGroup.id
        }
      ]
    }
  }
}

resource availabilityAlert 'Microsoft.Insights/metricAlerts@2024-03-01-preview' = {
  name: 'fdgoodsafe-model-availability'
  location: 'global'
  properties: {
    severity: 2
    enabled: true
    scopes: [
      account.id
    ]
    evaluationFrequency: 'PT5M'
    windowSize: 'PT15M'
    criteria: {
      'odata.type': 'Microsoft.Azure.Monitor.SingleResourceMultipleMetricCriteria'
      allOf: [
        {
          name: 'availability'
          metricName: 'ModelAvailabilityRate'
          operator: 'LessThan'
          threshold: 99
          timeAggregation: 'Average'
          criterionType: 'StaticThresholdCriterion'
        }
      ]
    }
    actions: [
      {
        actionGroupId: actionGroup.id
      }
    ]
  }
}

resource budget 'Microsoft.Consumption/budgets@2023-11-01' = {
  name: 'fdgoodsafe-monthly'
  properties: {
    category: 'Cost'
    amount: 500
    timeGrain: 'Monthly'
    timePeriod: {
      startDate: '2026-11-01T00:00:00Z'
    }
    notifications: {
      actual80: {
        enabled: true
        operator: 'GreaterThan'
        threshold: 80
        thresholdType: 'Actual'
        contactEmails: [
          'finops@example.com'
        ]
      }
      forecast100: {
        enabled: true
        operator: 'GreaterThan'
        threshold: 100
        thresholdType: 'Forecasted'
        contactEmails: [
          'finops@example.com'
        ]
      }
    }
  }
}
