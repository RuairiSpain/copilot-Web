targetScope = 'resourceGroup'

@description('Deployment location')
param location string = resourceGroup().location
param deployExtra bool = true
param accountNames array = [
  'acctone'
  'accttwo'
]
@secure()
param adminPassword string = newGuid()

var prefix = 'fdspike'

// conditional resource
resource extra 'Microsoft.Storage/storageAccounts@2023-01-01' = if (deployExtra) {
  name: '${prefix}extra'
  location: location
  sku: { name: 'Standard_LRS' }
  kind: 'StorageV2'
}

// looped resource
resource accts 'Microsoft.Storage/storageAccounts@2023-01-01' = [for (n, i) in accountNames: {
  name: '${prefix}${n}${i}'
  location: location
  sku: { name: 'Standard_GRS' }
  kind: 'StorageV2'
}]

// plain module
module single 'modules/storage.bicep' = {
  name: 'single'
  params: { name: '${prefix}mod', location: location }
}

// looped + conditional module
module many 'modules/storage.bicep' = [for n in accountNames: if (deployExtra) {
  name: 'many-${n}'
  params: { name: '${prefix}m${n}', location: location }
}]

// nested child resource + existing
resource blobSvc 'Microsoft.Storage/storageAccounts/blobServices@2023-01-01' = {
  parent: extra
  name: 'default'
}

output extraId string = deployExtra ? extra.id : ''
output moduleId string = single.outputs.id
