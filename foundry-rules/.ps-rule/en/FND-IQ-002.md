# FND-IQ-002

## SYNOPSIS

Required key/content/title/vector/semantic fields

## DESCRIPTION

An index used for agentic retrieval must contain a string document key, at least one searchable and retrievable string content field, and a semantic configuration whose prioritizedContentFields name such a field (via defaultSemanticConfiguration or the knowledge source semanticConfigurationName). Any vector field must be searchable, non-filterable, non-facetable, non-sortable and reference a vector search profile. Title and citation fields are documented as optional or recommended and are not failure conditions.

## RECOMMENDATION

Add a string key field, a searchable and retrievable content field, and a semantic configuration that lists the content field under prioritizedContentFields; set it as the index default or reference it from the knowledge source.

## EXAMPLES

```
"fields": [
  { "name": "id", "type": "Edm.String", "key": true, "searchable": false, "retrievable": true, "filterable": true },
  { "name": "page_chunk", "type": "Edm.String", "searchable": true, "retrievable": true }
],
"semantic": {
  "defaultConfiguration": "semantic_config",
  "configurations": [
    { "name": "semantic_config",
      "prioritizedFields": { "prioritizedContentFields": [ { "fieldName": "page_chunk" } ] } }
  ]
}
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Azure AI Search data-plane read of the index definition.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://learn.microsoft.com/azure/search/agentic-retrieval-how-to-create-index](https://learn.microsoft.com/azure/search/agentic-retrieval-how-to-create-index) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/vector-search-how-to-create-index](https://learn.microsoft.com/azure/search/vector-search-how-to-create-index) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/agentic-knowledge-source-how-to-search-index](https://learn.microsoft.com/azure/search/agentic-knowledge-source-how-to-search-index) (last verified 2026-10-04)
