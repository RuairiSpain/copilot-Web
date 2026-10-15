"""Evaluation decisions for the 108 Foundry rules. Maintainer data, not a dependency.

cls: sy = static-yaml, sp = static-ps, live = needs-live, inp = needs-input.
"""

CLASS_NAME = {"sy": "static-yaml", "sp": "static-ps", "live": "needs-live", "inp": "needs-input"}

# Rules evaluated offline by PSRule: the subset of the catalogue evidence that is implemented.
SUBSET = {
    "FND-CFG-002": "azure.ai.agent services with a literal project and name only. Expressions (${...}) are left out; the catalogue reports them as uncertain.",
    "FND-CFG-003": "Agent toolboxes (must be an azure.ai.toolbox service and also in uses), agent connections and toolbox tools[].connection (must be azure.ai.connection services). Voice conversationEngine.name, memory-store models and generic uses are not evaluated.",
    "FND-CFG-008": "A literal empty allowed_tools list on an inline mcp tool. A missing allowed_tools is uncertain in the catalogue and is not reported.",
    "FND-CFG-010": "Compiled ARM deployments only: model.version missing or latest, and versionUpgradeOption OnceNewDefaultVersionAvailable. The azure.yaml deployments side and model-router handling are not evaluated. The catalogue marks the API version that introduces versionUpgradeOption as unverified.",
    "FND-CFG-011": "config under an azure.ai.agent service, host microsoft.foundry, kind prompt-voice, displayName/inputSchema/outputSchema on hosted agents, env keys with a FOUNDRY_ or AGENT_ prefix. The requiredVersions comparison is not evaluated.",
    "FND-COST-002": "Compiled ARM only: provisioned deployment SKUs, Search standard2/standard3/storage_optimized_* or replica or partition count above 1, API Management Premium/Isolated or capacity above 1. Cosmos throughput (the catalogue ships no default limit), the azure.yaml side and exemption lists are not evaluated. The dev-only intent is carried by severity per profile (warning in dev, info in test and prod), not by environment detection.",
    "FND-IDN-001": "Foundry accounts and projects: identity.type present and not None, and userAssignedIdentities non-empty when UserAssigned. Workload resources (container apps, web apps) are not evaluated because their selection depends on a Foundry endpoint setting. The catalogue marks as unverified whether the platform rejects an account without identity.",
    "FND-IDN-003": "principalType present on every role assignment. The ServicePrincipal check for identities created in the same template, the object-ID check and the apiVersion check need correlation and are not evaluated. The catalogue marks as unverified whether omitting principalType always fails.",
    "FND-IDN-006": "Issuer is an https URL, subject is non-empty and has no * or ? wildcard. GitHub and AKS subject formats, audiences, duplicates and the 20-credential limit are not evaluated.",
    "FND-IQ-012": "Compiled ARM only: a free Search SKU with semanticSearch or knowledgeRetrieval standard. The 'used' test and the disabled check by API version need data-plane and API-version evidence and are not evaluated. The catalogue notes the ARM TypeSpec and the Learn article disagree about the disabled value.",
    "FND-NET-001": "Finding A (open posture) on accounts of kind AIServices or OpenAI: passes when publicNetworkAccess is Disabled or networkAcls.defaultAction is Deny. Finding B (private endpoint missing) needs correlation with Microsoft.Network/privateEndpoints and is not evaluated. Accounts are not limited to those that host a Foundry project.",
    "FND-NET-009": "virtualNetworkType External or Internal requires sku.name Developer or Premium. The V2 delegated-subnet and private-endpoint models are not evaluated; the catalogue marks the ARM properties for them as unverified.",
    "FND-OPS-004": "Tag names listed in the PSRule configuration key FOUNDRY_REQUIRED_TAGS (empty by default, so the rule is skipped), tag count above 50, tag name length (512, or 128 on storage accounts) and tag value length above 256. Tag value formats are not evaluated. Resource types in scope: Foundry accounts and projects, Search, Storage, Cosmos DB, API Management. The catalogue marks as unverified which resource types support tags.",
    "FND-OPS-005": "Names that are literals in the expanded resource for accounts, API Management, Storage, Cosmos DB, Key Vault, Container Registry, Log Analytics workspaces, Application Insights components, container apps and Search. Foundry projects are not evaluated because the expanded child name form was not verified. The accounts pattern enforces only the violations the catalogue lists.",
    "FND-REL-001": "Search services: replicaCount of at least 2 unless sku.name is free. The scope 'referenced by a Foundry connection' is not evaluated, and the info note for exactly 2 replicas is not emitted.",
    "FND-REL-002": "Storage sku.name must not be Standard_LRS or Premium_LRS. The GZRS note for Agent Service standard mode is not evaluated. The catalogue notes two Foundry statements about GZRS that still need reconciling.",
    "FND-REL-003": "Every properties.locations[].isZoneRedundant is true, and backupPolicy.type is Continuous. The continuous tier information and the region-without-zones uncertainty are not emitted.",
    "FND-REL-005": "Checks (a) partitionCount in 1, 2, 3, 4, 6, 12, (b) replicaCount at most 12 and at most 3 on basic, (c) replicas x partitions at most 36 on SKUs above basic, (d) HighDensity only with standard3 and at most 3 partitions. free and serverless SKUs are skipped. Basic partitionCount above 1 is not asserted.",
    "FND-REL-006": "Backends whose properties.url host is openai.azure.com, services.ai.azure.com or cognitiveservices.azure.com: circuitBreaker.rules must be non-empty. Backend pools, retry policy evidence, the Consumption tier and the API-version support check are not evaluated. The catalogue verifies the property only against API 2025-09-01-preview.",
    "FND-REL-008": "Provisioned deployments (ProvisionedManaged, GlobalProvisionedManaged, DataZoneProvisionedManaged): properties.spilloverDeploymentName is set. The check that the named deployment exists and is a standard deployment of the same model is not evaluated.",
    "FND-SEC-001": "Accounts of kind AIServices: properties.disableLocalAuth is true. The catalogue limits the rule to accounts that own a project or deployment child; that correlation is not evaluated, so every AIServices account is checked.",
    "FND-SEC-002": "Search services: properties.disableLocalAuth is true. The authOptions sub-state and the Foundry-scope test are not evaluated, so every Search service is checked.",
    "FND-SEC-003": "Cosmos DB accounts: properties.disableLocalAuth is true. The Foundry-scope test is not evaluated, so every Cosmos DB account is checked. The catalogue notes that PSRule limits its equivalent rule to the NoSQL API and that the API-kind detection was not verified.",
    "FND-SEC-004": "Storage accounts: allowSharedKeyAccess false, allowBlobPublicAccess not true, supportsHttpsTrafficOnly not false, minimumTlsVersion present and not TLS1_0 or TLS1_1. The Foundry-scope test is not evaluated.",
    "FND-SEC-005": "Key Vault: enableSoftDelete not false, enablePurgeProtection true, enableRbacAuthorization true.",
    "FND-SEC-006": "ipRules values on Storage, Cosmos DB, Key Vault, Search and Cognitive Services accounts equal to 0.0.0.0/0 (and 0.0.0.0 on Cosmos DB), and RFC 1918 address prefixes on Storage and Cosmos DB (plus 100.64.0.0/10 on Cosmos DB). Private ranges are matched by address prefix, not by CIDR containment.",
    "FND-SEC-010": "Informational. A fail result at level Information means customer-managed keys are not configured on a Foundry account, Search service, Cosmos DB account or Storage account. The catalogue defines no fail state; treat the result as a report line.",
    "FND-SEC-011": "Informational. A fail result at level Information means properties.restrictOutboundNetworkAccess is not true on an AIServices account. The catalogue defines no fail state and notes the property is configuration evidence only. The azure.yaml network.isolationMode side is not evaluated.",
}

