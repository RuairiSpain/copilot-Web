param name string
param location string

resource search 'Microsoft.Search/searchServices@2025-05-01' = {
  name: name
  location: location
  sku: { name: 'standard' }
  properties: { disableLocalAuth: false }
}

output id string = search.id
output name string = search.name
