// OPS-004 shapes: a resource with no tags, one with literal tags and one with
// an expression-valued tags property. Compiled with Bicep CLI 0.48.1.
targetScope = 'resourceGroup'

param location string = resourceGroup().location
param tags object = {}

resource noTags 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'notags'
  location: location
  kind: 'AIServices'
  sku: { name: 'S0' }
  properties: {}
}

resource literalTags 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'literaltags'
  location: location
  kind: 'AIServices'
  sku: { name: 'S0' }
  tags: { owner: 'team-a', 'cost-center': 'cc1' }
  properties: {}
}

resource exprTags 'Microsoft.CognitiveServices/accounts@2025-06-01' = {
  name: 'exprtags'
  location: location
  kind: 'AIServices'
  sku: { name: 'S0' }
  tags: tags
  properties: {}
}
