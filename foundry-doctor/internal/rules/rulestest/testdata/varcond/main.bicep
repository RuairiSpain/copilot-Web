// Compiled with Bicep CLI 0.48.1: bicep build main.bicep --outfile main.json
// Variable-mediated conditions, the common "use an existing account" shape
// (ADR-018 amendment 2).
targetScope = 'resourceGroup'

param location string = resourceGroup().location
param existingAccountResourceId string = ''
param customName string = ''

var useExisting = !empty(existingAccountResourceId)
var accountName = length(customName) == 0 ? 'auto-ai' : customName
var viaVariable = !useExisting && contains(accountName, 'ai')

resource account 'Microsoft.CognitiveServices/accounts@2025-06-01' = if (!useExisting) {
  name: accountName
  location: location
  kind: 'AIServices'
  sku: { name: 'S0' }
  properties: {
    customSubDomainName: accountName
    disableLocalAuth: true
  }
}

resource second 'Microsoft.Storage/storageAccounts@2023-05-01' = if (viaVariable) {
  name: 'stgvarcond'
  location: location
  kind: 'StorageV2'
  sku: { name: 'Standard_LRS' }
}
