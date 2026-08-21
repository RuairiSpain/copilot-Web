@description('Name of an existing Key Vault in this resource group.')
param keyVaultName string

@description('Principal ID (managed identity) to grant read access to.')
param principalId string

// Built-in "Key Vault Secrets User" role — GetSecret/ListSecret only, not
// SetSecret/DeleteSecret/manage. https://learn.microsoft.com/azure/role-based-access-control/built-in-roles/security#key-vault-secrets-user
var keyVaultSecretsUserRoleId = '4633458b-17de-408a-b874-0445c86b69e6'

resource keyVault 'Microsoft.KeyVault/vaults@2023-07-01' existing = {
  name: keyVaultName
}

resource roleAssignment 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(keyVault.id, principalId, keyVaultSecretsUserRoleId)
  scope: keyVault
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', keyVaultSecretsUserRoleId)
    principalId: principalId
    principalType: 'ServicePrincipal'
  }
}
