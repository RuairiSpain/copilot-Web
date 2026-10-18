// Phase 8 sample: Azure AI Search for Foundry IQ and an API Management AI
// gateway in front of a Foundry (AIServices) account. Names and IDs are fake.
// Knowledge bases and knowledge sources are Search data-plane objects; they
// are not created here (see README).

@description('Location for all resources.')
param location string = resourceGroup().location

resource search 'Microsoft.Search/searchServices@2025-05-01' = {
  name: 'srch-iq-sample'
  location: location
  sku: {
    name: 'basic'
  }
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    semanticSearch: 'standard'
  }
}

resource foundry 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'aif-iq-sample'
  location: location
  kind: 'AIServices'
  sku: {
    name: 'S0'
  }
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    customSubDomainName: 'aif-iq-sample'
    disableLocalAuth: true
  }
}

resource embedding 'Microsoft.CognitiveServices/accounts/deployments@2025-06-01' = {
  parent: foundry
  name: 'text-embedding-3-large'
  sku: {
    name: 'Standard'
    capacity: 10
  }
  properties: {
    model: {
      format: 'OpenAI'
      name: 'text-embedding-3-large'
      version: '1'
    }
  }
}

resource appi 'Microsoft.Insights/components@2020-02-02' = {
  name: 'appi-iq-sample'
  location: location
  kind: 'web'
  properties: {
    Application_Type: 'web'
  }
}

resource apim 'Microsoft.ApiManagement/service@2024-05-01' = {
  name: 'apim-iq-sample'
  location: location
  sku: {
    name: 'BasicV2'
    capacity: 1
  }
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    publisherEmail: 'admin@contoso.example'
    publisherName: 'Contoso sample'
  }
}

resource logger 'Microsoft.ApiManagement/service/loggers@2024-05-01' = {
  parent: apim
  name: 'appinsights'
  properties: {
    loggerType: 'applicationInsights'
    resourceId: appi.id
    credentials: {
      instrumentationKey: appi.properties.InstrumentationKey
    }
  }
}

resource diagnostics 'Microsoft.ApiManagement/service/diagnostics@2024-05-01' = {
  parent: apim
  name: 'applicationinsights'
  properties: {
    loggerId: logger.id
    metrics: true
  }
}

resource tokenGovernance 'Microsoft.ApiManagement/service/policyFragments@2024-05-01' = {
  parent: apim
  name: 'token-governance'
  properties: {
    format: 'rawxml'
    value: loadTextContent('policies/token-governance.xml')
  }
}

resource chatApi 'Microsoft.ApiManagement/service/apis@2024-05-01' = {
  parent: apim
  name: 'foundry-chat'
  properties: {
    displayName: 'Foundry chat'
    path: 'foundry'
    protocols: [
      'https'
    ]
    serviceUrl: 'https://aif-iq-sample.openai.azure.com/openai'
    subscriptionRequired: true
  }
}

resource chatApiPolicy 'Microsoft.ApiManagement/service/apis/policies@2024-05-01' = {
  parent: chatApi
  name: 'policy'
  properties: {
    format: 'rawxml'
    value: loadTextContent('policies/foundry-chat.xml')
  }
  dependsOn: [
    tokenGovernance
  ]
}

resource apimOpenAIUser 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  // Cognitive Services OpenAI User, written inline so the compiled template
  // carries the role GUID literally.
  name: guid(foundry.id, apim.id, '5e0bd9bd-7b93-4f28-af87-19fc36ad61bd')
  scope: foundry
  properties: {
    principalId: apim.identity.principalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '5e0bd9bd-7b93-4f28-af87-19fc36ad61bd')
  }
}

output searchServiceName string = search.name
output gatewayName string = apim.name
