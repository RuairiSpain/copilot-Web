param name string
param location string
param publicNetworkAccess string

resource search 'Microsoft.Search/searchServices@2025-05-01' = {
  name: name
  location: location
  sku: { name: 'basic' }
  properties: {
    publicNetworkAccess: publicNetworkAccess
  }
}
