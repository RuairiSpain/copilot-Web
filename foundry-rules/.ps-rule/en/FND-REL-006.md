# FND-REL-006

## SYNOPSIS

Gateway retry/circuit breaker

## DESCRIPTION

An API Management instance fronting Foundry or Azure OpenAI model backends should configure resilience for 429 and 5xx responses: a backend circuit breaker rule, and/or a retry policy. Learn recommends circuit breaker rules for Azure OpenAI backends because 429 responses can carry a large Retry-After value. WAF recommendation; the circuitBreaker property exists only in preview API versions.

## RECOMMENDATION

Add a circuit breaker rule to the backend that points at the model endpoint, trip on 429 and 5xx, and let it accept Retry-After. Keep API-level retry for transient errors only, with a bounded count, because API Management circuit breaker rules are approximate across gateway instances.

## EXAMPLES

```
resource backend 'Microsoft.ApiManagement/service/backends@2025-09-01-preview' = {
  parent: apim
  name: 'foundry-backend'
  properties: {
    url: 'https://<foundry-endpoint>/openai'
    protocol: 'http'
    circuitBreaker: {
      rules: [
        {
          name: 'trip-on-throttle-and-errors'
          failureCondition: {
            count: 3
            interval: 'PT1M'
            statusCodeRanges: [ { min: 429, max: 429 }, { min: 500, max: 599 } ]
          }
          tripDuration: 'PT1M'
          acceptRetryAfter: true
        }
      ]
    }
  }
}
```

## NOTES

- Severity dev/test/prod: info/info/warning (PSRule: Information/Information/Warning).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Backends whose properties.url host is openai.azure.com, services.ai.azure.com or cognitiveservices.azure.com: circuitBreaker.rules must be non-empty. Backend pools, retry policy evidence, the Consumption tier and the API-version support check are not evaluated. The catalogue verifies the property only against API 2025-09-01-preview.

## LINKS

- [https://learn.microsoft.com/azure/api-management/backends](https://learn.microsoft.com/azure/api-management/backends) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/api-management/retry-policy](https://learn.microsoft.com/azure/api-management/retry-policy) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement/preview/2025-09-01-preview/openapi.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/apimanagement/resource-manager/Microsoft.ApiManagement/ApiManagement/preview/2025-09-01-preview/openapi.json) (last verified 2026-10-04)
