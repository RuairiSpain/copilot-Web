package idn

// Recommendation and fix texts copied from the catalogue (rules/catalog/idn). A test compares them with the
// catalogue so they cannot drift.
const (
	rec001 = "Enable a system-assigned or user-assigned managed identity on the Foundry account, on each project and on every workload that calls Foundry, then grant that identity data-plane roles on the dependent resources (see FND-IDN-002). Choose the user-assigned option at project creation if you need it, because the project identity type cannot be changed later."
	fix001 = `resource account 'Microsoft.CognitiveServices/accounts@2026-09-01' = {
  name: accountName
  location: location
  kind: 'AIServices'
  sku: { name: 'S0' }
  identity: { type: 'SystemAssigned' }
  properties: { /* ... */ }
}
resource project 'Microsoft.CognitiveServices/accounts/projects@2026-09-01' = {
  parent: account
  name: projectName
  location: location
  identity: { type: 'SystemAssigned' }
  properties: {}
}`
	rec002 = "Assign the project managed identity the documented data roles on each store before agents run: Search Index Data Contributor and Search Service Contributor on Search, Storage Blob Data Contributor and Storage Blob Data Owner on the storage containers, Cosmos DB Operator on the account plus the Cosmos DB Built-in Data Contributor SQL role on the database. Assign them at the narrowest scope that works. These roles are separate from the roles the deploying identity needs."
	fix002 = `resource searchIndexData 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: search
  name: guid(search.id, project.id, '8ebe5a00-799e-43f5-93ac-243d3dce84a7')
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '8ebe5a00-799e-43f5-93ac-243d3dce84a7') // Search Index Data Contributor
    principalId: project.identity.principalId
    principalType: 'ServicePrincipal'
  }
}`
	rec003 = "Pass the object ID of the identity (identity.principalId or the user-assigned identity's properties.principalId), never the client ID, and set principalType explicitly. Use ServicePrincipal for managed identities and service principals created in the same deployment, and use API version 2022-04-01 or later."
	fix003 = `resource blobRole 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: storage
  name: guid(storage.id, uami.id, 'ba92f5b4-2d11-453d-a403-e96b0029c9fe')
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'ba92f5b4-2d11-453d-a403-e96b0029c9fe')
    principalId: uami.properties.principalId // object ID, not uami.properties.clientId
    principalType: 'ServicePrincipal'
  }
}`
	rec004 = "Replace Owner, Contributor, User Access Administrator and Role Based Access Control Administrator on application identities with the narrow roles the workload needs, such as Foundry User on the project, Foundry Agent Consumer for callers, and the data-plane roles in FND-IDN-002. Keep any unavoidable privileged role at the narrowest scope and add an ABAC condition when the role can assign roles. Give pipelines a separate deployment identity."
	fix004 = `// Instead of Contributor on the resource group, grant the project identity only what the agent needs:
resource projectUser 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: account
  name: guid(account.id, project.id, '53ca6127-db72-4b80-b1b0-d745d6d5456d')
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '53ca6127-db72-4b80-b1b0-d745d6d5456d') // Foundry User
    principalId: project.identity.principalId
    principalType: 'ServicePrincipal'
  }
}`
	rec006 = "Use the exact https issuer of the provider and a subject that names one workload: for GitHub Actions an environment, branch, pull request or tag of one repository, preferably in the immutable subject form; for AKS the exact namespace and service account. Do not put wildcards in a standard credential; if one credential must cover several branches, use a flexible federated identity credential (preview) with an immutable claim, and remove credentials whose workload no longer exists."
	fix006 = `resource fic 'Microsoft.ManagedIdentity/userAssignedIdentities/federatedIdentityCredentials@2024-11-30' = {
  parent: deployIdentity
  name: 'github-prod'
  properties: {
    issuer: 'https://token.actions.githubusercontent.com'
    subject: 'repo:contoso/payments-api:environment:prod' // one repository and one environment
    audiences: [ 'api://AzureADTokenExchange' ]
  }
}`
)
