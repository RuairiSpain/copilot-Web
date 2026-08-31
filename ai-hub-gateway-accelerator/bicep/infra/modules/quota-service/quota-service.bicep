/**
 * @module modules/quota-service
 * @description Provisions the quota-service Function App (Node 20, Linux
 *              Consumption) that backs the quota-override/approval
 *              mechanism: getQuotaAllowance (HTTP, called from the APIM
 *              policy fragment on a cache miss), submitQuotaRequest,
 *              decideQuotaRequest, listPendingQuotaRequests (HTTP), and
 *              expireQuotaOverrides (daily timer). See
 *              src/quota-service/README.md and
 *              guides/quota-override-approval.md for the full design.
 *
 *              Modeled directly on modules/pricing-service/pricing-service.bicep
 *              — same SKU, same managed-identity-only auth posture, same
 *              "deployable standalone, wiring into your main orchestration
 *              is left to you" scope.
 */

@description('Name for the Function App (must be globally unique).')
param functionAppName string

param location string = resourceGroup().location

param tags object = {}

@description('Existing storage account name used for the Functions runtime (AzureWebJobsStorage).')
param storageAccountName string

@description('Existing Cosmos DB account NAME (not just the endpoint — needed to grant this Function App'
  + ' data-plane RBAC via the existing modules/cosmos-db/cosmos-sql-role-assignment.bicep module).')
param cosmosDbAccountName string

@description('Existing Cosmos DB account endpoint (from the cosmos-db module output cosmosDbEndpoint).')
param cosmosDbEndpoint string

@description('Existing Cosmos DB database name (from the cosmos-db module output cosmosDbDatabaseName).')
param cosmosDbDatabaseName string

@description('Existing Cosmos DB quota-overrides container name (from the cosmos-db module output cosmosDbQuotaOverridesContainerName).')
param cosmosDbQuotaOverridesContainerName string

@description('Existing Cosmos DB quota-override-requests container name (from the cosmos-db module output cosmosDbQuotaOverrideRequestsContainerName).')
param cosmosDbQuotaOverrideRequestsContainerName string

@description('Default duration, in days, for a temporary quota override when the requester does not specify one. See guides/quota-override-approval.md §7.')
param defaultDurationDays int = 30

@description('An override request past this multiple of the scope\'s current quota requires a second-level approver. See guides/quota-override-approval.md §7.')
param escalationMultiplier int = 3

@description('If true, resetMonthlyQuotaOverrides also clears PERMANENT overrides (expiresAt: null) at the monthly boundary, not just temporary ones. Default false — a permanent grant was a deliberate approval decision; see quotaLogic.ts\'s survivesMonthlyReset() for the reasoning.')
param monthlyResetIncludesPermanent bool = false

@description('Application Insights connection string, if you want the Function App emitting to the same workspace as the rest of the gateway.')
param applicationInsightsConnectionString string = ''

@description('SMTP host for sending request-created/decided notification emails (guides/quota-override-approval.md §6) — e.g. smtp.office365.com, or your own relay/SendGrid SMTP endpoint.')
param smtpHost string = ''

@description('SMTP port. 587 (STARTTLS) is the common default; 465 (implicit TLS) is also supported.')
param smtpPort int = 587

@description('SMTP auth username.')
param smtpUser string = ''

@description('Full Key Vault secret URI (e.g. https://your-kv.vault.azure.net/secrets/smtp-password/) for the SMTP password — resolved as an @Microsoft.KeyVault(...) app setting reference, never stored as a plaintext value. The Function App\'s managed identity needs "Get" permission on this secret (Key Vault access policy or the Key Vault Secrets User RBAC role) — grant it as part of deploying this module, same prerequisite as any other Key Vault reference app setting in Azure.')
param smtpPasswordKeyVaultSecretUri string = ''

@description('The "From" address on outbound notification emails.')
param smtpFromAddress string = ''

