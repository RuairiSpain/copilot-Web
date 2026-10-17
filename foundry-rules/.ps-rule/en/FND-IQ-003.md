# FND-IQ-003

## SYNOPSIS

Vector field type, dimensions and profile

## DESCRIPTION

Each vector field must use a documented vector data type, declare a dimensions value within the service limit (4096) and within the range of the embedding model that produces it, and reference a vector search profile that exists in vectorSearch.profiles and itself references an existing algorithm (and vectorizer or compression when named).

## RECOMMENDATION

Use Collection(Edm.Single) (or a documented narrow type), set dimensions to the value the embedding model emits, and set vectorSearchProfile to a profile defined under vectorSearch.profiles.

## EXAMPLES

```
{
  "name": "page_chunk_vector",
  "type": "Collection(Edm.Single)",
  "searchable": true, "retrievable": false, "stored": false,
  "dimensions": 3072,
  "vectorSearchProfile": "hnsw_text_3_large"
}
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Azure AI Search data-plane read of the index definition.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://learn.microsoft.com/azure/search/vector-search-how-to-create-index](https://learn.microsoft.com/azure/search/vector-search-how-to-create-index) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/search-limits-quotas-capacity](https://learn.microsoft.com/azure/search/search-limits-quotas-capacity) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/data-plane/Search/stable/2026-04-01/search.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/data-plane/Search/stable/2026-04-01/search.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/vector-search-how-to-assign-narrow-data-types](https://learn.microsoft.com/azure/search/vector-search-how-to-assign-narrow-data-types) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/vector-search-vectorizer-azure-open-ai](https://learn.microsoft.com/azure/search/vector-search-vectorizer-azure-open-ai) (last verified 2026-10-04)