CLS = {}
for _r in "CFG-010 COST-002 IDN-001 IDN-003 IDN-006 IQ-012 NET-001 NET-009 OPS-005 REL-001 REL-002 REL-006 REL-008 SEC-001 SEC-002 SEC-003 SEC-004 SEC-005 SEC-010 SEC-011".split():
    CLS["FND-" + _r] = "sy"
for _r in "CFG-002 CFG-003 CFG-008 CFG-011 OPS-004 REL-003 REL-005 SEC-006".split():
    CLS["FND-" + _r] = "sp"

# Rules PSRule cannot evaluate offline because they call Azure or a runtime.
LIVE = {
    "FND-CFG-012": "Needs Subscriptions_ListLocations (control plane) and AZURE_LOCATION from .azure/<env>/.env. The catalogue says the list membership check is skipped, never passed, without the control plane.",
    "FND-COST-001": "Offline, a template with no budget is reported as skipped by the catalogue (budgets are usually created outside the repository). The positive check lists budgets with the Budgets API.",
    "FND-COST-004": "Needs a retail price file or price API and a supported-meter catalogue. Informational estimate with no pass or fail outcome.",
    "FND-DEP-001": "ARM GET /subscriptions/{id} and GET /tenants, plus AZURE_SUBSCRIPTION_ID and AZURE_TENANT_ID from the azd environment.",
    "FND-DEP-002": "ARM Providers_Get or Providers_List registrationState.",
    "FND-DEP-003": "ARM Permissions_ListForResourceGroup and role assignment lists for the caller.",
    "FND-DEP-004": "ARM Models_List for the target location (Microsoft.CognitiveServices).",
    "FND-DEP-005": "ARM Usages_List and ModelCapacities_List (quota and available capacity).",
    "FND-DEP-006": "ARM deleted-resource lists for Cognitive Services, API Management and Key Vault.",
    "FND-DEP-007": "Per-provider name availability calls (POST checks and one HEAD).",
    "FND-DEP-008": "ARM deployment what-if (long-running POST).",
    "FND-DEP-009": "Microsoft.PolicyInsights checkPolicyRestrictions, or the policy assignment list.",
    "FND-DEP-010": "ARM Microsoft.Authorization locks lists and the deployment history count.",
    "FND-DEP-011": "ARM subnet, serviceAssociationLinks and resourceNavigationLinks reads.",
    "FND-DEP-012": "ARM Subscriptions_ListLocations and Providers_Get resource type locations, plus a dated regional-support data file.",
    "FND-IDN-002": "Effective permission calculation over role definitions, deny assignments, conditions and Cosmos SQL role assignments (control plane).",
    "FND-IDN-005": "Live role assignments and PIM schedule instances at subscription, resource group, account and project scope.",
    "FND-IQ-001": "Azure AI Search data-plane reads of the knowledge source and index definitions.",
    "FND-IQ-002": "Azure AI Search data-plane read of the index definition.",
    "FND-IQ-003": "Azure AI Search data-plane read of the index definition.",
    "FND-IQ-004": "Search data-plane reads (index, vectorizer, skillset) joined with the account deployment (control plane).",
    "FND-IQ-005": "Search data-plane reads of the knowledge base and sources, plus the Search service resource and API version.",
    "FND-IQ-006": "Search data-plane read of the skillset JSON and project query definitions.",
    "FND-IQ-007": "Search data-plane reads of knowledge sources and knowledge base models.",
    "FND-IQ-008": "Search data-plane reads of connection strings and parameters (classification only), plus the service identity.",
    "FND-IQ-009": "Search data-plane reads of ingestion permission options, and a project requirement declaration.",
    "FND-IQ-010": "Needs the storage account behind the knowledge source connectionString (data plane) and its isHnsEnabled value.",
    "FND-IQ-011": "Search data-plane read of ingestionSchedule and indexer schedules.",
    "FND-NET-006": "Needs address spaces of peered virtual networks (control plane, live mode).",
    "FND-OPS-002": "Lists deployed project connections through the control plane and needs the profile observability requirement.",
    "FND-RUN-001": "Runtime probes: project, capability host, connections, role assignments and Cosmos SQL role assignments.",
    "FND-RUN-002": "DNS resolution from an operator-declared in-VNet vantage point plus private DNS zone link reads.",
    "FND-RUN-003": "ARM reads of project, capability host and connection provisioning state.",
    "FND-RUN-004": "ARM deployment provisioning state and Azure Monitor metrics.",
    "FND-RUN-005": "Foundry project endpoint data-plane reads of agents and connections.",
    "FND-RUN-006": "Azure AI Search data-plane reads of index, statistics and indexer status.",
    "FND-RUN-007": "Diagnostic settings, Azure Monitor metrics and a Log Analytics query.",
    "FND-RUN-008": "Deployed resource GET or what-if compared with the compiled template; the catalogue says this is a documented placeholder.",
    "FND-SEC-008": "GET Microsoft.Security/pricings at subscription scope (Defender plans).",
    "FND-SEC-009": "Policy assignments and PolicyInsights policy states at subscription and resource group scope.",
}

