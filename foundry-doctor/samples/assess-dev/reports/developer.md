# Foundry Doctor WAF developer detail

> Detailed control evidence for remediation. This report is scoped to available evidence and does not claim complete WAF compliance.

- Profile: `foundry-dev`
- Decision: Proceed with conditions\: mandatory controls need remediation or manual review\.
- Assessed scope: azure\.yaml and compiled infrastructure evidence where available

## Security

### No raw secrets in YAML/Bicep/environment blocks — PASS

- Rule: `FND-CFG-005` (version 4)
- Mandatory: true
- Evidence basis: deterministic
- Why this state: Evaluated with no remaining finding and no skipped check\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference> (verified `2026-10-05`)
- Recommendation: Store the secret in the azd environment as a secret \(azd env set\-secret\) or in Key Vault, and reference it with \$\{VAR\} in azure\.yaml\. In Bicep, mark secret parameters \@secure\(\) with no default and never output them\. Prefer Entra ID or managed identity authentication where the target supports it\.
- Fix:

  ```
  services:
    github-conn:
      host: azure.ai.connection
      uses: [ai-project]
      category: RemoteTool
      target: https://api.githubcopilot.com/mcp
      authType: CustomKeys
      credentials:
        Authorization: ${GITHUB_PAT}   # set with: azd env set-secret GITHUB_PAT
  ```

- Evidence: no finding or skip record was present.

### Outbound endpoints use HTTPS and reject unsafe local/metadata targets — PASS

- Rule: `FND-CFG-007` (version 2)
- Mandatory: true
- Evidence basis: deterministic
- Why this state: Evaluated with no remaining finding and no skipped check\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/agents/how-to/tools/model-context-protocol> (verified `2026-10-04`)
- Recommendation: Use https endpoints reachable from the service that calls them\. For internal servers, publish them on a private endpoint or an internal Container Apps ingress and use their private DNS name over https instead of an IP literal\. Remove localhost and metadata addresses from deployed configuration\.
- Fix:

  ```
  services:
    github-conn:
      host: azure.ai.connection
      category: RemoteTool
      target: https://api.githubcopilot.com/mcp
  ```

- Evidence: no finding or skip record was present.

### Deployer can create resources and role assignments — SKIPPED

- Rule: `FND-DEP-003` (version 2)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/authorization/resource-manager/Microsoft.Authorization/Authorization/stable/2022-04-01/authorization-RoleDefinitionsCalls.json> (verified `2026-10-04`)
- Recommendation: Assign the deployer a role that includes the missing write actions at the narrowest sufficient scope\. For templates that create role assignments, Owner or User Access Administrator \(or a custom role with Microsoft\.Authorization/roleAssignments/write\) is needed in addition to a resource\-write role\.
- Fix:

  ```
  az role assignment create --assignee <deployer-object-id> --role "Contributor" --scope /subscriptions/<sub-id>/resourceGroups/<rg>
  az role assignment create --assignee <deployer-object-id> --role "Role Based Access Control Administrator" --scope /subscriptions/<sub-id>/resourceGroups/<rg>
  ```

- Evidence:
  - skipped: not\-implemented

### Managed identities used for Foundry and workloads — SKIPPED

- Rule: `FND-IDN-001` (version 3)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/standard-agent-setup> (verified `2026-10-04`)
- Recommendation: Enable a system\-assigned or user\-assigned managed identity on the Foundry account, on each project and on every workload that calls Foundry, then grant that identity data\-plane roles on the dependent resources \(see FND\-IDN\-002\)\. Choose the user\-assigned option at project creation if you need it, because the project identity type cannot be changed later\.
- Fix:

  ```
  resource account 'Microsoft.CognitiveServices/accounts@2026-09-01' = {
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
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Project identity has required data roles — SKIPPED

- Rule: `FND-IDN-002` (version 1)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/standard-agent-setup> (verified `2026-10-04`)
- Recommendation: Assign the project managed identity the documented data roles on each store before agents run\: Search Index Data Contributor and Search Service Contributor on Search, Storage Blob Data Contributor and Storage Blob Data Owner on the storage containers, Cosmos DB Operator on the account plus the Cosmos DB Built\-in Data Contributor SQL role on the database\. Assign them at the narrowest scope that works\. These roles are separate from the roles the deploying identity needs\.
- Fix:

  ```
  resource searchIndexData 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
    scope: search
    name: guid(search.id, project.id, '8ebe5a00-799e-43f5-93ac-243d3dce84a7')
    properties: {
      roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '8ebe5a00-799e-43f5-93ac-243d3dce84a7') // Search Index Data Contributor
      principalId: project.identity.principalId
      principalType: 'ServicePrincipal'
    }
  }
  ```

- Evidence:
  - skipped: not\-implemented

### No broad Owner/Contributor for application identities — SKIPPED

- Rule: `FND-IDN-004` (version 6)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/role-based-access-control/role-assignments-steps> (verified `2026-10-04`)
- Recommendation: Replace Owner, Contributor, User Access Administrator and Role Based Access Control Administrator on application identities with the narrow roles the workload needs, such as Foundry User on the project, Foundry Agent Consumer for callers, and the data\-plane roles in FND\-IDN\-002\. Keep any unavoidable privileged role at the narrowest scope and add an ABAC condition when the role can assign roles\. Give pipelines a separate deployment identity\.
- Fix:

  ```
  // Instead of Contributor on the resource group, grant the project identity only what the agent needs:
  resource projectUser 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
    scope: account
    name: guid(account.id, project.id, '53ca6127-db72-4b80-b1b0-d745d6d5456d')
    properties: {
      roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '53ca6127-db72-4b80-b1b0-d745d6d5456d') // Foundry User
      principalId: project.identity.principalId
      principalType: 'ServicePrincipal'
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Standing human admin access reviewed — SKIPPED

