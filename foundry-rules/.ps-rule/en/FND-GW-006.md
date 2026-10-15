# FND-GW-006

## SYNOPSIS

Telemetry sink exists for token tracking

## DESCRIPTION

When token metrics or token logging are intended, a telemetry sink must exist and be wired: an llm-emit-token-metric policy needs an applicationInsights logger, an applicationInsights diagnostic with loggerId and metrics true at the scope of the API, and custom metrics with dimensions enabled on the Application Insights resource; alternatively gateway LLM logs need an Azure Monitor diagnostic setting to a Log Analytics workspace.

## RECOMMENDATION

Create an Application Insights logger, enable the applicationInsights diagnostic with metrics true on the API scope that has llm-emit-token-metric, and enable custom metrics with dimensions on the Application Insights resource; optionally enable the gateway LLM logs diagnostic setting.

## EXAMPLES

```
resource logger 'Microsoft.ApiManagement/service/loggers@2024-05-01' = {
  parent: apim
  name: 'appinsights'
  properties: { loggerType: 'applicationInsights', resourceId: appInsights.id }
}
resource diag 'Microsoft.ApiManagement/service/diagnostics@2024-05-01' = {
  parent: apim
  name: 'applicationinsights'
  properties: { loggerId: logger.id, metrics: true }
}
```

## NOTES

- Severity dev/test/prod: info/warning/error (PSRule: Information/Warning/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-input. Status: not-evaluated.
- Not evaluated by PSRule: Needs policy XML plus loggers and diagnostics correlation.
- How to feed it: None implemented.
- This rule never reports Pass in PSRule.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/api-management/llm-emit-token-metric-policy](https://learn.microsoft.com/azure/api-management/llm-emit-token-metric-policy) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/api-management-howto-app-insights](https://learn.microsoft.com/azure/api-management/api-management-howto-app-insights) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/api-management-howto-llm-logs](https://learn.microsoft.com/azure/api-management/api-management-howto-llm-logs) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement) (last verified 2026-10-04)
