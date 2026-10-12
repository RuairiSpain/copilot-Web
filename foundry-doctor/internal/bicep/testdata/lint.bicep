param unusedParam string = 'x'
param location string = resourceGroup().location

resource sa 'Microsoft.Storage/storageAccounts@2023-01-01' = {
  name: 'fdlintsa'
  location: location
  sku: { name: 'Standard_LRS' }
  kind: 'StorageV2'
}
