// Model deployments. They are created one at a time because the resource provider rejects
// concurrent changes to the same account.
param accountName string
@description('Objects with name, model, format, version, sku, capacity, versionUpgradeOption and raiPolicy.')
param deployments array

resource account 'Microsoft.CognitiveServices/accounts@2025-06-01' existing = {
  name: accountName
}

@batchSize(1)
resource modelDeployments 'Microsoft.CognitiveServices/accounts/deployments@2025-06-01' = [for d in deployments: {
  parent: account
  name: d.name
  sku: {
    name: d.sku
    capacity: d.capacity
  }
  properties: {
    model: {
      format: d.format
      name: d.model
      version: empty(d.version) ? null : d.version
    }
    versionUpgradeOption: d.versionUpgradeOption
    raiPolicyName: empty(d.raiPolicy) ? null : d.raiPolicy
  }
}]
