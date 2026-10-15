# FND-IQ-006

## SYNOPSIS

Chunk overlap and hybrid weights valid

## DESCRIPTION

Chunking and ranking parameters must be inside documented bounds: a Text Split skill with textSplitMode pages must have maximumPageLength between 300 and 50000 characters and, when pageOverlapLength is set, an overlap smaller than half of maximumPageLength; and every vector query weight used in a hybrid query must be a positive number greater than zero.

## RECOMMENDATION

Keep pageOverlapLength below half of maximumPageLength (the docs recommend roughly 10 to 25 percent), keep maximumPageLength between 300 and 50000 characters, and use a vector weight above zero.

## EXAMPLES

```
{ "@odata.type": "#Microsoft.Skills.Text.SplitSkill",
  "textSplitMode": "pages", "maximumPageLength": 2000, "pageOverlapLength": 200 }
"vectorQueries": [ { "kind": "text", "fields": "contentVector", "k": 10, "weight": 2.0 } ]
```

## NOTES

- Severity dev/test/prod: warning/warning/warning (PSRule: Warning/Warning/Warning).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Search data-plane read of the skillset JSON and project query definitions.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://learn.microsoft.com/azure/search/cognitive-search-skill-textsplit](https://learn.microsoft.com/azure/search/cognitive-search-skill-textsplit) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/vector-search-how-to-chunk-documents](https://learn.microsoft.com/azure/search/vector-search-how-to-chunk-documents) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/vector-search-how-to-query](https://learn.microsoft.com/azure/search/vector-search-how-to-query) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/search/data-plane/Search](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/search/data-plane/Search) (last verified 2026-10-04)
