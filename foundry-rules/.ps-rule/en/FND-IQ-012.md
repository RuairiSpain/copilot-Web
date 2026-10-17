# FND-IQ-012

## SYNOPSIS

Semantic ranking enabled when used

## DESCRIPTION

When a knowledge base, knowledge source or query uses semantic ranking (agentic retrieval always applies L2 semantic reranking, and a semantic configuration is required), the Search service must not have semantic ranker disabled. On management API versions up to 2025-11-01-preview semanticSearch disabled turns off semantic ranker and paid agentic retrieval; from 2026-03-01-preview disabled is no longer a valid value and is treated as free. The standard plan requires the Basic tier or higher.

## RECOMMENDATION

Set semanticSearch to free or standard (use standard on Basic or higher for production), and set knowledgeRetrieval to standard if agentic retrieval usage can exceed the free allowance. Use PATCH, not PUT, when changing these properties on an existing service.

## EXAMPLES

```
resource search 'Microsoft.Search/searchServices@2025-05-01' = {
  name: searchName
  location: location
  sku: { name: 'basic' }
  properties: { semanticSearch: 'standard' }
}
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: static-yaml. Status: evaluated-subset.
- Evaluated here: Compiled ARM only: a free Search SKU with semanticSearch or knowledgeRetrieval standard. The 'used' test and the disabled check by API version need data-plane and API-version evidence and are not evaluated. The catalogue notes the ARM TypeSpec and the Learn article disagree about the disabled value.

## LINKS

- [https://learn.microsoft.com/azure/search/semantic-how-to-enable-disable](https://learn.microsoft.com/azure/search/semantic-how-to-enable-disable) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/agentic-retrieval-how-to-enable-disable](https://learn.microsoft.com/azure/search/agentic-retrieval-how-to-enable-disable) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/agentic-retrieval-how-to-create-index](https://learn.microsoft.com/azure/search/agentic-retrieval-how-to-create-index) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/search/resource-manager/Microsoft.Search/Search](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/search/resource-manager/Microsoft.Search/Search) (last verified 2026-10-04)