# id -> (reason, how to feed it)
INPUT = {
    "FND-CFG-001": ("Needs the azd azure.yaml v1.0 JSON schema and the azure.ai.* extension schemas (vendored at a pinned revision) and a YAML parser that reports duplicate keys. PSRule does neither.", "Run a schema validator and a duplicate-key-aware parser outside PSRule."),
    "FND-CFG-004": ("Needs file-system checks (project directory, Dockerfile) and the documented container resource bounds. PSRule reads the azure.yaml object only.", "None implemented."),
    "FND-CFG-005": ("Needs .env tracking state in git and the azure.yaml credential shapes by authType. The catalogue maps the Bicep side to other tools.", "For the Bicep side run PSRule for Azure Azure.Deployment.* rules and the Bicep linter (listed by the catalogue overlap research)."),
    "FND-CFG-006": ("Needs the selected .azure/<env>/.env keys and compiled template outputs.", "Convert .env to JSON, for example: Get-Content .azure/dev/.env | ConvertFrom-StringData | ConvertTo-Json > env.dev.json. No rule consumes the file yet."),
    "FND-CFG-007": ("The URL-typed fields are described by description only and their azure.yaml paths are not carried in the catalogue evidence.", "None implemented."),
    "FND-CFG-009": ("The placement of the recurring trigger in azure.yaml is a preview shape not carried in the catalogue evidence; a five-field cron parser with firing simulation is also needed.", "None implemented."),
    "FND-COST-003": ("Needs effective API Management policy XML across global, product, API and operation scope, with include-fragment resolution.", "None implemented."),
    "FND-ENV-001": ("Needs every .azure/<env>/.env file in the workspace.", "Convert each .env to JSON (see FND-CFG-006) and add a rule over the set. Not implemented."),
    "FND-ENV-002": ("Needs per-environment .env and parameter files compared by key.", "Same as FND-ENV-001."),
    "FND-ENV-003": ("Needs the environment marked prod in configuration and the .env values of every other environment.", "Same as FND-ENV-001."),
    "FND-ENV-004": ("Needs two or more .env files. Informational difference summary with no pass or fail outcome.", "Same as FND-ENV-001."),
    "FND-GW-001": ("Needs API Management policy XML (validate-jwt, validate-azure-ad-token) resolved with <base /> inheritance.", "None implemented."),
    "FND-GW-002": ("Needs effective API Management policy XML (llm-token-limit, llm-emit-token-metric).", "None implemented."),
    "FND-GW-003": ("Needs policy XML set-backend-service values correlated with backend resources.", "None implemented."),
    "FND-GW-004": ("Needs all APIs of one service compared (path uniqueness across resources and version sets). PSRule evaluates one object at a time.", "Needs a resource-set wrapper object; not implemented."),
    "FND-GW-005": ("Needs policy XML (authentication-managed-identity, set-header) plus service identity and role assignment correlation.", "None implemented."),
    "FND-GW-006": ("Needs policy XML plus loggers and diagnostics correlation.", "None implemented."),
    "FND-IDN-004": ("Needs role assignments correlated with the identities of Foundry accounts, projects and workloads, and scope evaluation.", "Needs a resource-set wrapper object; not implemented."),
    "FND-NET-002": ("Needs capabilitySettings resolution and private endpoints correlated with Storage, Cosmos DB and Search resources.", "Needs a resource-set wrapper object; not implemented."),
    "FND-NET-003": ("Needs private endpoints, DNS zone groups, zones and virtual network links correlated.", "Needs a resource-set wrapper object; not implemented."),
    "FND-NET-004": ("Needs the account networkInjections subnet resolved to the subnet resource and other accounts compared.", "Needs a resource-set wrapper object; not implemented."),
    "FND-NET-005": ("Needs the account, subnet, virtual network and private endpoint resources correlated.", "Needs a resource-set wrapper object; not implemented."),
    "FND-NET-007": ("The catalogue skips this rule when an approved private endpoint targets the account; that needs resource correlation. Without it the rule would report an intentional private-only posture as a failure.", "Needs a resource-set wrapper object; not implemented."),
    "FND-NET-008": ("Needs private endpoints and capabilitySettings correlated with the Search service.", "Needs a resource-set wrapper object; not implemented."),
    "FND-NET-010": ("Needs Application Insights, Azure Monitor private link scopes, scoped resources and private endpoints correlated, plus a profile declaration.", "Needs a resource-set wrapper object; not implemented."),
    "FND-NET-011": ("Needs virtual network subnets, NSG associations, public IPs and DDoS plans correlated.", "Needs a resource-set wrapper object; not implemented."),
    "FND-OPS-001": ("Needs diagnostic settings correlated with the resources they scope, and workspaces.", "Needs a resource-set wrapper object; not implemented."),
    "FND-OPS-003": ("Needs alert rules, scopes and action groups correlated.", "Needs a resource-set wrapper object; not implemented."),
    "FND-OPS-006": ("Needs workspaces, tables, diagnostic settings and a profile minimum retention.", "Needs a resource-set wrapper object and a profile value; not implemented."),
    "FND-OPS-007": ("Needs the pipeline files under .github/workflows, .azdo/pipelines and .azuredevops/pipelines from the git root, and shell command tokenising.", "None implemented."),
    "FND-OPS-008": ("Needs eval.yaml and pipeline files, and a step ordering check.", "None implemented."),
    "FND-OPS-009": ("Needs a declared data-residency requirement (scope and allowed regions) from the profile; without it the catalogue skips the rule.", "A PSRule configuration value could carry it; not implemented."),
    "FND-OPS-010": ("Needs profile allow and deny lists and the policy assignment parameters.", "A PSRule configuration value could carry the lists; not implemented."),
    "FND-REL-004": ("Needs locks (extension resources with a scope) correlated with the stateful resources.", "Needs a resource-set wrapper object; not implemented."),
    "FND-REL-007": ("No pass or fail evidence exists; the answer is a declared disaster recovery strategy.", "None."),
    "FND-REL-009": ("Evaluated only against a resilience objective declared in the profile; without it the catalogue skips the rule.", "A PSRule configuration value could carry it; not implemented."),
    "FND-SEC-007": ("Needs raiPolicies resources correlated with deployments, and azure.yaml policies entries. The catalogue notes the two Foundry articles conflict.", "Needs a resource-set wrapper object; not implemented."),
    "FND-SEC-012": ("The ARM resource type name of the API Management diagnostic resource is not stated in the catalogue evidence, and the API-level check needs correlation with APIs that front a Foundry backend.", "None implemented."),
    "FND-SEC-013": ("Needs API Management policy XML at service, product, API or operation scope.", "None implemented."),
    "FND-SEC-014": ("Catalogue decision is wrap of the Bicep linter diagnostic outputs-should-not-contain-secrets. It needs the Bicep CLI diagnostics, not an ARM property.", "Run bicep build and read the diagnostic, or use PSRule for Azure Azure.Deployment.OutputSecretValue (listed as related by the FND-CFG-005 research; equivalence is not verified here)."),
}

for _k in INPUT:
    CLS[_k] = "inp"
for _k in LIVE:
    CLS[_k] = "live"
