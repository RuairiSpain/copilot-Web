"""PSRule logic per rule. Maintainer data, not a dependency.

Every property path below appears in the evidence of the catalogue entry of the same rule.
tools/verify.py checks that.
"""


def eq(p, v):
    return {"field": p, "equals": v}


def ne(p, v):
    # notEquals on a missing field is not assumed to be true, so absence is listed explicitly.
    return {"anyOf": [{"field": p, "exists": False}, {"field": p, "notEquals": v}]}


def guard(p):
    # Skipped, not passed, when the expanded value is still an unresolved ARM expression.
    return {"not": {"field": p, "startsWith": "["}}


def of(*types):
    return {"anyOf": [{"field": "type", "equals": t} for t in types]}


ACC = "Microsoft.CognitiveServices/accounts"
DEPLOY = "Microsoft.CognitiveServices/accounts/deployments"
SEARCH = "Microsoft.Search/searchServices"
STORAGE = "Microsoft.Storage/storageAccounts"
COSMOS = "Microsoft.DocumentDB/databaseAccounts"
KV = "Microsoft.KeyVault/vaults"
APIM = "Microsoft.ApiManagement/service"

PROVISIONED = ["ProvisionedManaged", "GlobalProvisionedManaged", "DataZoneProvisionedManaged"]

NAME_RULES = [
    (ACC, r"^[A-Za-z0-9][A-Za-z0-9_.-]{1,63}$"),
    (APIM, r"^[A-Za-z]([A-Za-z0-9-]{0,48}[A-Za-z0-9])?$"),
    (STORAGE, r"^[a-z0-9]{3,24}$"),
    (COSMOS, r"^[a-z0-9][a-z0-9-]{2,43}$"),
    (KV, r"^(?!.*--)[A-Za-z][A-Za-z0-9-]{1,22}[A-Za-z0-9]$"),
    ("Microsoft.ContainerRegistry/registries", r"^[A-Za-z0-9]{5,50}$"),
    ("Microsoft.OperationalInsights/workspaces", r"^[A-Za-z0-9][A-Za-z0-9-]{2,61}[A-Za-z0-9]$"),
    ("Microsoft.Insights/components", r"^[^%&\\?/\x00-\x1F]{0,259}[^%&\\?/\x00-\x1F .]$"),
    ("Microsoft.App/containerApps", r"^[a-z][a-z0-9-]{0,30}[a-z0-9]$"),
    (SEARCH, r"^(?!.*--)(?!-)(?!.-)[a-z0-9-]{2,60}(?<!-)$"),
]

