targetScope = 'resourceGroup'

@description('Deployment location.')
param location string = resourceGroup().location

var prefix = 'fd'

resource st 'Microsoft.Storage/storageAccounts@2026-09-01' = {
  name: '${prefix}${uniqueString(resourceGroup().id)}'
  location: location
  kind: 'StorageV2'
  sku: {
    name: 'Standard_LRS'
  }
}
