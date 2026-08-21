// Subscription-scoped: creates the resource group itself, so the entire
// default deployment is one command —
//   az deployment sub create --location <loc> --template-file main.bicep --parameters main.parameters.json
// See infra/README.md for the full walkthrough, cost breakdown, and the
// cheaper (VM) and more-secure (Front Door + WAF, Entra ID SSO) alternatives.
targetScope = 'subscription'

@description('Azure region. eastus is used in infra/README.md\'s cost estimates; other regions may price differently.')
param location string = 'eastus'

@description('Short, unique-ish prefix for resource names (lowercase letters/numbers only, <= 12 chars) — keeps Key Vault/Postgres global-uniqueness requirements manageable without you having to hand-pick every name.')
@minLength(3)
@maxLength(12)
param namePrefix string

@description('Full container image reference, e.g. ghcr.io/YOUR_GH_USERNAME/copilot-web:latest')
param containerImage string

@description('Container registry hostname.')
param registryServer string = 'ghcr.io'

@description('Container registry username (your GitHub username, for ghcr.io).')
param registryUsername string

@description('Container registry password (a GitHub PAT with read:packages, for ghcr.io).')
@secure()
param registryPassword string

@description('GitHub OAuth App client ID (from github.com/settings/developers).')
param githubClientId string

@description('GitHub OAuth App client secret.')
@secure()
param githubClientSecret string

@description('Auth.js session secret — generate with `npx auth secret`.')
@secure()
param authSecret string

@description('32-byte base64 key for encrypting stored GitHub tokens — generate with `openssl rand -base64 32`.')
@secure()
param tokenEncryptionKey string

@description('Postgres admin password — generate with `openssl rand -base64 24`, or let deploy.sh do it for you.')
@secure()
param postgresAdminPassword string

@description('Extra Postgres firewall allow-rules, e.g. your own IP for running `prisma migrate deploy` locally. Each entry: {name, startIp, endIp}.')
param postgresAllowedIpRanges array = []

@description('Container App min/max replicas. Keep min at 1 — see containerApp.bicep\'s param doc for why scaling to 0 breaks auto-mode sessions.')
param minReplicas int = 1
param maxReplicas int = 1

var resourceGroupName = '${namePrefix}-rg'
var logAnalyticsName = '${namePrefix}-logs'
var envName = '${namePrefix}-env'
var containerAppName = '${namePrefix}-app'
var postgresServerName = '${namePrefix}-pg'
var keyVaultName = '${namePrefix}-kv'

resource rg 'Microsoft.Resources/resourceGroups@2024-03-01' = {
  name: resourceGroupName
  location: location
}

module logAnalytics 'modules/logAnalytics.bicep' = {
  name: 'logAnalytics'
  scope: rg
  params: {
    location: location
    name: logAnalyticsName
  }
}

module containerAppsEnv 'modules/containerAppsEnv.bicep' = {
  name: 'containerAppsEnv'
  scope: rg
  params: {
    location: location
    name: envName
    logAnalyticsCustomerId: logAnalytics.outputs.customerId
    logAnalyticsSharedKey: logAnalytics.outputs.sharedKey
  }
}

module postgres 'modules/postgres.bicep' = {
  name: 'postgres'
  scope: rg
  params: {
    location: location
    name: postgresServerName
    adminPassword: postgresAdminPassword
    allowedIpRanges: postgresAllowedIpRanges
  }
}

module keyVault 'modules/keyvault.bicep' = {
  name: 'keyVault'
  scope: rg
  params: {
    location: location
    name: keyVaultName
    githubClientSecret: githubClientSecret
    authSecret: authSecret
    tokenEncryptionKey: tokenEncryptionKey
    postgresAdminPassword: postgresAdminPassword
  }
}

// The Container App's own FQDN is deterministic (name + environment's
// default domain) even though the environment doesn't exist yet at
// template-authoring time, so AUTH_URL can be computed up front instead
// of needing a second deployment pass once the app exists.
var authUrl = 'https://${containerAppName}.${containerAppsEnv.outputs.defaultDomain}'

module containerApp 'modules/containerApp.bicep' = {
  name: 'containerApp'
  scope: rg
  params: {
    location: location
    name: containerAppName
    environmentId: containerAppsEnv.outputs.environmentId
    containerImage: containerImage
    registryServer: registryServer
    registryUsername: registryUsername
    registryPassword: registryPassword
    githubClientSecretUri: keyVault.outputs.githubClientSecretUri
    authSecretUri: keyVault.outputs.authSecretUri
    tokenEncryptionKeyUri: keyVault.outputs.tokenEncryptionKeyUri
    postgresAdminPasswordUri: keyVault.outputs.postgresAdminPasswordUri
    githubClientId: githubClientId
    authUrl: authUrl
    postgresServerFqdn: postgres.outputs.serverFqdn
    postgresDatabaseName: postgres.outputs.databaseName
    postgresAdminUsername: 'copilotadmin'
    minReplicas: minReplicas
    maxReplicas: maxReplicas
  }
}

// Grants the Container App's system-assigned identity read-only access to
// just the four secrets it needs — "Key Vault Secrets User" is a built-in
// role scoped to GetSecret/ListSecret, not manage/delete.
module keyVaultAccess 'modules/keyvaultAccess.bicep' = {
  name: 'keyVaultAccess'
  scope: rg
  params: {
    keyVaultName: keyVaultName
    principalId: containerApp.outputs.principalId
  }
}

output resourceGroupName string = rg.name
output containerAppFqdn string = containerApp.outputs.fqdn
output appUrl string = authUrl
output postgresServerFqdn string = postgres.outputs.serverFqdn
output keyVaultName string = keyVaultName
