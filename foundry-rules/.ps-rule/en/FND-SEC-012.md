# FND-SEC-012

## SYNOPSIS

Gateway telemetry avoids raw prompt/completion logging

## DESCRIPTION

An API Management AI gateway should not persist raw prompts and completions in logs unless that is a deliberate, suppressed decision. The rule reads the diagnostic largeLanguageModel.requests.messages and responses.messages settings (value all, maxSizeInBytes) and the frontend/backend request/response body.bytes settings of Microsoft.ApiManagement service and API diagnostics.

## RECOMMENDATION

Do not enable LLM message logging in production unless required; if required, restrict workspace access, set retention, apply data masking and record a suppression with reason and expiry. Prefer token-usage logging (largeLanguageModel.logs enabled) which does not log message content.

## EXAMPLES

```
resource diag 'Microsoft.ApiManagement/service/apis/diagnostics@2025-09-01-preview' = {
  parent: api
  name: 'azuremonitor'
  properties: {
    loggerId: loggerId
    largeLanguageModel: { logs: 'enabled' } // token usage only; no requests/responses message logging
  }
}
```

## NOTES

- Severity dev/test/prod: info/warning/error (PSRule: Information/Warning/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: The ARM resource type name of the API Management diagnostic resource is not stated in the catalogue evidence, and the API-level check needs correlation with APIs that front a Foundry backend.
- How to feed it: None implemented.
- This rule never reports Pass in PSRule.

## LINKS

- [https://learn.microsoft.com/azure/api-management/api-management-howto-llm-logs](https://learn.microsoft.com/azure/api-management/api-management-howto-llm-logs) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement/preview/2025-09-01-preview/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement/preview/2025-09-01-preview/openapi.json) (last verified 2026-10-04)