# id -> (types, where, condition)
YAML = {
    "FND-SEC-001": ([ACC], {"allOf": [eq("kind", "AIServices"), guard("properties.disableLocalAuth")]},
                    eq("properties.disableLocalAuth", True)),
    "FND-SEC-002": ([SEARCH], guard("properties.disableLocalAuth"), eq("properties.disableLocalAuth", True)),
    "FND-SEC-003": ([COSMOS], guard("properties.disableLocalAuth"), eq("properties.disableLocalAuth", True)),
    "FND-SEC-004": ([STORAGE], None, {"allOf": [
        eq("properties.allowSharedKeyAccess", False),
        ne("properties.allowBlobPublicAccess", True),
        ne("properties.supportsHttpsTrafficOnly", False),
        {"field": "properties.minimumTlsVersion", "exists": True},
        {"field": "properties.minimumTlsVersion", "notIn": ["TLS1_0", "TLS1_1"]},
    ]}),
    "FND-SEC-005": ([KV], None, {"allOf": [
        ne("properties.enableSoftDelete", False),
        eq("properties.enablePurgeProtection", True),
        eq("properties.enableRbacAuthorization", True),
    ]}),
    "FND-SEC-010": ([ACC, SEARCH, COSMOS, STORAGE], None, {"anyOf": [
        {"allOf": [eq("type", ACC), eq("properties.encryption.keySource", "Microsoft.KeyVault")]},
        {"allOf": [eq("type", SEARCH), eq("properties.encryptionWithCmk.enforcement", "Enabled")]},
        {"allOf": [eq("type", COSMOS), {"field": "properties.keyVaultKeyUri", "hasValue": True}]},
        {"allOf": [eq("type", STORAGE), eq("properties.encryption.keySource", "Microsoft.Keyvault")]},
    ]}),
    "FND-SEC-011": ([ACC], eq("kind", "AIServices"), eq("properties.restrictOutboundNetworkAccess", True)),
    "FND-NET-001": ([ACC], {"anyOf": [eq("kind", "AIServices"), eq("kind", "OpenAI")]},
                    {"anyOf": [eq("properties.publicNetworkAccess", "Disabled"),
                               eq("properties.networkAcls.defaultAction", "Deny")]}),
    "FND-NET-009": ([APIM], {"field": "properties.virtualNetworkType", "in": ["External", "Internal"]},
                    {"field": "sku.name", "in": ["Developer", "Premium"]}),
    "FND-REL-001": ([SEARCH], None, {"anyOf": [eq("sku.name", "free"),
                                               {"field": "properties.replicaCount", "greaterOrEquals": 2}]}),
    "FND-REL-002": ([STORAGE], None, {"allOf": [{"field": "sku.name", "exists": True},
                                                {"field": "sku.name", "notIn": ["Standard_LRS", "Premium_LRS"]}]}),
    "FND-REL-006": (["Microsoft.ApiManagement/service/backends"],
                    {"field": "properties.url",
                     "match": r"(openai\.azure\.com|services\.ai\.azure\.com|cognitiveservices\.azure\.com)"},
                    {"field": "properties.circuitBreaker.rules", "hasValue": True}),
    "FND-REL-008": ([DEPLOY], {"field": "sku.name", "in": PROVISIONED},
                    {"field": "properties.spilloverDeploymentName", "hasValue": True}),
    "FND-COST-002": ([DEPLOY, SEARCH, APIM], None, {"not": {"anyOf": [
        {"allOf": [eq("type", DEPLOY), {"field": "sku.name", "in": PROVISIONED}]},
        {"allOf": [eq("type", SEARCH), {"anyOf": [
            {"field": "sku.name", "in": ["standard2", "standard3", "storage_optimized_l1", "storage_optimized_l2"]},
            {"field": "properties.replicaCount", "greater": 1},
            {"field": "properties.partitionCount", "greater": 1}]}]},
        {"allOf": [eq("type", APIM), {"anyOf": [
            {"field": "sku.name", "in": ["Premium", "Isolated"]},
            {"field": "sku.capacity", "greater": 1}]}]},
    ]}}),
    "FND-CFG-010": ([DEPLOY], None, {"allOf": [
        {"field": "properties.model.version", "hasValue": True},
        ne("properties.model.version", "latest"),
        ne("properties.versionUpgradeOption", "OnceNewDefaultVersionAvailable"),
    ]}),
    "FND-IDN-001": ([ACC, ACC + "/projects"], guard("identity.type"), {"allOf": [
        {"field": "identity.type", "hasValue": True},
        {"field": "identity.type", "notEquals": "None"},
        {"anyOf": [{"field": "identity.type", "notMatch": "UserAssigned"},
                   {"field": "identity.userAssignedIdentities", "hasValue": True}]},
    ]}),
    "FND-IDN-003": (["Microsoft.Authorization/roleAssignments"], None,
                    {"field": "properties.principalType", "hasValue": True}),
    "FND-IDN-006": (["Microsoft.ManagedIdentity/userAssignedIdentities/federatedIdentityCredentials"],
                    {"allOf": [guard("properties.issuer"), guard("properties.subject")]}, {"allOf": [
                        {"field": "properties.issuer", "match": "^https://[^/]+"},
                        {"field": "properties.subject", "hasValue": True},
                        {"field": "properties.subject", "notMatch": r"[*?]"},
                    ]}),
    "FND-IQ-012": ([SEARCH], None, {"not": {"allOf": [
        eq("sku.name", "free"),
        {"anyOf": [eq("properties.semanticSearch", "standard"), eq("properties.knowledgeRetrieval", "standard")]},
    ]}}),
    "FND-OPS-005": ([t for t, _ in NAME_RULES], guard("name"), {"anyOf": [
        {"allOf": [eq("type", t), {"field": "name", "match": rx}]} for t, rx in NAME_RULES]}),
}

