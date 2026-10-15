# FND-IQ-005

## SYNOPSIS

Retrieval mode supported by index and service tier

## DESCRIPTION

The knowledge base retrieval configuration (retrievalReasoningEffort, outputMode, models) must be supported by the Search API version in use, by the knowledge sources it includes, and by the Search service tier and region; and the index behind an indexed knowledge source must have the semantic configuration that every effort level relies on.

## RECOMMENDATION

Choose a retrieval reasoning effort and output mode that the target API version, the knowledge sources and the service tier support: use minimal with extractiveData on 2026-04-01, add a model and use the 2026-08-01-preview API for auto, and keep knowledge sources within the tier limits.

## EXAMPLES

```
{
  "name": "my-kb",
  "knowledgeSources": [ { "name": "hotels-ks" } ],
  "retrievalReasoningEffort": { "kind": "minimal" },
  "outputMode": "extractiveData"
}
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Search data-plane reads of the knowledge base and sources, plus the Search service resource and API version.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://learn.microsoft.com/azure/search/agentic-retrieval-how-to-set-retrieval-reasoning-effort](https://learn.microsoft.com/azure/search/agentic-retrieval-how-to-set-retrieval-reasoning-effort) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/agentic-retrieval-how-to-create-knowledge-base](https://learn.microsoft.com/azure/search/agentic-retrieval-how-to-create-knowledge-base) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/search-limits-quotas-capacity](https://learn.microsoft.com/azure/search/search-limits-quotas-capacity) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/search/data-plane/Search](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/search/data-plane/Search) (last verified 2026-10-04)
