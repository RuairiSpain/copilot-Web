/**
 * @module policy-fragments
 * @description This module creates all policy fragments for the API Management service.
 * It includes configurations for authentication, routing, usage tracking, and PII handling.
 */

// ------------------
//    PARAMETERS
// ------------------

@description('The name of the API Management service')
param apimServiceName string

@description('Enable PII Anonymization features')
param enablePIIAnonymization bool = true

@description('Enable AI Model Inference features')
// Retained for interface compatibility with callers; not referenced within this module.
#disable-next-line no-unused-params
param enableAIModelInference bool = true

@description('Enable Unified AI API features')
param enableUnifiedAiApi bool = true

// ------------------
//    RESOURCES
// ------------------

resource apimService 'Microsoft.ApiManagement/service@2022-08-01' existing = {
  name: apimServiceName
}

resource raiseThrottlingEventsPolicyFragment 'Microsoft.ApiManagement/service/policyFragments@2022-08-01' = {
  parent: apimService
  name: 'raise-throttling-events'
  properties: {
    description: 'Raises custom events when throttling limits are hit through App Insights metrics, for proactive monitoring and alerting'
    value: loadTextContent('./policies/frag-raise-throttling-events.xml')
    format: 'rawxml'
  }
}

resource raiseAlertEventsPolicyFragment 'Microsoft.ApiManagement/service/policyFragments@2022-08-01' = {
  parent: apimService
  name: 'raise-alert-events'
  properties: {
    description: 'Comprehensive, opt-in alerting: emits App Insights custom metrics for throttling, backend, authorization, content-safety, and PII failures, sliceable per product/model/backend/app'
    value: loadTextContent('./policies/frag-raise-alert-events.xml')
    format: 'rawxml'
  }
}

// Publish Contract usage fragments (Tools/MCP and Agents/A2A). Emit request-count metrics to
// dedicated App Insights namespaces (mcp-usage / a2a-usage) that scheduled Logic Apps aggregate
// into Cosmos. Registered here so a freshly deployed gateway already has them; the
// citadel-publish-contracts deployment also (idempotently) ensures they exist.
resource mcpUsagePolicyFragment 'Microsoft.ApiManagement/service/policyFragments@2022-08-01' = {
  parent: apimService
  name: 'mcp-usage'
  properties: {
    description: 'Tracks usage of published Tools (MCP) as App Insights custom metrics (mcp-usage namespace)'
    value: loadTextContent('./policies/frag-mcp-usage.xml')
    format: 'rawxml'
  }
}

resource a2aUsagePolicyFragment 'Microsoft.ApiManagement/service/policyFragments@2022-08-01' = {
  parent: apimService
  name: 'a2a-usage'
  properties: {
    description: 'Tracks usage of published Agents (A2A) as App Insights custom metrics (a2a-usage namespace)'
    value: loadTextContent('./policies/frag-a2a-usage.xml')
    format: 'rawxml'
  }
}

// Access Contract asset-kind classifier. Lets a single product policy that mixes asset types apply the
// right controls per request (llm / tool / agent). Driven by contractToolApis / contractAgentApis
// variables the product policy sets; defaults to 'llm' so existing LLM-only contracts are unaffected.
resource setAssetKindPolicyFragment 'Microsoft.ApiManagement/service/policyFragments@2022-08-01' = {
  parent: apimService
  name: 'set-asset-kind'
  properties: {
    description: 'Classifies the current request as llm | tool | agent for asset-type-aware access-contract product policies'
    value: loadTextContent('./policies/frag-set-asset-kind.xml')
    format: 'rawxml'
  }
}

resource piiAnonymizationPolicyFragment 'Microsoft.ApiManagement/service/policyFragments@2022-08-01' = {
  parent: apimService
  name: 'pii-anonymization'
  properties: {
    description: 'Anonymizes personally identifiable information (PII) in API requests'
    value: loadTextContent('./policies/frag-pii-anonymization.xml')
    format: 'rawxml'
  }
}

resource piiDenonymizationPolicyFragment 'Microsoft.ApiManagement/service/policyFragments@2022-08-01' = {
  parent: apimService
  name: 'pii-deanonymization'
  properties: {
    description: 'Deanonymizes personally identifiable information (PII) in API responses when needed for backend processing'
    value: loadTextContent('./policies/frag-pii-deanonymization.xml')
    format: 'rawxml'
  }
}

