param name string
param location string
param privateLinkServiceId string
param subnetId string
param groupId string

resource pe 'Microsoft.Network/privateEndpoints@2024-05-01' = {
  name: name
  location: location
  properties: {
    subnet: { id: subnetId }
    privateLinkServiceConnections: [
      { name: name, properties: { privateLinkServiceId: privateLinkServiceId, groupIds: [ groupId ] } }
    ]
  }
}
