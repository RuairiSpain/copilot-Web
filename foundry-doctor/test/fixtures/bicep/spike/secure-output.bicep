param location string = 'westeurope'
resource sa 'Microsoft.Storage/storageAccounts@2023-01-01' = {
  name: 'secsa'
  location: location
  sku: { name: 'Standard_LRS' }
  kind: 'StorageV2'
}
output key string = sa.listKeys().keys[0].value
@secure()
param pw string
output leakedPw string = pw
