// ============================================================================
// keyvault-apim-system-rbac.bicep
// ----------------------------------------------------------------------------
// Grants the APIM SYSTEM-assigned managed identity read access to Key Vault
// secrets and certificates. Must be invoked AFTER the APIM service is created
// (the system-assigned principal does not exist before APIM provisions).
// ============================================================================

@description('Name of an existing Key Vault to grant access on.')
param keyVaultName string

@description('Principal ID of the APIM system-assigned managed identity.')
param apimSystemAssignedPrincipalId string

// Built-in Key Vault roles
var keyVaultSecretsUserRoleId = '4633458b-17de-408a-b874-0445c86b69e6'
var keyVaultCertificateUserRoleId = 'db79e9a7-68ee-4b58-9aeb-b90e7c24fcba'

resource keyVault 'Microsoft.KeyVault/vaults@2025-05-01' existing = {
  name: keyVaultName
}

// Secrets User: required for named-value Key Vault references.
resource secretsUser 'Microsoft.Authorization/roleAssignments@2022-04-01' = if (!empty(apimSystemAssignedPrincipalId)) {
  scope: keyVault
  name: guid(subscription().id, resourceGroup().id, keyVault.name, keyVaultSecretsUserRoleId, apimSystemAssignedPrincipalId)
  properties: {
    roleDefinitionId: resourceId('Microsoft.Authorization/roleDefinitions', keyVaultSecretsUserRoleId)
    principalId: apimSystemAssignedPrincipalId
    principalType: 'ServicePrincipal'
  }
}

// Certificate User: required for custom-domain certs and certificate-as-named-value scenarios.
resource certificateUser 'Microsoft.Authorization/roleAssignments@2022-04-01' = if (!empty(apimSystemAssignedPrincipalId)) {
  scope: keyVault
  name: guid(subscription().id, resourceGroup().id, keyVault.name, keyVaultCertificateUserRoleId, apimSystemAssignedPrincipalId)
  properties: {
    roleDefinitionId: resourceId('Microsoft.Authorization/roleDefinitions', keyVaultCertificateUserRoleId)
    principalId: apimSystemAssignedPrincipalId
    principalType: 'ServicePrincipal'
  }
}