@description('Your Entra ID tenant ID — used by submitQuotaRequest/decideQuotaRequest to independently re-validate the caller\'s bearer token (defense-in-depth on top of the Quota Override API\'s own JWT gate — see quota-api-policy.xml and guides/quota-override-approval.md\'s "Impersonation" section).')
param entraTenantId string = ''

@description('The audience (App ID URI) the gateway\'s own Entra app registration expects — same value as the APIM JWT-AppRegistrationId named value.')
param entraAudience string = ''

@description('Whether submitQuotaRequest/decideQuotaRequest independently re-validate the caller\'s bearer token (defense-in-depth against the x-verified-oid header being spoofable by anyone holding the function key — see requestAuth.ts). Default true = secure by default. Set false ONLY for local development without a real Entra tenant to validate against; doing so in a real deployment silently reopens the impersonation gap this exists to close.')
param requireTokenRevalidation bool = true

@description('Resource ID of an existing subnet (delegated to Microsoft.Web/serverFarms — see guides/network-approach.md\'s "Logic App Subnet") to VNet-integrate this Function App into. Empty (the default) deploys with no VNet integration, unchanged from before this param existed. Required for either of the accelerator\'s private-networking topologies — without it, this is the only compute in the platform (alongside pricing-service) still reachable/reaching out over the public network. See guides/enterprise-hardening-checklist.md §2.')
param functionAppSubnetId string = ''

var vnetIntegrated = !empty(functionAppSubnetId)

resource storageAccount 'Microsoft.Storage/storageAccounts@2023-01-01' existing = {
  name: storageAccountName
}

resource hostingPlan 'Microsoft.Web/serverfarms@2023-01-01' = {
  name: '${functionAppName}-plan'
  location: location
  tags: tags
  // EP1/ElasticPremium by default — Y1/Consumption does not reliably
  // support regional VNet integration on Linux, and this is already
  // what this module's own prior comment recommended. Same precedent as
  // bicep/infra/modules/functionapp/functionapp.bicep (an existing,
  // working VNet-integrated Function App in this repo).
  sku: {
    name: 'EP1'
    tier: 'ElasticPremium'
    family: 'EP'
  }
  properties: {
    reserved: true // Linux
    maximumElasticWorkerCount: 10
  }
}