resource piiStateSavingPolicyFragment 'Microsoft.ApiManagement/service/policyFragments@2022-08-01' = if (enablePIIAnonymization) {
  parent: apimService
  name: 'pii-state-saving'
  properties: {
    description: 'Saves the state of personally identifiable information (PII) for testing & validation purposes'
    value: loadTextContent('./policies/frag-pii-state-saving.xml')
    format: 'rawxml'
  }
}

resource aiFoundryCompatibilityPolicyFragment 'Microsoft.ApiManagement/service/policyFragments@2022-08-01' = if (enablePIIAnonymization) {
  parent: apimService
  name: 'ai-foundry-compatibility'
  properties: {
    description: 'Ensures compatibility with Microsoft Foundry CORS requirements'
    value: loadTextContent('./policies/frag-ai-foundry-compatibility.xml')
    format: 'rawxml'
  }
}

// ------------------
//    UNIFIED AI API FRAGMENTS
// ------------------

resource centralCacheManagerFragment 'Microsoft.ApiManagement/service/policyFragments@2024-06-01-preview' = if (enableUnifiedAiApi) {
  parent: apimService
  name: 'central-cache-manager'
  properties: {
    description: 'Caches metadata configuration for Unified AI API performance'
    value: loadTextContent('./policies/frag-central-cache-manager.xml')
    format: 'rawxml'
  }
}

resource requestProcessorFragment 'Microsoft.ApiManagement/service/policyFragments@2024-06-01-preview' = if (enableUnifiedAiApi) {
  parent: apimService
  name: 'request-processor'
  properties: {
    description: 'Analyzes incoming Unified AI requests to extract routing context'
    value: loadTextContent('./policies/frag-request-processor.xml')
    format: 'rawxml'
  }
}

resource pathBuilderFragment 'Microsoft.ApiManagement/service/policyFragments@2024-06-01-preview' = if (enableUnifiedAiApi) {
  parent: apimService
  name: 'path-builder'
  properties: {
    description: 'Reconstructs backend URI paths for Unified AI API routing'
    value: loadTextContent('./policies/frag-path-builder.xml')
    format: 'rawxml'
  }
}

resource securityHandlerFragment 'Microsoft.ApiManagement/service/policyFragments@2024-06-01-preview' = {
  parent: apimService
  name: 'security-handler'
  properties: {
    description: 'Unified authentication handler for all AI Gateway APIs (API Key + optional JWT per-product)'
    value: loadTextContent('./policies/frag-security-handler.xml')
    format: 'rawxml'
  }
}

resource setResponseHeadersFragment 'Microsoft.ApiManagement/service/policyFragments@2024-06-01-preview' = if (enableUnifiedAiApi) {
  parent: apimService
  name: 'set-response-headers'
  properties: {
    description: 'Adds UAIG-* response headers when enableResponseHeaders is true'
    value: loadTextContent('./policies/frag-set-response-headers.xml')
    format: 'rawxml'
  }
}

resource stripBackendHeadersFragment 'Microsoft.ApiManagement/service/policyFragments@2024-06-01-preview' = {
  parent: apimService
  name: 'strip-backend-headers'
  properties: {
    description: 'Removes browser, App Service / ARR, and X-Forwarded-* headers from requests forwarded to AI backends'
    value: loadTextContent('./policies/frag-strip-backend-headers.xml')
    format: 'rawxml'
  }
}

// Resolve Model Alias fragment - shared by all 3 LLM APIs (Azure OpenAI, Universal LLM, Unified AI).
// Initially deployed with an empty inline alias map so policies compile before any aliases are
// onboarded. The llm-backend-onboarding deployment overwrites this fragment with the configured
// modelAliases entries inlined as a static JObject (and also publishes them via metadata-config).
resource resolveModelAliasFragment 'Microsoft.ApiManagement/service/policyFragments@2024-06-01-preview' = {
  parent: apimService
  name: 'resolve-model-alias'
  properties: {
    description: 'Resolves model alias names to actual underlying models with priority/weighted strategy'
    value: replace(loadTextContent('./policies/frag-resolve-model-alias.xml'), '//{inlineAliasesCode}', '')
    format: 'rawxml'
  }
}

