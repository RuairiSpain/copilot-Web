// Compiled with Bicep CLI 0.48.1. A module declared under a parameter-driven
// condition (default false) and one under a literal false condition.
targetScope = 'resourceGroup'

param location string = resourceGroup().location
param deploySearch bool = false
var uniq = uniqueString(resourceGroup().id)

module s1 'modules/search.bicep' = if (deploySearch) {
  name: 's1'
  params: { name: 'srch-c-${uniq}', location: location }
}

module s2 'modules/search.bicep' = if (false) {
  name: 's2'
  params: { name: 'srch-never-${uniq}', location: location }
}
