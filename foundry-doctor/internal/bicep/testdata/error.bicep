param location string
resource sa 'Microsoft.Storage/storageAccounts@2023-01-01' = {
  name: 'errsa'
  location: location
  sku: { name: 'Standard_LRS' }
  kind: 'StorageV2'
  properties: {
    notARealProp: 1
  }
}
output bad string = undefinedSymbol
