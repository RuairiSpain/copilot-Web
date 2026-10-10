targetScope = 'resourceGroup'

@description('Azure region for all resources.')
param location string = resourceGroup().location

@description('Short environment name used in resource names.')
@minLength(2)
@maxLength(12)
param environmentName string

@description('Whether to create the sample storage account.')
param deployStorage bool = true

var suffix = uniqueString(resourceGroup().id, environmentName)

module storage 'modules/storage.bicep' = if (deployStorage) {
  name: 'storage'
  params: {
    location: location
    name: toLower('st${take(replace(environmentName, '-', ''), 8)}${take(suffix, 10)}')
  }
}

output storageAccountName string = deployStorage ? storage.outputs.name : ''
