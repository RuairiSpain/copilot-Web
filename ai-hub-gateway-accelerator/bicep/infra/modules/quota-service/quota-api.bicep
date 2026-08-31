/**
 * @module modules/quota-service/quota-api
 * @description Creates a standalone "Quota Override API" in API Management
 *              — the write-back front door for the Power Apps
 *              custom connector (power-platform/quota-connector/) and,
 *              per guides/quota-override-approval.md's "Implementation
 *              status", the fix for the authorization gap flagged in
 *              guides/enterprise-hardening-checklist.md §1.
 *
 *              Composed from this accelerator's own existing generic
 *              ../apim/api.bicep module (the same one that would import
 *              any other OpenAPI-defined API) rather than a new bicep
 *              pattern — same reuse-what-exists posture as
 *              version-api.bicep, which this module's shape mirrors
 *              (standalone, composable, deployable on its own against an
 *              existing APIM instance).
 *
 *              Deployable standalone; wiring into your main orchestration
 *              is left to you, same as pricing-service.bicep and
 *              quota-service.bicep before it.
 */

@description('The name of the API Management service to deploy the Quota Override API to.')
@minLength(1)
param apiManagementName string

@description('The base URL of the quota-service Function App (e.g. https://<app>.azurewebsites.net/api) — from quota-service.bicep\'s functionAppDefaultHostName output.')
@minLength(1)
param quotaServiceBaseUrl string

@description('The relative path (URL suffix) for the Quota Override API in the APIM gateway.')
param quotaApiPath string = 'quota'

@description('Set to true if an APIM subscription key is required alongside the JWT — matches this accelerator\'s own stated default ("API key is always required").')
param subscriptionRequired bool = true

@description('Whether to enable Azure Monitor + Application Insights diagnostics on this API, same as every other API in this accelerator.')
param enableAPIDiagnostics bool = true

var openApiSpec = loadTextContent('./quota-api.openapi.json')
var policyXml = loadTextContent('./quota-api-policy.xml')

module quotaApi '../apim/api.bicep' = {
  name: 'quota-api-deployment'
  params: {
    apiName: 'quota-api'
    apiDispalyName: 'Quota Override API'
    apiDescription: 'Front door for the quota-override/approval mechanism (guides/quota-override-approval.md) — submit and decide operations, backing the Power Apps write-back kit in power-platform/quota-connector/.'
    openApiSpecification: openApiSpec
    policyDocument: policyXml
    serviceName: apiManagementName
    path: quotaApiPath
    serviceUrl: quotaServiceBaseUrl
    subscriptionRequired: subscriptionRequired
    enableAPIDiagnostics: enableAPIDiagnostics
  }
}

output quotaApiId string = quotaApi.outputs.id
output quotaApiPathOut string = quotaApi.outputs.path
