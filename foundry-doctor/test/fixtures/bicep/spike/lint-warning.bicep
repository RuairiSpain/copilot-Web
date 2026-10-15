param unusedParam string = 'x'
var unusedVar = 'y'
resource sa 'Microsoft.Storage/storageAccounts@2023-01-01' = {
  name: 'lintsa'
  location: 'westeurope'
  sku: { name: 'Standard_LRS' }
  kind: 'StorageV2'
}