// Quota override/approval mechanism (guides/quota-override-approval.md) —
// fork addition, not part of the upstream accelerator. Deployed
// unconditionally (no enableX gate) since an access contract that never
// includes either fragment is completely unaffected — same
// backward-compatibility posture the rest of this module already uses
// for opt-in features.
resource resolveQuotaScopeFragment 'Microsoft.ApiManagement/service/policyFragments@2024-06-01-preview' = {
  parent: apimService
  name: 'resolve-quota-scope'
  properties: {
    description: 'Resolves the per-user/per-team dynamic quota scope (oid or department claim) from the validated JWT, for the tier-2 sub-quota'
    value: loadTextContent('./policies/frag-resolve-quota-scope.xml')
    format: 'rawxml'
  }
}

resource loadQuotaAllowanceFragment 'Microsoft.ApiManagement/service/policyFragments@2024-06-01-preview' = {
  parent: apimService
  name: 'load-quota-allowance'
  properties: {
    description: 'Resolves the tier-2 dynamic sub-quota for the scope from resolve-quota-scope, via quota-service, cached in APIM for 300s, fail-open to the contract baseline on any error'
    value: loadTextContent('./policies/frag-load-quota-allowance.xml')
    format: 'rawxml'
  }
}

// Agent hierarchy attribution (guides/agent-hierarchy-attribution.md) —
// fork addition. Same unconditional-deploy posture as the quota-scope
// fragments above: a request with no x-agent-root-id header is
// completely unaffected (both fragments no-op for ordinary traffic).
resource resolveAgentHierarchyFragment 'Microsoft.ApiManagement/service/policyFragments@2024-06-01-preview' = {
  parent: apimService
  name: 'resolve-agent-hierarchy'
  properties: {
    description: 'Captures x-agent-root-id/x-agent-caller-type/x-agent-depth attribution headers and emits a correlated trace for sub-agent/MCP-tool call chains'
    value: loadTextContent('./policies/frag-resolve-agent-hierarchy.xml')
    format: 'rawxml'
  }
}

resource enforceAgentLimitsFragment 'Microsoft.ApiManagement/service/policyFragments@2024-06-01-preview' = {
  parent: apimService
  name: 'enforce-agent-limits'
  properties: {
    description: 'Depth (log-only until agent-limits-enforce=true) and fan-out (always enforcing, quota-by-key) guards on agent/sub-agent/MCP-tool call chains. Must run after resolve-agent-hierarchy.'
    value: loadTextContent('./policies/frag-enforce-agent-limits.xml')
    format: 'rawxml'
  }
}

// ------------------
//    OUTPUTS
// ------------------

@description('The name of the PII anonymization policy fragment')
output piiAnonymizationPolicyFragmentName string = piiAnonymizationPolicyFragment.name

@description('The name of the PII deanonymization policy fragment')
output piiDenonymizationPolicyFragmentName string = piiDenonymizationPolicyFragment.name
@description('The name of the PII state saving policy fragment')
output piiStateSavingPolicyFragmentName string = enablePIIAnonymization ? piiStateSavingPolicyFragment.name : ''
@description('The name of the AI Foundry compatibility policy fragment')
output aiFoundryCompatibilityPolicyFragmentName string = enablePIIAnonymization ? aiFoundryCompatibilityPolicyFragment.name : ''

@description('The name of the central cache manager policy fragment')
output centralCacheManagerFragmentName string = enableUnifiedAiApi ? centralCacheManagerFragment.name : ''
@description('The name of the request processor policy fragment')
output requestProcessorFragmentName string = enableUnifiedAiApi ? requestProcessorFragment.name : ''
@description('The name of the path builder policy fragment')
output pathBuilderFragmentName string = enableUnifiedAiApi ? pathBuilderFragment.name : ''
@description('The name of the security handler policy fragment')
output securityHandlerFragmentName string = enableUnifiedAiApi ? securityHandlerFragment.name : ''
@description('The name of the set response headers policy fragment')
output setResponseHeadersFragmentName string = enableUnifiedAiApi ? setResponseHeadersFragment.name : ''

@description('The name of the resolve model alias policy fragment')
output resolveModelAliasFragmentName string = resolveModelAliasFragment.name

@description('The name of the resolve quota scope policy fragment (fork addition — guides/quota-override-approval.md)')
output resolveQuotaScopeFragmentName string = resolveQuotaScopeFragment.name
@description('The name of the load quota allowance policy fragment (fork addition — guides/quota-override-approval.md)')
output loadQuotaAllowanceFragmentName string = loadQuotaAllowanceFragment.name
