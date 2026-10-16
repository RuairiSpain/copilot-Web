// SEC-014 shapes: outputs derived from a secure parameter, from a list*()
// function, from a variable that wraps either, name-only hits and clean
// outputs. Compiled with Bicep CLI 0.48.1.
targetScope = 'resourceGroup'

@secure()
param adminPassword string
param location string = resourceGroup().location

var wrapped = adminPassword

resource storage 'Microsoft.Storage/storageAccounts@2023-05-01' = {
  name: 'sec014stg'
  location: location
  kind: 'StorageV2'
  sku: { name: 'Standard_LRS' }
}

resource kv 'Microsoft.KeyVault/vaults@2023-07-01' = {
  name: 'sec014kv'
  location: location
  properties: {
    tenantId: subscription().tenantId
    sku: { family: 'A', name: 'standard' }
    accessPolicies: []
  }
}

output pw string = adminPassword
output viaVar string = wrapped
output k string = storage.listKeys().keys[0].value
output keyVaultUri string = kv.properties.vaultUri
output apiKeyName string = 'x'
output storageId string = storage.id
output literalText string = 'adminPassword listKeys(x)'
@secure()
output safePw string = adminPassword