# id -> (rule parameters, body). Bodies return a boolean.
PS = {}

PS["FND-CFG-002"] = ("-If { $null -ne $TargetObject.services }", r"""
$agents = @($TargetObject.services.PSObject.Properties | Where-Object {
    $_.Value.host -eq 'azure.ai.agent' -and $_.Value.project -and $_.Value.name -and
    "$($_.Value.project)$($_.Value.name)" -notlike '*${*'
})
@($agents | Group-Object { "$($_.Value.project)|$($_.Value.name)" } | Where-Object { $_.Count -gt 1 }).Count -eq 0
""")

PS["FND-CFG-003"] = ("-If { $null -ne $TargetObject.services }", r"""
$hosts = @{}
foreach ($s in $TargetObject.services.PSObject.Properties) { $hosts[$s.Name] = $s.Value.host }
$bad = @(foreach ($s in $TargetObject.services.PSObject.Properties) {
    $v = $s.Value
    if ($v.host -eq 'azure.ai.agent') {
        foreach ($t in @($v.toolboxes)) {
            if ($t -and "$t" -notlike '*${*' -and ($hosts["$t"] -ne 'azure.ai.toolbox' -or @($v.uses) -notcontains $t)) { "$($s.Name)>$t" }
        }
        foreach ($c in @($v.connections)) {
            if ($c -and "$c" -notlike '*${*' -and $hosts["$c"] -ne 'azure.ai.connection') { "$($s.Name)>$c" }
        }
    }
    if ($v.host -eq 'azure.ai.toolbox') {
        foreach ($tool in @($v.tools)) {
            $c = $tool.connection
            if ($c -and "$c" -notlike '*${*' -and $hosts["$c"] -ne 'azure.ai.connection') { "$($s.Name)>$c" }
        }
    }
})
$bad.Count -eq 0
""")

PS["FND-CFG-008"] = ("-If { $null -ne $TargetObject.services }", r"""
$bad = @(foreach ($s in $TargetObject.services.PSObject.Properties) {
    foreach ($t in @($s.Value.tools)) {
        if ($t.type -eq 'mcp' -and $null -ne $t.PSObject.Properties['allowed_tools'] -and @($t.allowed_tools).Count -eq 0) { $s.Name }
    }
})
$bad.Count -eq 0
""")

PS["FND-CFG-011"] = ("-If { $null -ne $TargetObject.services }", r"""
$bad = @(foreach ($s in $TargetObject.services.PSObject.Properties) {
    $v = $s.Value
    if ($v.host -eq 'azure.ai.agent' -and $null -ne $v.PSObject.Properties['config']) { $s.Name }
    if ($v.host -eq 'microsoft.foundry') { $s.Name }
    if ($v.kind -eq 'prompt-voice') { $s.Name }
    if ($v.host -eq 'azure.ai.agent' -and $v.kind -eq 'hosted') {
        foreach ($p in 'displayName', 'inputSchema', 'outputSchema') { if ($null -ne $v.PSObject.Properties[$p]) { $s.Name } }
    }
    if ($null -ne $v.env) {
        foreach ($k in $v.env.PSObject.Properties.Name) { if ($k -clike 'FOUNDRY_*' -or $k -clike 'AGENT_*') { $s.Name } }
    }
})
$bad.Count -eq 0
""")

