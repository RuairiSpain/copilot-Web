param name string
param location string = resourceGroup().location
param tags object = {}

var eventHubsDataOwnerRoleDefinitionId = resourceId('Microsoft.Authorization/roleDefinitions', 'f526a384-b230-433a-b45c-95f59c4a2dec')

resource managedIdentity 'Microsoft.ManagedIdentity/userAssignedIdentities@2018-11-30' = {
  name: name
  location: location
  tags: union(tags, { 'azd-service-name': name })
}

// Assign to Azure Event Hubs Data Owner role to the user-defined managed identity used by workloads.
// ARM role assignments accept a principalType hint, which lets Azure skip the synchronous AAD
// principal-existence check and tolerate replication delay for a freshly created identity.
resource eventHubsDataOwnerRoleAssignment 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(managedIdentity.id, eventHubsDataOwnerRoleDefinitionId)
  scope: resourceGroup()
  properties: {
    roleDefinitionId: eventHubsDataOwnerRoleDefinitionId
    principalId: managedIdentity.properties.principalId
    principalType: 'ServicePrincipal'
  }
}

output managedIdentityName string = managedIdentity.name
output managedIdentityId string = managedIdentity.id
output managedIdentityPrincipalId string = managedIdentity.properties.principalId
