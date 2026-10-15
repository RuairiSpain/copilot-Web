param prefix string
param location string
param deployInner bool

module inner 'search.bicep' = if (deployInner) {
  name: 'inner'
  params: { name: '${prefix}-inner', location: location }
}

module always 'search.bicep' = {
  name: 'always'
  params: { name: '${prefix}-always', location: location }
}