- Rule: `FND-IDN-005` (version 2)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/role-based-access-control/best-practices> (verified `2026-10-04`)
- Review question: Has the team reviewed standing human admin access with current business and support context?
- Recommendation: Review each listed assignment\. Prefer a job function role over a privileged administrator role, assign roles to groups, use the narrowest scope, and make human admin access eligible or time\-bound through PIM where licensed\. Keep between two and three subscription owners\.
- Fix:

  ```
  // Make an admin group eligible through PIM instead of a permanent assignment (portal, CLI or REST):
  // Access control (IAM) > Add role assignment > Assignment type: Eligible
  // Time-bound active assignment through a role assignment schedule request is also acceptable.
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Federated credentials use HTTPS issuer and specific subject — SKIPPED

- Rule: `FND-IDN-006` (version 1)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://openid.net/specs/openid-connect-discovery-1_0.html#IssuerDiscovery> (verified `2026-10-04`)
- Recommendation: Use the exact https issuer of the provider and a subject that names one workload\: for GitHub Actions an environment, branch, pull request or tag of one repository, preferably in the immutable subject form; for AKS the exact namespace and service account\. Do not put wildcards in a standard credential; if one credential must cover several branches, use a flexible federated identity credential \(preview\) with an immutable claim, and remove credentials whose workload no longer exists\.
- Fix:

  ```
  resource fic 'Microsoft.ManagedIdentity/userAssignedIdentities/federatedIdentityCredentials@2024-11-30' = {
    parent: deployIdentity
    name: 'github-prod'
    properties: {
      issuer: 'https://token.actions.githubusercontent.com'
      subject: 'repo:contoso/payments-api:environment:prod' // one repository and one environment
      audiences: [ 'api://AzureADTokenExchange' ]
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Foundry public access/private endpoint posture — SKIPPED

- Rule: `FND-NET-001` (version 4)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/networking-options> (verified `2026-10-04`)
- Recommendation: For test and production set publicNetworkAccess to Disabled and add a private endpoint with group ID account in the VNet that hosts your clients and agents, with its private DNS zones \(see FND\-NET\-003\)\. If you need limited public access, use restricted mode \(defaultAction Deny plus an explicit allow list\)\. Public access is acceptable for dev prototyping\.
- Fix:

  ```
  resource account 'Microsoft.CognitiveServices/accounts@2026-09-01' = {
    name: accountName
    location: location
    kind: 'AIServices'
    sku: { name: 'S0' }
    identity: { type: 'SystemAssigned' }
    properties: {
      customSubDomainName: accountName
      publicNetworkAccess: 'Disabled'
      networkAcls: { defaultAction: 'Deny' }
    }
  }
  resource pe 'Microsoft.Network/privateEndpoints@2024-05-01' = {
    name: '${accountName}-pe'
    location: location
    properties: {
      subnet: { id: peSubnetId }
      privateLinkServiceConnections: [{
        name: 'account'
        properties: { privateLinkServiceId: account.id, groupIds: ['account'] }
      }]
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Dependent services public access/default\-deny/private endpoints — SKIPPED

- Rule: `FND-NET-002` (version 3)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks> (verified `2026-10-04`)
- Recommendation: Disable public network access on the Storage, Cosmos DB and AI Search resources that back the agents and give each a private endpoint in the private endpoint subnet, with its private DNS zone \(FND\-NET\-003\)\. Do not rely on the Foundry template to create them; the Foundry documentation states it does not\.
- Fix:

  ```
  resource search 'Microsoft.Search/searchServices@2025-05-01' = {
    name: searchName
    location: location
    sku: { name: 'standard' }
    properties: { publicNetworkAccess: 'Disabled' }
  }
  resource searchPe 'Microsoft.Network/privateEndpoints@2024-05-01' = {
    name: '${searchName}-pe'
    location: location
    properties: {
      subnet: { id: peSubnetId }
      privateLinkServiceConnections: [{
        name: 'search'
        properties: { privateLinkServiceId: search.id, groupIds: ['searchService'] }
      }]
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Restricted mode has allowed IP range — SKIPPED

- Rule: `FND-NET-007` (version 2)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/how-to/configure-private-link> (verified `2026-10-04`)
- Recommendation: If you want restricted mode, list the egress IPs of your build agents and developer networks as ipRules \(or subnets as virtualNetworkRules\)\. If you want fully private access, set publicNetworkAccess to Disabled and use a private endpoint instead\.
- Fix:

  ```
  properties: {
    publicNetworkAccess: 'Enabled'
    networkAcls: {
      defaultAction: 'Deny'
      bypass: 'AzureServices'
      ipRules: [{ value: '203.0.113.0/24' }]
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Azure Monitor private\-link choice is explicit — SKIPPED

- Rule: `FND-NET-010` (version 3)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: An Azure Monitor Private Link Scope is reached through a private endpoint, and Azure Monitor refers its cost to Azure Private Link pricing\. (<https://learn.microsoft.com/azure/azure-monitor/fundamentals/private-link-security>, verified `2026-10-18`)
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/azure-monitor/fundamentals/private-link-design> (verified `2026-10-04`)
- Recommendation: Decide the telemetry path\. For private telemetry, create one AMPLS per DNS boundary, add the Application Insights component and its workspace as scoped resources, create the private endpoint with group ID azuremonitor and link the Azure Monitor private DNS zones, and set both access modes explicitly\. Use Open while resources are still being added and switch to Private Only only when all Azure Monitor resources are in the scope\. If you accept public telemetry, record that decision in the Foundry Doctor config so the finding is suppressed with a reason\.
- Fix:

  ```
  resource ampls 'Microsoft.Insights/privateLinkScopes@2021-09-01' = {
    name: 'ampls-${name}'
    location: 'global'
    properties: {
      accessModeSettings: { ingestionAccessMode: 'Open', queryAccessMode: 'Open' } // explicit; move to PrivateOnly once every resource is scoped
    }
  }
  resource scoped 'Microsoft.Insights/privateLinkScopes/scopedResources@2021-09-01' = {
    parent: ampls
    name: 'appi'
    properties: { linkedResourceId: appInsights.id }
  }
  // private endpoint with groupIds: ['azuremonitor'] and privateLinkServiceId: ampls.id
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### NSG/DDoS recommendations — SKIPPED

- Rule: `FND-NET-011` (version 3)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: DDoS Network Protection and DDoS IP Protection are priced per protected IP or per 100 protected IPs; only DDoS infrastructure protection is at no additional cost\. (<https://learn.microsoft.com/azure/ddos-protection/ddos-protection-sku-comparison>, verified `2026-10-18`)
- Source: <https://azure.github.io/PSRule.Rules.Azure/en/rules/Azure.VNET.UseNSGs/> (verified `2026-10-04`)
- Recommendation: Associate an NSG with each subnet that supports one and review inbound allow rules\. If the design exposes public IP addresses, enable Azure DDoS Protection \(IP Protection on the public IP, or Network Protection on the VNet with a plan\)\. For a fully private design, rely on the default infrastructure protection and do not buy a plan for it\.
- Fix:

  ```
  resource nsg 'Microsoft.Network/networkSecurityGroups@2024-05-01' = { name: 'nsg-pe', location: location, properties: { securityRules: [] } }
  resource vnet 'Microsoft.Network/virtualNetworks@2024-05-01' = {
    name: vnetName
    location: location
    properties: {
      addressSpace: { addressPrefixes: ['10.0.0.0/16'] }
      subnets: [ { name: 'pe', properties: { addressPrefix: '10.0.1.0/24', networkSecurityGroup: { id: nsg.id } } } ]
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Data\-residency regions and deployment SKU — SKIPPED

- Rule: `FND-OPS-009` (version 2)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: profile\-key\-missing\:dataResidency\.scope\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/foundry-models/concepts/deployment-types> (verified `2026-10-04`)
- Recommendation: Choose a deployment SKU that matches the residency requirement \- Data Zone Standard or Data Zone Provisioned for a data zone, Standard or Regional Provisioned for a geography \- in a region that belongs to that zone or geography, and keep the region list in the profile current\.
- Fix:

  ```
  # azure.yaml
  services:
    ai-project:
      host: azure.ai.project
      deployments:
        - name: chat
          model: { format: OpenAI, name: gpt-5.4-mini, version: "2026-03-17" }
          sku: { name: DataZoneStandard, capacity: 10 }
  ```

- Evidence:
  - skipped: profile\-key\-missing\:dataResidency\.scope

### Model allow/deny lists remain consistent — SKIPPED

- Rule: `FND-OPS-010` (version 2)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: profile\-key\-missing\:models\.allow\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/how-to/model-deployment-policy> (verified `2026-10-04`)
- Recommendation: Keep one source of truth for approved models, mirror it in the policy assignment parameters allowedPublishers and allowedAssetIds, and remove any model from azure\.yaml that the lists exclude\. Assign the policy with Audit first and move to Deny after the compliance dashboard is clean\.
- Fix:

  ```
  # profile excerpt (allow list) - mirror the same values in the policy assignment parameters
  models:
    allow:
      publishers: ["OpenAI"]
    deny: []
  ```

- Evidence:
  - skipped: profile\-key\-missing\:models\.allow

### Effective project identity access — SKIPPED

- Rule: `FND-RUN-001` (version 4)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/standard-agent-setup> (verified `2026-10-04`)
- Recommendation: Assign the project managed identity the roles the Foundry standard agent setup lists on each bound resource, at the narrowest scope that contains the resources\. Assign Cosmos DB Built\-in Data Contributor with a Cosmos SQL role assignment, not the Access control \(IAM\) blade\.
- Fix:

  ```
  resource searchIndexData 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
    name: guid(search.id, project.id, '8ebe5a00-799e-43f5-93ac-243d3dce84a7')
    scope: search
    properties: {
      principalId: project.identity.principalId
      principalType: 'ServicePrincipal'
      roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '8ebe5a00-799e-43f5-93ac-243d3dce84a7') // Search Index Data Contributor
    }
  }
  ```

- Evidence:
  - skipped: not\-implemented

### Foundry local authentication disabled — SKIPPED

- Rule: `FND-SEC-001` (version 3)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json> (verified `2026-10-04`)
- Recommendation: Set properties\.disableLocalAuth to true on the Foundry account and grant callers Entra ID roles instead of distributing keys\. Keep keys enabled only in dev with a recorded suppression \(reason and expiry\)\.
- Fix:

  ```
  resource account 'Microsoft.CognitiveServices/accounts@2026-09-01' = {
    name: accountName
    location: location
    kind: 'AIServices'
    sku: { name: 'S0' }
    properties: {
      customSubDomainName: accountName
      disableLocalAuth: true
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### AI Search key authentication disabled or RBAC\-only — SKIPPED

- Rule: `FND-SEC-002` (version 3)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json> (verified `2026-10-04`)
- Recommendation: Set disableLocalAuth to true and remove authOptions, then assign Entra roles \(for example index data roles\) to the Foundry project identity\. If a migration needs keys temporarily, use authOptions\.aadOrApiKey with aadAuthFailureMode and track removal with a suppression expiry\.
- Fix:

  ```
  resource search 'Microsoft.Search/searchServices@2025-05-01' = {
    name: searchName
    location: location
    sku: { name: 'standard' }
    properties: {
      disableLocalAuth: true
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Cosmos DB local authentication disabled — SKIPPED

- Rule: `FND-SEC-003` (version 3)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cosmos-db/resource-manager/Microsoft.DocumentDB/DocumentDB/stable/2026-03-15/openapi.json> (verified `2026-10-04`)
- Recommendation: Set properties\.disableLocalAuth to true and grant the Foundry project managed identity a Cosmos DB data\-plane role assignment\. Check that clients use Entra ID before enforcing in an existing environment\.
- Fix:

  ```
  resource cosmos 'Microsoft.DocumentDB/databaseAccounts@2026-03-15' = {
    name: cosmosName
    location: location
    kind: 'GlobalDocumentDB'
    properties: {
      databaseAccountOfferType: 'Standard'
      locations: [ { locationName: location, failoverPriority: 0 } ]
      disableLocalAuth: true
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Storage shared key/anonymous access disabled; HTTPS/TLS required — SKIPPED

- Rule: `FND-SEC-004` (version 2)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/storage/resource-manager/Microsoft.Storage/stable/2026-06-01/openapi.json> (verified `2026-10-04`)
- Recommendation: Set allowSharedKeyAccess false, allowBlobPublicAccess false, supportsHttpsTrafficOnly true and minimumTlsVersion TLS1\_2 on the account, and give the Foundry identity Storage data roles\. Verify no client still uses account keys or SAS signed with the account key before disabling Shared Key\.
- Fix:

  ```
  resource st 'Microsoft.Storage/storageAccounts@2026-06-01' = {
    name: storageName
    location: location
    sku: { name: 'Standard_LRS' }
    kind: 'StorageV2'
    properties: {
      allowSharedKeyAccess: false
      allowBlobPublicAccess: false
      supportsHttpsTrafficOnly: true
      minimumTlsVersion: 'TLS1_2'
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Firewall rules reject private/open ranges — SKIPPED

- Rule: `FND-SEC-006` (version 3)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/storage/common/storage-network-security-limitations> (verified `2026-10-04`)
- Recommendation: Remove open or private ranges from IP rules\. Use private endpoints, virtual network rules or service endpoints for private traffic, and list only the specific public egress addresses that need access\.
- Fix:

  ```
  networkAcls: {
    defaultAction: 'Deny'
    ipRules: [
      { value: '203.0.113.10' } // specific public address, not 0.0.0.0/0 or 10.0.0.0/8
    ]
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Relevant Defender plans enabled — SKIPPED

- Rule: `FND-SEC-008` (version 2)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: Defender for Cloud plans beyond foundational CSPM are paid plans; charges begin after the 30\-day trial or usage limit\. (<https://learn.microsoft.com/azure/defender-for-cloud/connect-azure-subscription>, verified `2026-10-18`)
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/security/resource-manager/Microsoft.Security/Security/stable/2026-07-01/pricings.json> (verified `2026-10-04`)
- Recommendation: Enable the Defender plan for each resource type in use on the subscription \(Standard tier\), or confirm it is enforced at a parent scope\. Enabling Defender plans has a cost; review pricing before enabling in non\-production\.
- Fix:

  ```
  resource defenderAi 'Microsoft.Security/pricings@2026-07-01' = {
    name: 'AI'
    properties: { pricingTier: 'Standard' }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Policy coverage and compliance evidence — SKIPPED

- Rule: `FND-SEC-009` (version 2)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Authorization/policy/stable/2025-01-01/policyAssignments.json> (verified `2026-10-04`)
- Recommendation: Assign the built\-in policies that correspond to the Foundry Doctor security rules \(for example local\-auth, public network access, CMK\) at subscription or management\-group scope, in Default enforcement mode once existing resources are compliant\.
- Fix:

  ```
  resource assign 'Microsoft.Authorization/policyAssignments@2025-01-01' = {
    name: 'foundry-no-local-auth'
    properties: {
      policyDefinitionId: tenantResourceId('Microsoft.Authorization/policyDefinitions', '71ef260a-8f18-47b7-abcb-62d0673d94dc')
      enforcementMode: 'Default'
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Customer\-managed keys informational — SKIPPED

- Rule: `FND-SEC-010` (version 2)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: Customer\-managed keys require a billable Azure Key Vault \(stated for Azure AI Search; the same Key Vault dependency applies to each service listed\)\. (<https://learn.microsoft.com/azure/search/search-sku-manage-costs>, verified `2026-10-18`)
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json> (verified `2026-10-04`)
- Recommendation: If your compliance regime requires customer\-managed keys, configure them on each resource listed and use a Key Vault with soft delete and purge protection \(see FND\-SEC\-005\)\. Otherwise no action is needed\.
- Fix:

  ```
  // Informational. Example for a Search service that enforces CMK compliance reporting:
  properties: {
    encryptionWithCmk: { enforcement: 'Enabled' }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Controlled outbound egress informational — SKIPPED

- Rule: `FND-SEC-011` (version 1)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: unsupported\-version\.
- Costly recommendation: FQDN outbound rules in Allow Only Approved Outbound mode create a managed Azure Firewall, which adds Azure Firewall charges\. (<https://learn.microsoft.com/azure/foundry/how-to/managed-virtual-network>, verified `2026-10-18`)
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json> (verified `2026-10-04`)
- Recommendation: If you need to control data exfiltration, set the managed network to AllowOnlyApprovedOutbound or restrict outbound access with an allowed FQDN list, and confirm required dependencies are reachable\. Otherwise record the decision\.
- Fix:

  ```
  # azure.yaml (azure.ai.project service)
  network:
    peSubnet:
      vnet: ${VNET_RESOURCE_ID}
      name: private-endpoint-subnet
    isolationMode: AllowOnlyApprovedOutbound
  ```

- Evidence:
  - skipped: unsupported\-version

### Secrets not emitted as Bicep outputs — SKIPPED

- Rule: `FND-SEC-014` (version 2)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: unsupported\-version\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/azure-resource-manager/bicep/linter-rule-outputs-should-not-contain-secrets> (verified `2026-10-16`)
- Recommendation: Output the resourceId of the resource that holds the secret, or store the secret in Key Vault, and read it at the consuming resource\. Do not output keys, connection strings or passwords\.
- Fix:

  ```
  output storageId string = stg.id // consumer calls listKeys(storageId, ...) when it needs the key; do not output keys
  ```

- Evidence:
  - skipped: unsupported\-version

## Reliability

### All inter\-service references resolve — PASS

- Rule: `FND-CFG-003` (version 4)
- Mandatory: true
- Evidence basis: deterministic
- Why this state: Evaluated with no remaining finding and no skipped check\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/how-to/author-azure-yaml> (verified `2026-10-04`)
- Recommendation: Make each referenced name match a service key defined under services, and add toolbox and connection services to the consuming agent uses list as well as its toolboxes or connections list\.
- Fix:

  ```
  # Partial fragment: category, target and authentication fields for the
  # connection, plus toolbox-specific required fields, depend on the selected
  # documented tool. Merge this graph into a schema-valid manifest.
  services:
    ai-project:
      host: azure.ai.project
    github-conn:
      host: azure.ai.connection
      uses: [ai-project]
    agent-tools:
      host: azure.ai.toolbox
      uses: [ai-project, github-conn]
      tools:
        - type: mcp
          connection: github-conn
    my-agent:
      host: azure.ai.agent
      project: src/my-agent
      uses: [ai-project, github-conn, agent-tools]
      kind: hosted
      toolboxes: [agent-tools]
  ```

- Evidence: no finding or skip record was present.

### Prompt model or hosted source/image is valid — SKIPPED

- Rule: `FND-CFG-004` (version 4)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: uncertain\: assistant\: no Dockerfile under \"\./agents/assistant\" and no image, codeConfiguration, runtime, startupCommand or skill; the agent source is unclear\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference> (verified `2026-10-05`)
- Recommendation: Set model to the name of a deployment declared on the project, provide instructions for prompt agents, and for hosted agents provide either source with a Dockerfile, a codeConfiguration, or a prebuilt image\.
- Fix:

  ```
  services:
    ai-project:
      host: azure.ai.project
      deployments:
        - name: gpt-5.4-mini
          model: {format: OpenAI, name: gpt-5.4-mini, version: "2026-03-17"}
          sku: {name: GlobalStandard, capacity: 10}
    helper:
      host: azure.ai.agent
      project: src/helper
      uses: [ai-project]
      kind: prompt
      name: helper
      model: gpt-5.4-mini
      instructions: Answer briefly.
  ```

- Evidence:
  - skipped: uncertain\: assistant\: no Dockerfile under \"\./agents/assistant\" and no image, codeConfiguration, runtime, startupCommand or skill; the agent source is unclear

### Azure region values are valid — SKIPPED

- Rule: `FND-CFG-012` (version 4)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: no\-environment\-selected\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/subscriptions/stable/2022-12-01/subscriptions.json> (verified `2026-10-04`)
- Recommendation: Use a location name returned by az account list\-locations for your subscription \(the name field, such as eastus2\), and set it with azd env set AZURE\_LOCATION\. Check model and resource availability for the region before provisioning\.
- Fix:

  ```
  azd env set AZURE_LOCATION eastus2
  ```

- Evidence:
  - skipped: input\-unavailable\: no\-environment\-selected

### Model/version/SKU offered in target region — SKIPPED

- Rule: `FND-DEP-004` (version 1)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json> (verified `2026-10-04`)
- Recommendation: Choose a model, version and SKU that the target region lists, or move the Foundry resource to a region that offers it\. To list what a region offers, query the models API for that location\. If the model is deprecating, plan the move to the replacement version\.
- Fix:

  ```
  az cognitiveservices model list --location <region> --query "[?model.name=='gpt-4o'].{version:model.version,sku:model.skus[].name}"
  ```

- Evidence:
  - skipped: not\-implemented

### Quota covers requested capacity — SKIPPED

- Rule: `FND-DEP-005` (version 2)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/openai/how-to/quota> (verified `2026-10-04`)
- Recommendation: Reduce sku\.capacity, move the deployment to a region or deployment type with remaining quota, free quota from unused deployments, or request a quota increase before provisioning\.
- Fix:

  ```
  # lower the requested capacity in the model deployment or choose another location
  deployments:
    - name: chat
      model: { format: OpenAI, name: gpt-4o, version: "2024-08-06" }
      sku: { name: GlobalStandard, capacity: 50 }
  ```

- Evidence:
  - skipped: not\-implemented

### No blocking soft\-deleted resource/name — SKIPPED

- Rule: `FND-DEP-006` (version 2)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/ai-services/recover-purge-resources> (verified `2026-10-04`)
- Recommendation: Recover the soft\-deleted resource if you still need it, or purge it \(needs the delete permission at subscription scope\) before provisioning, or choose a different name\. Purging permanently deletes data\.
- Fix:

  ```
  # purge a soft-deleted Foundry account so the name can be reused (permanently deletes the account)
  az resource delete --ids /subscriptions/<sub-id>/providers/Microsoft.CognitiveServices/locations/<region>/resourceGroups/<rg>/deletedAccounts/<name>
  ```

- Evidence:
  - skipped: not\-implemented

### Globally unique names available — SKIPPED

- Rule: `FND-DEP-007` (version 2)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json> (verified `2026-10-04`)
- Recommendation: Add a deterministic unique suffix to globally unique names \(for example with the Bicep uniqueString function\), keep the Foundry custom subdomain and account naming rules, and avoid reusing a name whose resource was recently deleted \(see FND\-DEP\-006\)\.
- Fix:

  ```
  var resourceToken = toLower(uniqueString(subscription().id, environmentName, location))
  var foundryName = 'aif-${resourceToken}'
  ```

- Evidence:
  - skipped: not\-implemented

### What\-if has no unexpected delete — SKIPPED

- Rule: `FND-DEP-008` (version 4)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/deployments/stable/2026-06-01/deployments.json> (verified `2026-10-04`)
- Recommendation: Review each predicted Delete\. If it is intended, record it in the baseline with a reason and expiry; if not, switch the deployment to incremental mode, add the missing resource to the template, or remove the conflicting change\. Re\-run what\-if after fixing\.
- Fix:

  ```
  # preview at resource group scope; nothing is deployed
  az deployment group what-if --resource-group <rg> --template-file main.bicep --parameters main.bicepparam
  ```

- Evidence:
  - skipped: not\-implemented

### Locks do not block change — SKIPPED

- Rule: `FND-DEP-010` (version 3)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/azure-resource-manager/management/lock-resources> (verified `2026-10-04`)
- Recommendation: Remove or relax the lock for the duration of the deployment through your change process, deploy to a scope without a ReadOnly lock, or scope locks to the specific resources that need protection and reapply them after the deployment\.
- Fix:

  ```
  az lock list --resource-group <rg> --output table
  az lock delete --name <lock-name> --resource-group <rg>   # then redeploy and re-create the lock
  ```

- Evidence:
  - skipped: not\-implemented

### Subnet delegation/capacity/use is compatible — SKIPPED

- Rule: `FND-DEP-011` (version 2)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks> (verified `2026-10-04`)
- Recommendation: Use a dedicated subnet of /24 \(at least /27\) in an allowed private range, delegate it to Microsoft\.App/environments, keep it in the same region as the Foundry resource, and give each Foundry resource its own agent subnet\.
- Fix:

  ```
  resource agentSubnet 'Microsoft.Network/virtualNetworks/subnets@2025-05-01' = {
    parent: vnet
    name: 'agent-subnet'
    properties: {
      addressPrefix: '192.168.0.0/24'
      delegations: [
        { name: 'Microsoft.App/environments', properties: { serviceName: 'Microsoft.App/environments' } }
      ]
    }
  }
  ```

- Evidence:
  - skipped: not\-implemented

### Target region supports selected setup — SKIPPED

- Rule: `FND-DEP-012` (version 2)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/limits-quotas-regions> (verified `2026-10-04`)
- Recommendation: Choose a region that offers every selected resource type and feature, or change the feature selection \(for example a different tool\)\. Use the Learn supported\-regions table to compare candidate regions\.
- Fix:

  ```
  azd env set AZURE_LOCATION eastus2
  ```

- Evidence:
  - skipped: not\-implemented

### Prod does not reference non\-prod resource IDs — NOT_APPLICABLE

- Rule: `FND-ENV-003` (version 4)
- Mandatory: true
- Evidence basis: deterministic
- Why this state: Not applicable\: not\-applicable\: fewer than two azd environments\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview> (verified `2026-10-05`)
- Recommendation: Point the production environment at production resources, or provision the dependency in production\. Re\-run provisioning for the production environment so its outputs are written to its own \.env\.
- Fix:

  ```
  # .azure/prod/.env
  AZURE_AI_PROJECT_ENDPOINT=https://prod-account.services.ai.azure.com/api/projects/prod-project
  ```

- Evidence:
  - skipped: not\-applicable\: fewer than two azd environments

### Role assignments use object IDs and principalType — SKIPPED

- Rule: `FND-IDN-003` (version 5)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/role-based-access-control/role-assignments-template> (verified `2026-10-04`)
- Recommendation: Pass the object ID of the identity \(identity\.principalId or the user\-assigned identity\'s properties\.principalId\), never the client ID, and set principalType explicitly\. Use ServicePrincipal for managed identities and service principals created in the same deployment, and use API version 2022\-04\-01 or later\.
- Fix:

  ```
  resource blobRole 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
    scope: storage
    name: guid(storage.id, uami.id, 'ba92f5b4-2d11-453d-a403-e96b0029c9fe')
    properties: {
      roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'ba92f5b4-2d11-453d-a403-e96b0029c9fe')
      principalId: uami.properties.principalId // object ID, not uami.properties.clientId
      principalType: 'ServicePrincipal'
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Private endpoint DNS zone groups and VNet links — SKIPPED

- Rule: `FND-NET-003` (version 3)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/private-link/private-endpoint-dns> (verified `2026-10-04`)
- Recommendation: Add a privateDnsZoneGroup to each private endpoint with the documented zones \(three for a Foundry account\) and link every zone to the VNet that resolves the names\. If DNS is centralised in a hub or a private resolver, declare that in the Foundry Doctor configuration so the check is skipped with a reason instead of failing\.
- Fix:

  ```
  resource zoneGroup 'Microsoft.Network/privateEndpoints/privateDnsZoneGroups@2024-05-01' = {
    parent: pe
    name: 'default'
    properties: {
      privateDnsZoneConfigs: [
        { name: 'cognitiveservices', properties: { privateDnsZoneId: zoneCognitive.id } }
        { name: 'openai',            properties: { privateDnsZoneId: zoneOpenAi.id } }
        { name: 'servicesai',        properties: { privateDnsZoneId: zoneServicesAi.id } }
      ]
    }
  }
  resource link 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2024-06-01' = {
    parent: zoneCognitive
    name: 'vnet-link'
    location: 'global'
    properties: { virtualNetwork: { id: vnetId }, registrationEnabled: false }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Agent subnet delegation and sizing — SKIPPED

- Rule: `FND-NET-004` (version 3)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks> (verified `2026-10-04`)
- Recommendation: Create a dedicated subnet for each Foundry account, delegate it to Microsoft\.App/environments, size it /27 or larger \(the Foundry docs recommend /24 for production hosted agents\), keep the name at 63 bytes or fewer, and do not reuse it for another Foundry resource or other services\. Set the network injection when you create the account; it cannot be added to an existing account for hosted agents\.
- Fix:

  ```
  resource agentSubnet 'Microsoft.Network/virtualNetworks/subnets@2024-05-01' = {
    parent: vnet
    name: 'agent-subnet'
    properties: {
      addressPrefix: '10.0.0.0/24'
      delegations: [{
        name: 'Microsoft.App.environments'
        properties: { serviceName: 'Microsoft.App/environments' }
      }]
    }
  }
  // on the account:
  //   networkInjections: [{ scenario: 'agent', subnetArmId: agentSubnet.id, useMicrosoftManagedNetwork: false }]
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Private\-mode VNet/region consistency — SKIPPED

- Rule: `FND-NET-005` (version 4)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks> (verified `2026-10-04`)
- Recommendation: Create the VNet, agent subnet and Foundry account in the same region, and create each private endpoint in the region and subscription of the VNet that hosts it\. Cross\-region data resources are allowed but cost more in data transfer; keep them in the Foundry region unless you have a reason\.
- Fix:

  ```
  param location string = 'swedencentral'
  resource vnet 'Microsoft.Network/virtualNetworks@2024-05-01' = { name: vnetName, location: location, properties: { addressSpace: { addressPrefixes: ['10.0.0.0/16'] } } }
  resource account 'Microsoft.CognitiveServices/accounts@2026-09-01' = { name: accountName, location: location, kind: 'AIServices', sku: { name: 'S0' }, properties: { /* networkInjections -> subnet in vnet */ } }
  resource pe 'Microsoft.Network/privateEndpoints@2024-05-01' = { name: '${accountName}-pe', location: location /* same region as the VNet */, properties: { subnet: { id: peSubnetId } } }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Private address space and overlap — SKIPPED

- Rule: `FND-NET-006` (version 3)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks> (verified `2026-10-04`)
- Recommendation: Put the agent subnet in 10\.0\.0\.0/8, 172\.16\.0\.0/12 \(avoiding 172\.30\.0\.0/16 and 172\.31\.0\.0/16\) or 192\.168\.0\.0/16 and keep the VNet and its peers disjoint from each other and from the reserved ranges\. If your IP plan overlaps or you cannot manage ranges, use a Microsoft managed virtual network instead\.
- Fix:

  ```
  resource vnet 'Microsoft.Network/virtualNetworks@2024-05-01' = {
    name: vnetName
    location: location
    properties: {
      // avoid 169.254/16, 172.30/16, 172.31/16, 192.0.2/24, 0/8, 127/8 and 100.100.* reserved ranges
      addressSpace: { addressPrefixes: ['10.20.0.0/16'] }
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Search SKU supports private endpoints — SKIPPED

- Rule: `FND-NET-008` (version 3)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/search/service-create-private-endpoint> (verified `2026-10-04`)
- Recommendation: If private access is required, choose Basic or higher and account for the additional fixed cost; otherwise keep Free public rather than creating a false expectation that Free supports Private Link\. Plan the tier against indexer needs using the Search limits article\.
- Fix:

  ```
  resource search 'Microsoft.Search/searchServices@2025-05-01' = {
    name: searchName
    location: location
    sku: { name: 'standard' } // 'free' does not support private endpoints
    properties: { publicNetworkAccess: 'Disabled' }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Search replica/SLA posture — SKIPPED

- Rule: `FND-REL-001` (version 1)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: Dedicated Azure AI Search bills per search unit \(replicas x partitions\); adding replicas raises cost\. (<https://learn.microsoft.com/azure/search/search-sku-manage-costs>, verified `2026-10-18`)
- Source: <https://learn.microsoft.com/azure/search/search-limits-quotas-capacity> (verified `2026-10-04`)
- WARA references (recommendations, not platform facts):
  - `b376281d-bfec-4695-8f90-9a44544fdfa4` AI Search replicas for availability zone support (pgVerified: false)
  - `dff62efe-c3a3-4621-98b3-c877c65cb195` AI Search multi\-region (pgVerified: false)
- Recommendation: Set properties\.replicaCount to at least 2 if the application only queries, or at least 3 if it also updates indexes online\. Each replica adds search units and cost, so keep dev and test at 1 unless you are testing failover behaviour\.
- Fix:

  ```
  resource search 'Microsoft.Search/searchServices@2025-05-01' = {
    name: searchName
    location: location
    sku: { name: 'standard' }
    properties: {
      replicaCount: 3
      partitionCount: 1
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Storage redundancy posture — SKIPPED

- Rule: `FND-REL-002` (version 1)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: LRS is the lowest\-cost redundancy option; ZRS and GZRS trade higher cost for availability\. (<https://learn.microsoft.com/azure/storage/common/storage-redundancy>, verified `2026-10-18`)
- Source: <https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency> (verified `2026-10-04`)
- WARA references (recommendations, not platform facts):
  - `e6c7e1cc-2f47-264d-aa50-1da421314472` Zone or region redundant storage (pgVerified: true)
- Recommendation: Use Standard\_ZRS at minimum for storage that holds project or agent data, and Standard\_GZRS for Agent Service standard\-mode storage as recommended by the Foundry resiliency guidance\. SKU changes on an existing account are a migration, so decide at design time\. Note the Foundry multi\-region section says default storage failover with GRS/GZRS is not supported for Foundry projects, so geo\-redundancy there protects data recovery, not automatic project failover\.
- Fix:

  ```
  resource st 'Microsoft.Storage/storageAccounts@2026-09-01' = {
    name: storageName
    location: location
    kind: 'StorageV2'
    sku: { name: 'Standard_GZRS' }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Cosmos zone redundancy and continuous backup — SKIPPED

- Rule: `FND-REL-003` (version 1)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: Continuous backup beyond the 7\-day tier carries a monthly backup storage charge and every restore is charged; the 7\-day tier has no backup storage charge\. The cost of zone redundancy is not cited here\. (<https://learn.microsoft.com/azure/cosmos-db/continuous-backup-restore-introduction>, verified `2026-10-18`)
- Source: <https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency> (verified `2026-10-04`)
- WARA references (recommendations, not platform facts):
  - `921631f6-ed59-49a5-94c1-f0f3ececa580` Cosmos DB availability zones (pgVerified: false)
  - `e544520b-8505-7841-9e77-1f1974ee86ec` Cosmos DB continuous backup (pgVerified: true)
- Recommendation: Set isZoneRedundant true on each location in regions that support availability zones, and set backupPolicy\.type Continuous \(7\-day or 30\-day tier, per the Foundry guidance\) before an incident occurs\. Continuous backup adds cost\. Also consider a second location with automatic failover enabled \(see FND\-REL\-007\)\.
- Fix:

  ```
  resource cosmos 'Microsoft.DocumentDB/databaseAccounts@2025-10-15' = {
    name: cosmosName
    location: location
    properties: {
      databaseAccountOfferType: 'Standard'
      locations: [
        { locationName: location, failoverPriority: 0, isZoneRedundant: true }
      ]
      backupPolicy: {
        type: 'Continuous'
        continuousModeProperties: { tier: 'Continuous7Days' }
      }
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Delete locks on stateful services — SKIPPED

- Rule: `FND-REL-004` (version 2)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency> (verified `2026-10-04`)
- Recommendation: Add a CanNotDelete lock on the Foundry account and on each stateful dependency, or on the resource group that contains only that workload\. Creating locks needs Microsoft\.Authorization/locks/write \(Owner or User Access Administrator, not Contributor\)\. Locks do not protect data\-plane content such as individual blobs, indexes or threads; combine with least\-privilege RBAC and, optionally, the Azure Policy denyAction effect\.
- Fix:

  ```
  resource lock 'Microsoft.Authorization/locks@2020-05-01' = {
    name: 'protect-foundry'
    scope: foundryAccount
    properties: {
      level: 'CanNotDelete'
      notes: 'Foundry account holds projects and connections'
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Search sizing within platform limits — SKIPPED

- Rule: `FND-REL-005` (version 1)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/resource-manager/Microsoft.Search/Search/stable/2025-05-01/search.json> (verified `2026-10-04`)
- Recommendation: Choose a partitionCount from 1, 2, 3, 4, 6 or 12 and a replicaCount of 12 or fewer \(3 or fewer on basic\), keep replicas x partitions at or below 36 search units, and use hostingMode HighDensity only with standard3 and at most 3 partitions\. See the partition and replica combinations table in the capacity planning article\.
- Fix:

  ```
  resource search 'Microsoft.Search/searchServices@2025-05-01' = {
    name: searchName
    location: location
    sku: { name: 'standard' }
    properties: {
      replicaCount: 3
      partitionCount: 2   // 6 search units, within the 36 SU limit
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Multi\-region/DR plan question — QUESTION

- Rule: `FND-REL-007` (version 1)
- Mandatory: false
- Evidence basis: none
- Why this state: No answer recorded \(disasterRecovery\.declared\)\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/how-to/high-availability-resiliency> (verified `2026-10-04`)
- Review question: Has the team recorded its disaster recovery strategy and a reference to the supporting plan?
- WARA references (recommendations, not platform facts):
  - `dff62efe-c3a3-4621-98b3-c877c65cb195` AI Search multi\-region (pgVerified: false)
  - `43663217-a1d3-844b-80ea-571a2ce37c6c` Cosmos DB in at least two regions (pgVerified: unrecorded)
  - `9cabded7-a1fc-6e4a-944b-d7dd98ea31a2` Cosmos DB service\-managed failover (pgVerified: unrecorded)
  - `61187af4-7d36-4b48-b16e-de78bef143a0` Azure OpenAI in multiple regions (pgVerified: false)
- Recommendation: Decide and record the intended strategy\: single region with backup and restore, or multi\-region \(hot/hot, hot/warm, hot/cold\) with the RTO/RPO and cost it implies\. For multi\-region Foundry the article says you create separate Foundry resources and dependencies in each region, deploy to both from CI/CD, and replicate or rebuild state \(Cosmos DB read regions with service\-managed failover, a second AI Search service, storage redundancy\)\.
- Fix:

  ```
  # foundry-doctor configuration (key name as in ADR-007; the schema is defined by the configuration spec)
  reliability:
    disasterRecovery:
      strategy: single-region   # or hot-hot | hot-warm | hot-cold
      recoveryRegion: northeurope
      notes: Recover from Cosmos point-in-time backup; rebuild Search index from source
  ```

- Evidence:
  - skipped: not\-implemented

### Provisioned throughput spillover recommendation — SKIPPED

- Rule: `FND-REL-008` (version 1)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/openai/how-to/spillover-traffic-management> (verified `2026-10-04`)
- WARA references (recommendations, not platform facts):
  - `0c193899-da60-4a52-b4a0-77d75ac8c5c5` Pay\-as\-you\-go instance to manage provisioned overflow (pgVerified: true)
- Recommendation: Create a standard \(pay\-as\-you\-go\) deployment of the same model and version in the same account, then set spilloverDeploymentName on the provisioned deployment to its name\. Spillover initiates on 429, 500 and 503 \(and on 400 for long\-context requests\) and is billed at standard rates for the overflow; monitor with the IsSpillover split in Azure Monitor\.
- Fix:

  ```
  resource ptu 'Microsoft.CognitiveServices/accounts/deployments@2026-05-01' = {
    parent: foundry
    name: 'gpt-4o-mini-ptu'
    sku: { name: 'GlobalProvisionedManaged', capacity: 100 }
    properties: {
      model: { format: 'OpenAI', name: 'gpt-4o-mini', version: '2024-07-18' }
      spilloverDeploymentName: 'gpt-4o-mini-standard'
    }
    dependsOn: [ standard ]
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### APIM availability tier recommendation — SKIPPED

- Rule: `FND-REL-009` (version 1)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/api-management/api-management-features> (verified `2026-10-04`)
- Review question: Which API Management SLA, zone\-redundancy or multi\-region objective does the workload need? \(The rule reads reliability\.apimSlaRequired, apimZoneRequired and apimRegionalRequired, which the configuration schema does not define yet; ADR\-026\.\)
- WARA references (recommendations, not platform facts):
  - `baf3bfc0-32a2-4c0c-926d-c9bf0b49808e` API Management Premium for availability zones (pgVerified: true)
  - `740f2c1c-8857-4648-80eb-47d2c56d5a50` API Management availability zones (pgVerified: true)
  - `af4f88cb-35e8-4371-b29e-3a32b1d2f40a` API Management multi\-region (pgVerified: true)
  - `e35cf148-8eee-49d1-a1c9-956160f99e0b` API Management stv2 platform \(platform migration, context only\) (pgVerified: true)
- Recommendation: Select the least APIM tier and capacity that satisfy the workload\'s declared SLA, throughput, zone\-resilience and multi\-region objectives; do not default to Premium or two units\. For production traffic that requires an SLA, do not use Developer\. If zone resilience is declared, select the least documented zone\-capable tier and capacity that satisfy the objective in a region where APIM zone support has been verified\. Select Premium and add another region only when regional resilience is declared \(see FND\-REL\-007\)\. Record the cost\-versus\-resilience tradeoff and accept a lower tier or capacity when it satisfies the declared objective\.
- Fix:

  ```
  // Values are selected from the declared SLA, throughput, zone and region objectives.
  // Do not hard-code Premium/2 as a universal baseline. Verify current tier SLA,
  // regional feature availability and required capacity before setting these parameters.
  param apimTier string
  param apimCapacity int
  resource apim 'Microsoft.ApiManagement/service@2024-05-01' = {
    name: apimName
    location: location
    sku: {
      name: apimTier
      capacity: apimCapacity
    }
    properties: {
      publisherEmail: publisherEmail
      publisherName: publisherName
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Private DNS resolves from an appropriate vantage point — SKIPPED

- Rule: `FND-RUN-002` (version 4)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/how-to/virtual-networks> (verified `2026-10-04`)
- Recommendation: Link each required privatelink zone to the VNet that hosts the agent client and private endpoint subnets, configure conditional forwarders to 168\.63\.129\.16 where custom DNS is used, and re\-run the doctor from a machine inside the VNet\.
- Fix:

  ```
  resource searchDnsLink 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2018-09-01' = {
    parent: searchPrivateDnsZone // privatelink.search.windows.net
    name: 'link-to-agent-vnet'
    location: 'global'
    properties: {
      virtualNetwork: { id: agentVnet.id }
      registrationEnabled: false
    }
  }
  ```

- Evidence:
  - skipped: not\-implemented

### Capability host and project connections healthy — SKIPPED

- Rule: `FND-RUN-003` (version 3)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices/stable/2026-09-01/cognitiveservices.json> (verified `2026-10-17`)
- Recommendation: Inspect the failing capability host or project in the Foundry portal or with az rest, correct the underlying cause \(for example a missing role or a deleted bound resource\), and redeploy the capability host\. Recreate any missing connection with the same name the capability host references\.
- Fix:

  ```
  resource capHost 'Microsoft.CognitiveServices/accounts/projects/capabilityHosts@2026-09-01' = {
    parent: project
    name: 'default'
    properties: {
      capabilityHostKind: 'Agents'
      storageConnections: [ storageConnection.name ]
      threadStorageConnections: [ cosmosConnection.name ]
      vectorStoreConnections: [ searchConnection.name ]
    }
  }
  ```

- Evidence:
  - skipped: not\-implemented

### Model deployment state and throttling signal — SKIPPED

- Rule: `FND-RUN-004` (version 4)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/azure-monitor/reference/supported-metrics/microsoft-cognitiveservices-accounts-metrics> (verified `2026-10-18`)
- Recommendation: For a failed or disabled deployment, review the deployment error in the Foundry portal and recreate it\. For 429 signals, raise the deployment capacity or quota, spread load across deployments or regions, or add client back\-off and retry\.
- Fix:

  ```
  resource gpt 'Microsoft.CognitiveServices/accounts/deployments@2026-09-01' = {
    parent: account
    name: 'gpt-4o'
    sku: { name: 'GlobalStandard', capacity: 100 }
    properties: {
      model: { format: 'OpenAI', name: 'gpt-4o', version: '2024-11-20' }
    }
  }
  ```

- Evidence:
  - skipped: not\-implemented

### Agent tools/connections respond — SKIPPED

- Rule: `FND-RUN-005` (version 4)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://github.com/Azure/azure-dev/blob/main/cli/azd/extensions/azure.ai.agents/internal/project/service_target_agent.go> (verified `2026-10-18`)
- Recommendation: Open the failing agent version or connection in the Foundry portal, read its error, and redeploy the agent or recreate the connection\. Test tool calls with an explicit, operator\-run test; the doctor does not call tools\.
- Fix:

  ```
  # azure.yaml: keep the agent service and the connection names it uses aligned with the project
  services:
    support-agent:
      host: azure.ai.agent
  ```

- Evidence:
  - skipped: not\-implemented

### Search index/documents/vector/indexer health — SKIPPED

- Rule: `FND-RUN-006` (version 5)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/data-plane/Search/stable/2026-04-01/search.json> (verified `2026-10-17`)
- Recommendation: Create or repair the index with the expected schema, run the indexer and review its execution errors, and confirm the agent\'s search connection and index name match\. Grant the doctor identity Search Service Contributor only if you want schema and indexer checks\.
- Fix:

  ```
  // Expected vector field shape in the index definition (REST, api-version 2026-04-01)
  {
    "name": "contentVector",
    "type": "Collection(Edm.Single)",
    "dimensions": 1536,
    "vectorSearchProfile": "default-profile"
  }
  ```

- Evidence:
  - skipped: not\-implemented

## Operational Excellence

### Valid YAML, duplicate\-key detection and compatible azure\.ai\.\* schema — PASS

- Rule: `FND-CFG-001` (version 7)
- Mandatory: true
- Evidence basis: deterministic
- Why this state: Evaluated with no remaining finding and no skipped check\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference> (verified `2026-10-05`)
- Recommendation: Fix the YAML syntax or the reported schema violation\. Add the yaml\-language\-server schema comment to azure\.yaml so the editor flags the same errors, and install azure\.ai\.agents and azure\.ai\.projects at the versions declared in requiredVersions\.
- Fix:

  ```
  # yaml-language-server: $schema=https://raw.githubusercontent.com/Azure/azure-dev/main/schemas/v1.0/azure.yaml.json
  name: my-agent-project
  services:
    ai-project:
      host: azure.ai.project
    my-agent:
      host: azure.ai.agent
      project: src/my-agent
      uses: [ai-project]
      kind: hosted
  ```

- Evidence: no finding or skip record was present.

### Unique service names by kind — PASS

- Rule: `FND-CFG-002` (version 2)
- Mandatory: true
- Evidence basis: deterministic
- Why this state: Evaluated with no remaining finding and no skipped check\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference> (verified `2026-10-04`)
- Recommendation: Give separate logical agents distinct names\. If the services intentionally represent versions of one agent, keep one service definition and deploy the intended version through that service\.
- Fix:

  ```
  services:
    ai-project:
      host: azure.ai.project
    triage:
      host: azure.ai.agent
      project: src/triage
      uses: [ai-project]
      kind: hosted
      name: triage-agent
    summarizer:
      host: azure.ai.agent
      project: src/summarizer
      uses: [ai-project]
      kind: hosted
      name: summarizer-agent
  ```

- Evidence: no finding or skip record was present.

### Every variable has an environment or infrastructure producer — SKIPPED

- Rule: `FND-CFG-006` (version 5)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: no\-environment\-selected\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference> (verified `2026-10-04`)
- Recommendation: Set the variable in the active azd environment \(azd env set\), emit it from a Bicep output, give it an inline default, or remove the reference\. Do not declare FOUNDRY\_ or AGENT\_ prefixed names in the azure\.yaml service env \(FND\-CFG\-011\)\.
- Fix:

  ```
  # Produce the value
  #   azd env set MICROSOFT_FOUNDRY_MODEL_DEPLOYMENT_NAME gpt-5.4-mini
  services:
    ai-project:
      host: azure.ai.project
    my-agent:
      host: azure.ai.agent
      project: src/my-agent
      uses: [ai-project]
      kind: hosted
      env:
        MICROSOFT_FOUNDRY_MODEL_DEPLOYMENT_NAME: ${MICROSOFT_FOUNDRY_MODEL_DEPLOYMENT_NAME}
        LOG_LEVEL: ${LOG_LEVEL:-info}
  ```

- Evidence:
  - skipped: input\-unavailable\: no\-environment\-selected

### Preview/retired fields are version\-aware — PASS

- Rule: `FND-CFG-011` (version 5)
- Mandatory: true
- Evidence basis: deterministic
- Why this state: Evaluated with no remaining finding and no skipped check\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/agents/concepts/azure-yaml-reference> (verified `2026-10-04`)
- Recommendation: Move to the unified service\-level shape\: put env on the agent service, use kind voice, use host azure\.ai\.project instead of microsoft\.foundry, drop fields the hosted\-agent create request ignores, and declare requiredVersions for extension versions verified by this catalogue\. Do not infer a field\'s introduction version from the current schema\.
- Fix:

  ```
  requiredVersions:
    extensions:
      azure.ai.agents: ">=1.0.0-beta.18"
      azure.ai.projects: ">=1.0.0-beta.13"
  services:
    ai-project:
      host: azure.ai.project
    my-agent:
      host: azure.ai.agent
      project: src/my-agent
      uses: [ai-project]
      kind: hosted
      env:
        MICROSOFT_FOUNDRY_MODEL_DEPLOYMENT_NAME: ${MICROSOFT_FOUNDRY_MODEL_DEPLOYMENT_NAME}
  ```

- Evidence: no finding or skip record was present.

### Signed into matching tenant/subscription — SKIPPED

- Rule: `FND-DEP-001` (version 1)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/subscriptions/stable/2022-12-01/subscriptions.json> (verified `2026-10-04`)
- Recommendation: Sign in to the tenant that owns the subscription \(azd auth login \-\-tenant\-id &lt;tenant\-id&gt;\), then confirm AZURE\_SUBSCRIPTION\_ID and AZURE\_TENANT\_ID in the selected azd environment \(azd env get\-values\)\. If the subscription is in another tenant, create a separate azd environment for that tenant\.
- Fix:

  ```
  azd auth login --tenant-id <tenant-id>
  azd env set AZURE_TENANT_ID <tenant-id>
  azd env set AZURE_SUBSCRIPTION_ID <subscription-id>
  ```

- Evidence:
  - skipped: not\-implemented

### Required providers registered — SKIPPED

- Rule: `FND-DEP-002` (version 3)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/resources/stable/2025-04-01/resources.json> (verified `2026-10-04`)
- Recommendation: Register the missing providers once per subscription with a principal that has the &lt;provider&gt;/register/action permission \(Contributor or Owner\), and wait until the state is Registered, or at least Registering for the regions in use\.
- Fix:

  ```
  az provider register --namespace Microsoft.App
  az provider register --namespace Microsoft.ContainerService
  az provider show --namespace Microsoft.App --query registrationState
  ```

- Evidence:
  - skipped: not\-implemented

### Likely policy denials identified — SKIPPED

- Rule: `FND-DEP-009` (version 3)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://github.com/Azure/azure-rest-api-specs/blob/main/specification/policyinsights/resource-manager/Microsoft.PolicyInsights/PolicyInsights/stable/2024-10-01/openapi.json> (verified `2026-10-04`)
- Recommendation: Change the planned value to one the policy allows, request a policy exemption, or ask the policy owner for a compliant pattern\. Do not disable the policy to make a deployment pass\.
- Fix:

  ```
  # example: a policy restricts locations; pick an allowed one
  azd env set AZURE_LOCATION <allowed-region>
  ```

- Evidence:
  - skipped: not\-implemented

### Environments do not unintentionally share resource groups — NOT_APPLICABLE

- Rule: `FND-ENV-001` (version 5)
- Mandatory: true
- Evidence basis: deterministic
- Why this state: Not applicable\: not\-applicable\: fewer than two azd environments\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview> (verified `2026-10-05`)
- Recommendation: Give each environment its own resource group, for example by including the environment name in the group name \(resourceGroup\: rg\-\$\{AZURE\_ENV\_NAME\}\), or record an explicit suppression with a reason and expiry when sharing is intentional\.
- Fix:

  ```
  name: my-agent-project
  resourceGroup: rg-${AZURE_ENV_NAME}
  ```

- Evidence:
  - skipped: not\-applicable\: fewer than two azd environments

### Environment\-bound names/IDs/rules were not copied unchanged — NOT_APPLICABLE

- Rule: `FND-ENV-002` (version 4)
- Mandatory: false
- Evidence basis: deterministic
- Why this state: Not applicable\: not\-applicable\: fewer than two azd environments\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview> (verified `2026-10-05`)
- Recommendation: Review each reported key and give the environment its own value, or suppress the finding with a reason and an expiry where the shared value is intentional\. Prefer deriving names from AZURE\_ENV\_NAME\.
- Fix:

  ```
  # .azure/prod/.env
  AZURE_RESOURCE_GROUP=rg-myapp-prod
  AZURE_AI_PROJECT_ENDPOINT=https://prod-account.services.ai.azure.com/api/projects/prod-project
  ```

- Evidence:
  - skipped: not\-applicable\: fewer than two azd environments

### Informational difference summary — NOT_APPLICABLE

- Rule: `FND-ENV-004` (version 3)
- Mandatory: false
- Evidence basis: deterministic
- Why this state: Not applicable\: not\-applicable\: fewer than two azd environments\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/developer/azure-developer-cli/environments-overview> (verified `2026-10-05`)
- Recommendation: Review the summary before promoting a release; use azd env get\-values per environment for the details you are entitled to see\.
- Fix:

  ```
  foundry-doctor compare --env dev --env prod
  ```

- Evidence:
  - skipped: not\-applicable\: fewer than two azd environments

### Diagnostic settings to Log Analytics — SKIPPED

- Rule: `FND-OPS-001` (version 2)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/azure-monitor/reference/supported-logs/microsoft-cognitiveservices-accounts-logs> (verified `2026-10-04`)
- Recommendation: Add a Microsoft\.Insights/diagnosticSettings resource per resource, scoped to the resource, that sends the audit category group \(or allLogs when you need request and response diagnostics\) to your Log Analytics workspace\. Use audit as a low\-cost start because several categories \(for example AzureOpenAIRequestUsage and ManagedNetworkEvent on accounts, and all project categories\) are marked as having a cost to export in the Azure Monitor reference\.
- Fix:

  ```
  resource diag 'Microsoft.Insights/diagnosticSettings@2021-05-01-preview' = {
    name: 'to-law'
    scope: foundryAccount
    properties: {
      workspaceId: logAnalytics.id
      logs: [
        { categoryGroup: 'audit', enabled: true }
      ]
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Application Insights connected — QUESTION

- Rule: `FND-OPS-002` (version 1)
- Mandatory: false
- Evidence basis: none
- Why this state: No requirement or objective is declared \(no\-explicit\-observability\-or\-server\-tracing\-requirement\); the control needs a team decision\.
- Costly recommendation: Workspace\-based Application Insights data is billed for ingestion and retention by the Log Analytics workspace that stores it\. (<https://learn.microsoft.com/azure/azure-monitor/logs/cost-logs>, verified `2026-10-18`)
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/observability/how-to/trace-agent-setup> (verified `2026-10-04`)
- Review question: Is server\-side tracing or hosted\-agent telemetry a declared requirement for this project? \(No configuration key exists yet; ADR\-026\.\)
- Recommendation: If server\-side tracing or hosted\-agent telemetry is a declared requirement, connect an Application Insights resource to the Foundry project \(a connection with category AppInsights\) and link the component to a Log Analytics workspace\. Keep exactly one AppInsights connection per scope\. Choose sampling, retention and daily\-cap settings deliberately\: telemetry ingestion and retention incur cost, so a project that does not require this observability path should not be forced to enable it\.
- Fix:

  ```
  # Safe remediation (no unverified ARM wiring):
  # In the Foundry portal, open the project, Monitoring, and connect the intended
  # workspace-based Application Insights resource. Re-run the live inventory and
  # verify exactly one project connection whose category is AppInsights.
  ```

- Evidence:
  - skipped: no\-explicit\-observability\-or\-server\-tracing\-requirement

### Baseline alerts exist — SKIPPED

- Rule: `FND-OPS-003` (version 2)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/how-to/stay-informed-service-health> (verified `2026-10-04`)
- Recommendation: Add a Service Health alert and metric alerts on ModelAvailabilityRate, ModelRequests filtered by StatusCode \(429 and 5xx\) and TimeToResponse for the Foundry account, each with an action group\. Choose thresholds from your own traffic and latency targets\.
- Fix:

  ```
  resource availabilityAlert 'Microsoft.Insights/metricAlerts@2026-01-01' = {
    name: 'foundry-model-availability'
    location: 'global'
    properties: {
      severity: 2
      enabled: true
      scopes: [ foundryAccount.id ]
      evaluationFrequency: 'PT5M'
      windowSize: 'PT15M'
      criteria: {
        'odata.type': 'Microsoft.Azure.Monitor.SingleResourceMultipleMetricCriteria'
        allOf: [ { name: 'avail', metricName: 'ModelAvailabilityRate', operator: 'LessThan', threshold: 99, timeAggregation: 'Average', criterionType: 'StaticThresholdCriterion' } ]
      }
      actions: [ { actionGroupId: actionGroup.id } ]
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Required tags exist — SKIPPED

- Rule: `FND-OPS-004` (version 2)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/azure-resource-manager/management/tag-resources> (verified `2026-10-04`)
- Recommendation: Define the required tags once \(for example in a Bicep user\-defined type\), pass them to every resource, and enforce them at runtime with Azure Policy \(Require a tag on resources\)\. Foundry Doctor reports where the template omits them\.
- Fix:

  ```
  @description('Tags required by the organisation.')
  param tags requiredTags
  type requiredTags = { Env: string, CostCode: string }

  resource account 'Microsoft.CognitiveServices/accounts@2026-09-01' = {
    name: accountName
    location: location
    kind: 'AIServices'
    sku: { name: 'S0' }
    tags: tags
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Resource\-name provider constraints — SKIPPED

- Rule: `FND-OPS-005` (version 2)
- Mandatory: true
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/azure-resource-manager/management/resource-name-rules> (verified `2026-10-04`)
- Recommendation: Choose names that satisfy the provider limits for every environment name azd can produce, constrain name parameters with \@minLength and \@maxLength, and keep a short resource token for globally scoped resources \(Storage 24, Key Vault 24, Cosmos DB 44, APIM 50, Search 60\)\.
- Fix:

  ```
  @minLength(3)
  @maxLength(24)
  param storageName string

  resource sa 'Microsoft.Storage/storageAccounts@2024-01-01' = {
    name: toLower(replace(storageName, '-', ''))
    location: location
    sku: { name: 'Standard_LRS' }
    kind: 'StorageV2'
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Log retention meets policy — SKIPPED

- Rule: `FND-OPS-006` (version 2)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: Log Analytics charges for data retention in addition to ingestion; longer retention adds cost\. (<https://learn.microsoft.com/azure/azure-monitor/logs/cost-logs>, verified `2026-10-18`)
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/azure-monitor/logs/data-retention-configure> (verified `2026-10-04`)
- Recommendation: Set workspace retentionInDays to at least the value your policy requires, or set totalRetentionInDays on the tables that hold Foundry diagnostics and traces to keep older data in low\-cost long\-term retention\. Retention beyond the free period is billed\.
- Fix:

  ```
  resource law 'Microsoft.OperationalInsights/workspaces@2025-07-01' = {
    name: lawName
    location: location
    properties: {
      retentionInDays: 90
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### CI/CD pipeline exists — NOT_APPLICABLE

- Rule: `FND-OPS-007` (version 3)
- Mandatory: false
- Evidence basis: deterministic
- Why this state: Not applicable\: dev\-profile\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/developer/azure-developer-cli/configure-devops-pipeline> (verified `2026-10-05`)
- Recommendation: Run azd pipeline config to generate azure\-dev\.yml and the federated credential for GitHub Actions or Azure Pipelines, and use one pipeline per azd environment\.
- Fix:

  ```
  azd pipeline config --provider github
  ```

- Evidence:
  - skipped: dev\-profile

### Evaluations run before promotion — NOT_APPLICABLE

- Rule: `FND-OPS-008` (version 3)
- Mandatory: false
- Evidence basis: deterministic
- Why this state: Not applicable\: dev\-profile\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/agents/how-to/set-up-ci-cd-cli> (verified `2026-10-04`)
- Review question: Is promotion gated by evaluations that reflect the project\'s release decision?
- Recommendation: Generate the evaluation recipe with azd ai agent eval generate, commit eval\.yaml, and run azd ai agent eval run in the pipeline against the candidate version; promote to production only after the evaluation passes your thresholds\.
- Fix:

  ```
  # in .github/workflows/azure-dev.yml, before the production promotion job
  - run: azd ai agent eval run --no-prompt
  ```

- Evidence:
  - skipped: dev\-profile

### Diagnostic logs are arriving — SKIPPED

- Rule: `FND-RUN-007` (version 4)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: not\-implemented\.
- Costly recommendation: false
- Source: <https://learn.microsoft.com/azure/foundry/how-to/diagnostic-logging> (verified `2026-10-04`)
- Recommendation: Enable a diagnostic setting for the Foundry account that sends Audit and Request and Response logs to a Log Analytics workspace, wait up to two hours, and confirm rows with a count query\. Check workspace access and ingestion errors if rows stay at zero while the account has traffic\.
- Fix:

  ```
  resource diag 'Microsoft.Insights/diagnosticSettings@2021-05-01-preview' = {
    name: 'to-law'
    scope: account
    properties: {
      workspaceId: law.id
      logs: [
        { category: 'Audit', enabled: true }
        { category: 'RequestResponse', enabled: true }
      ]
    }
  }
  ```

- Evidence:
  - skipped: not\-implemented

## Cost Optimization

### Budget and alerts exist — SKIPPED

- Rule: `FND-COST-001` (version 4)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://github.com/Azure/azure-rest-api-specs/tree/main/specification/consumption/resource-manager/Microsoft.Consumption/Consumption/stable/2023-11-01> (verified `2026-10-04`)
- Recommendation: Add a Microsoft\.Consumption/budgets resource for the subscription or resource group with an actual\-cost notification \(for example 80 percent\) and a forecasted\-cost notification, sent to a mailbox or an action group that someone monitors\.
- Fix:

  ```
  targetScope = 'subscription'

  resource budget 'Microsoft.Consumption/budgets@2023-11-01' = {
    name: 'foundry-monthly'
    properties: {
      category: 'Cost'
      amount: 500
      timeGrain: 'Monthly'
      timePeriod: { startDate: '2026-11-01T00:00:00Z' }
      notifications: {
        actual80: {
          enabled: true
          operator: 'GreaterThan'
          threshold: 80
          thresholdType: 'Actual'
          contactEmails: [ 'finops@contoso.example' ]
        }
      }
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

### Dev avoids production\-sized fixed SKUs — SKIPPED

- Rule: `FND-COST-002` (version 4)
- Mandatory: false
- Evidence basis: none
- Why this state: Skipped\: input\-unavailable\: compiled ARM model unavailable\.
- Costly recommendation: false
- Recommendation basis: project opinion
- Project opinion, not Microsoft guidance; context source: <https://learn.microsoft.com/azure/foundry/foundry-models/concepts/deployment-types> (verified `2026-10-18`)
- Recommendation: In dev use pay\-per\-token deployments \(GlobalStandard or Standard\), the Search free or basic tier with one replica and one partition, an API Management Developer or consumption\-style tier, and the lowest Cosmos DB throughput that the Foundry Agent Service setup allows\. Reserve production\-sized capacity for test and production environments\.
- Fix:

  ```
  // Bicep - dev environment: a pay-per-token deployment type instead of a provisioned one.
  // Model name and version are placeholders; use a model available in your region.
  resource chat 'Microsoft.CognitiveServices/accounts/deployments@2026-09-01' = {
    parent: account
    name: 'chat'
    sku: { name: 'GlobalStandard', capacity: 10 }
    properties: {
      model: { format: 'OpenAI', name: '<model>', version: '<version>' }
    }
  }
  ```

- Evidence:
  - skipped: input\-unavailable\: compiled ARM model unavailable

## Limitations

- This report maps available Foundry Doctor evidence to selected Azure Well\-Architected aligned controls\. It is not a certification and does not claim complete WAF compliance\.
- Controls without complete deterministic evidence remain visible as UNKNOWN, QUESTION or SKIPPED rather than being treated as passing; a partly skipped evaluation is UNKNOWN, never PASS\.
- ATTESTED controls rest on an answer the team recorded in Foundry Doctor configuration\. Foundry Doctor did not verify the answer\.
- Control IDs are Foundry Doctor rule IDs\. They are not mapped to WAF checklist codes in this release\. Pillar assignments come from the rule catalogue and are project judgement, not Microsoft guidance\.
- A suppressed or baselined finding is shown as WARNING and stays visible in the evidence pack\. It does not by itself produce a No\-go decision, so the decision can be Proceed with conditions while accepted failures remain\.
- The decision wording and the top\-action ranking are project opinion, not Microsoft guidance\.
- PSRule for Azure, Azure Advisor and Microsoft Defender for Cloud recommendations were not imported\. Overlapping external check IDs are listed for reference only \(ADR\-020, ADR\-026\)\.
- Deployed Azure state was not queried\: assess waf runs offline\. Controls that need deployment or runtime evidence and were not evaluated\: FND\-COST\-001, FND\-DEP\-001, FND\-DEP\-002, FND\-DEP\-003, FND\-DEP\-004, FND\-DEP\-005, FND\-DEP\-006, FND\-DEP\-007, FND\-DEP\-008, FND\-DEP\-009, FND\-DEP\-010, FND\-DEP\-011, FND\-DEP\-012, FND\-IDN\-002, FND\-IDN\-005, FND\-NET\-006, FND\-OPS\-009, FND\-RUN\-001, FND\-RUN\-002, FND\-RUN\-003, FND\-RUN\-004, FND\-RUN\-005, FND\-RUN\-006, FND\-RUN\-007, FND\-SEC\-008, FND\-SEC\-009\.
- Compiled infrastructure \(Bicep to ARM\) was not available, so template\-dependent controls are SKIPPED\.
- 30 catalogue rules are excluded from the WAF view with a recorded reason \(internal/assess/waf/mapping\.yaml\)\.
