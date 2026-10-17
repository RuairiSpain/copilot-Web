# FND-IQ-001

## SYNOPSIS

Filter fields/claims reference filterable fields

## DESCRIPTION

Every index field that a Foundry IQ search index knowledge source (or its index) uses in a filter must be attributed filterable: the fields named in searchIndexParameters.queryHints.filters[].field, the field identifiers inside searchIndexParameters.baseFilter, and the security (claims) field that holds user or group identities for security-filter trimming. Azure AI Search evaluates filters only over filterable fields, and an existing field cannot be made filterable without adding a field or rebuilding the index.

## RECOMMENDATION

Mark every field used in a filter as filterable. Because an existing field cannot be changed to filterable, add a new filterable field and repopulate it, or rebuild the index, then point the filter hint or base filter at it.

## EXAMPLES

```
{
  "name": "category",
  "type": "Edm.String",
  "searchable": false,
  "retrievable": true,
  "filterable": true
}
// knowledge source
"searchIndexParameters": {
  "searchIndexName": "product-docs-index",
  "queryHints": { "filters": [ { "field": "category", "fieldValues": ["Benefits"] } ] }
}
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Azure AI Search data-plane reads of the knowledge source and index definitions.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://learn.microsoft.com/azure/search/agentic-knowledge-source-how-to-search-index](https://learn.microsoft.com/azure/search/agentic-knowledge-source-how-to-search-index) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/search-filters](https://learn.microsoft.com/azure/search/search-filters) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/search-security-trimming-for-azure-search](https://learn.microsoft.com/azure/search/search-security-trimming-for-azure-search) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/search/data-plane/Search](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/search/data-plane/Search) (last verified 2026-10-04)
