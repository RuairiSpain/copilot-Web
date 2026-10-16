// Compiled with Bicep CLI 0.48.1. A module deployed through a for loop.
targetScope = 'resourceGroup'

param location string = resourceGroup().location
var uniq = uniqueString(resourceGroup().id)

module s 'modules/search.bicep' = [for i in range(0, 2): {
  name: 's-${i}'
  params: { name: 'srch-l${i}-${uniq}', location: location }
}]