PS["FND-OPS-004"] = (
    "-Type 'Microsoft.CognitiveServices/accounts','Microsoft.CognitiveServices/accounts/projects',"
    "'Microsoft.Search/searchServices','Microsoft.Storage/storageAccounts',"
    "'Microsoft.DocumentDB/databaseAccounts','Microsoft.ApiManagement/service' "
    "-If { @($Configuration.FOUNDRY_REQUIRED_TAGS).Count -gt 0 -and $TargetObject.tags -isnot [string] }",
    r"""
$required = @($Configuration.FOUNDRY_REQUIRED_TAGS)
$tags = @(if ($TargetObject.tags) { $TargetObject.tags.PSObject.Properties })
$maxName = if ($TargetObject.type -eq 'Microsoft.Storage/storageAccounts') { 128 } else { 512 }
$missing = @($required | Where-Object { $tags.Name -notcontains $_ })
$tooLong = @($tags | Where-Object { $_.Name.Length -gt $maxName -or "$($_.Value)".Length -gt 256 })
$missing.Count -eq 0 -and $tooLong.Count -eq 0 -and $tags.Count -le 50
""")

PS["FND-REL-003"] = (
    "-Type 'Microsoft.DocumentDB/databaseAccounts' -If { $TargetObject.properties.locations -isnot [string] }",
    r"""
$locations = @($TargetObject.properties.locations)
$zones = $locations.Count -gt 0 -and @($locations | Where-Object { $_.isZoneRedundant -ne $true }).Count -eq 0
$backup = $TargetObject.properties.backupPolicy.type -eq 'Continuous'
$zones -and $backup
""")

PS["FND-REL-005"] = (
    "-Type 'Microsoft.Search/searchServices' -If { $TargetObject.sku.name -notin 'free', 'serverless' -and "
    "$TargetObject.properties.partitionCount -isnot [string] -and $TargetObject.properties.replicaCount -isnot [string] }",
    r"""
$p = $TargetObject.properties
$sku = "$($TargetObject.sku.name)"
$partitions = if ($null -ne $p.partitionCount) { [int]$p.partitionCount } else { 1 }
$replicas = if ($null -ne $p.replicaCount) { [int]$p.replicaCount } else { 1 }
$ok = $partitions -in 1, 2, 3, 4, 6, 12
if ($replicas -gt 12) { $ok = $false }
if ($sku -eq 'basic' -and $replicas -gt 3) { $ok = $false }
if ($sku -ne 'basic' -and $replicas * $partitions -gt 36) { $ok = $false }
if ($p.hostingMode -eq 'HighDensity' -and ($sku -ne 'standard3' -or $partitions -gt 3)) { $ok = $false }
$ok
""")

PS["FND-SEC-006"] = (
    "-Type 'Microsoft.Storage/storageAccounts','Microsoft.DocumentDB/databaseAccounts','Microsoft.KeyVault/vaults',"
    "'Microsoft.Search/searchServices','Microsoft.CognitiveServices/accounts'",
    r"""
$p = $TargetObject.properties
$cosmos = $TargetObject.type -eq 'Microsoft.DocumentDB/databaseAccounts'
$storage = $TargetObject.type -eq 'Microsoft.Storage/storageAccounts'
$values = switch ($TargetObject.type) {
    'Microsoft.Storage/storageAccounts' { @($p.networkAcls.ipRules).value }
    'Microsoft.DocumentDB/databaseAccounts' { @($p.ipRules).ipAddressOrRange }
    'Microsoft.KeyVault/vaults' { @($p.networkAcls.ipRules).value }
    'Microsoft.Search/searchServices' { @($p.networkRuleSet.ipRules).value }
    'Microsoft.CognitiveServices/accounts' { @($p.networkAcls.ipRules).value }
}
$values = @($values | Where-Object { $_ })
$open = @($values | Where-Object { $_ -eq '0.0.0.0/0' -or ($cosmos -and $_ -eq '0.0.0.0') })
$private = @()
if ($storage -or $cosmos) {
    $private = @($values | Where-Object { $_ -match '^(10\.|192\.168\.|172\.(1[6-9]|2[0-9]|3[01])\.)' })
}
if ($cosmos) {
    $private += @($values | Where-Object { $_ -match '^100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\.' })
}
$open.Count -eq 0 -and $private.Count -eq 0
""")
