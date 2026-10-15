// Compiled with Bicep CLI 0.48.1. Nested modules: a conditional outer module
// containing a conditional and an unconditional inner module.
targetScope = 'resourceGroup'

param location string = resourceGroup().location
param deployOuter bool = false
var uniq = uniqueString(resourceGroup().id)

module outer 'modules/outer.bicep' = if (deployOuter) {
  name: 'outer'
  params: { prefix: 'n-${uniq}', location: location, deployInner: true }
}