resource functionApp 'Microsoft.Web/sites@2023-01-01' = {
  name: functionAppName
  location: location
  tags: tags
  kind: 'functionapp,linux'
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    serverFarmId: hostingPlan.id
    httpsOnly: true
    virtualNetworkSubnetId: vnetIntegrated ? functionAppSubnetId : null
    siteConfig: {
      linuxFxVersion: 'Node|20'
      vnetRouteAllEnabled: vnetIntegrated
      functionsRuntimeScaleMonitoringEnabled: vnetIntegrated
      minimumElasticInstanceCount: vnetIntegrated ? 1 : 0
      appSettings: concat([
        { name: 'AzureWebJobsStorage__accountName', value: storageAccount.name }
        { name: 'FUNCTIONS_EXTENSION_VERSION', value: '~4' }
        { name: 'FUNCTIONS_WORKER_RUNTIME', value: 'node' }
        { name: 'WEBSITE_NODE_DEFAULT_VERSION', value: '~20' }
        { name: 'CosmosDB_Endpoint', value: cosmosDbEndpoint }
        { name: 'CosmosDB_Database', value: cosmosDbDatabaseName }
        { name: 'CosmosDB_QuotaOverridesContainer', value: cosmosDbQuotaOverridesContainerName }
        { name: 'CosmosDB_QuotaOverrideRequestsContainer', value: cosmosDbQuotaOverrideRequestsContainerName }
        { name: 'QuotaOverride_DefaultDurationDays', value: string(defaultDurationDays) }
        { name: 'QuotaOverride_EscalationMultiplier', value: string(escalationMultiplier) }
        { name: 'QuotaOverride_MonthlyResetIncludesPermanent', value: string(monthlyResetIncludesPermanent) }
        { name: 'Smtp_Host', value: smtpHost }
        { name: 'Smtp_Port', value: string(smtpPort) }
        { name: 'Smtp_User', value: smtpUser }
        {
          name: 'Smtp_Password'
          // Key Vault reference — resolved by the platform at runtime, never
          // a plaintext secret in this app setting or in source control.
          // Empty smtpPasswordKeyVaultSecretUri (the default) leaves this
          // blank, which src/lib/email.ts already handles by throwing a
          // clear error only when an email actually needs to be sent —
          // deploying without email notifications configured doesn't fail.
          value: empty(smtpPasswordKeyVaultSecretUri) ? '' : '@Microsoft.KeyVault(SecretUri=${smtpPasswordKeyVaultSecretUri})'
        }
        { name: 'Smtp_FromAddress', value: smtpFromAddress }
        { name: 'Entra_TenantId', value: entraTenantId }
        { name: 'Entra_Audience', value: entraAudience }
        { name: 'QuotaOverride_RequireTokenRevalidation', value: string(requireTokenRevalidation) }
        {
          name: 'Entra_OpenIdConfigUrl'
          value: empty(entraTenantId) ? '' : 'https://login.microsoftonline.com/${entraTenantId}/v2.0/.well-known/openid-configuration'
        }
        {
          name: 'APPLICATIONINSIGHTS_CONNECTION_STRING'
          value: applicationInsightsConnectionString
        }
      ], vnetIntegrated ? [
        { name: 'WEBSITE_VNET_ROUTE_ALL', value: '1' }
        { name: 'WEBSITE_CONTENTOVERVNET', value: '1' }
      ] : [])
    }
  }
}

// Swift-connects the Function App into the subnet — see the identical
// comment in pricing-service.bicep, same precedent
// (bicep/infra/modules/functionapp/functionapp.bicep).
resource networkConfig 'Microsoft.Web/sites/networkConfig@2023-01-01' = if (vnetIntegrated) {
  parent: functionApp
  name: 'virtualNetwork'
  properties: {
    subnetResourceId: functionAppSubnetId
    swiftSupported: true
  }
}

// Data-plane RBAC via the accelerator's own existing reusable module
// (bicep/infra/modules/cosmos-db/cosmos-sql-role-assignment.bicep) rather
// than a new custom role assignment — same "Cosmos DB Built-in Data
// Contributor" role pricing-service's own comment already points at as
// the established precedent (bicep/infra/apim-gateway-upgrade/services/logic-app.bicep),
// now actually reused instead of just cited.
//
// NOTE: this grants account-wide data-plane access, not scoped down to
// just the two quota-* containers — matching every existing use of this
// module in this accelerator (its own Logic Apps are granted the same
// account-wide scope). Cosmos SQL role assignments DO support narrowing
// `scope` to a specific database/container path, which would be a
// genuinely tighter grant for this Function App (it only ever touches
// two containers) — flagged as a real hardening opportunity in
// guides/enterprise-hardening-checklist.md rather than attempted here
// unverified, since no existing pattern in this repo demonstrates the
// narrower scope string actually working end-to-end.
//
// Also per that module's own comment: deploy this in a separate stage
// from functionApp's own creation so the new managed identity has time
// to replicate in Entra ID first (Cosmos validates the principal
// synchronously and does not accept a principalType hint) — same
// ordering constraint as every other identity this accelerator grants
// Cosmos access to.
module cosmosRoleAssignment '../cosmos-db/cosmos-sql-role-assignment.bicep' = {
  name: '${functionAppName}-cosmos-rbac'
  params: {
    cosmosDbAccountName: cosmosDbAccountName
    principalId: functionApp.identity.principalId
  }
}

output functionAppName string = functionApp.name
output functionAppPrincipalId string = functionApp.identity.principalId
output functionAppDefaultHostName string = functionApp.properties.defaultHostName
