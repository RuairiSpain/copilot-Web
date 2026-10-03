// Foundry Hosted Agent Pooling Service on Azure Container Apps.
// Deploy with `azd up`. The Foundry account and project must already exist.
targetScope = 'subscription'

@minLength(1)
@maxLength(64)
@description('Name of the azd environment. Used to derive resource names.')
param environmentName string

@minLength(1)
@description('Azure region for the new resources.')
param location string

@description('Resource group that contains the Foundry account and project.')
param foundryResourceGroupName string

@description('Name of the Foundry (Cognitive Services) account.')
param foundryAccountName string

@description('Name of the Foundry project.')
param foundryProjectName string

@description('Foundry project endpoint. Defaults to the standard form for the account and project.')
param foundryProjectEndpoint string = 'https://${foundryAccountName}.services.ai.azure.com/api/projects/${foundryProjectName}'

@description('Microsoft Entra tenant that issues the caller tokens.')
param entraTenantId string = tenant().tenantId

@description('Accepted token audience, for example api://<client-id>. Comma separate several.')
param entraAudience string

@description('Role definition id granted to the service identity on the Foundry project. Default is Foundry User (formerly Azure AI User). Verify it covers hosted agent session create, list and delete in your tenant; use a broader role if it does not.')
param foundryRoleDefinitionId string = '53ca6127-db72-4b80-b1b0-d745d6d5456d'

@secure()
@description('Secret of at least 32 characters that derives each stateful user session id, so sessions survive a restart. Leave empty to turn recovery off. Never change it on a live system: existing sessions would be orphaned. Stateful agents cannot use min_warm_sessions while it is set.')
param sessionIdKey string = ''

@secure()
@description('Constant x-ms-user-isolation-key sent to Foundry. Needed only for agent endpoints that use the Header authorisation scheme. Leave empty otherwise.')
param foundryIsolationKey string = ''

@description('Container image. Leave empty on first provision; `azd deploy` sets it.')
param image string = ''

var tags = {
  'azd-env-name': environmentName
  workload: 'hosted-agent-kit'
}
var resourceToken = toLower(uniqueString(subscription().id, environmentName, location))

resource rg 'Microsoft.Resources/resourceGroups@2024-03-01' = {
  name: 'rg-${environmentName}'
  location: location
  tags: tags
}

module resources 'resources.bicep' = {
  scope: rg
  name: 'resources'
  params: {
    location: location
    tags: tags
    resourceToken: resourceToken
    foundryProjectEndpoint: foundryProjectEndpoint
    entraTenantId: entraTenantId
    entraAudience: entraAudience
    sessionIdKey: sessionIdKey
    foundryIsolationKey: foundryIsolationKey
    image: image
  }
}

module foundryAccess 'foundry-access.bicep' = {
  scope: resourceGroup(foundryResourceGroupName)
  name: 'foundry-access'
  params: {
    accountName: foundryAccountName
    projectName: foundryProjectName
    principalId: resources.outputs.identityPrincipalId
    roleDefinitionId: foundryRoleDefinitionId
  }
}

output AZURE_LOCATION string = location
output AZURE_RESOURCE_GROUP string = rg.name
output AZURE_CONTAINER_REGISTRY_ENDPOINT string = resources.outputs.registryLoginServer
output SERVICE_API_URI string = resources.outputs.appUrl
output SERVICE_API_IDENTITY_PRINCIPAL_ID string = resources.outputs.identityPrincipalId
