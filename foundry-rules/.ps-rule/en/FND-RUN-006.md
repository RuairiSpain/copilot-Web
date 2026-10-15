# FND-RUN-006

## SYNOPSIS

Search index/documents/vector/indexer health

## DESCRIPTION

For each Azure AI Search index that a Foundry project connection, agent tool or knowledge source references, the index must exist, expose the expected schema (including vector fields with dimensions and a vector search profile), contain at least one document, and any indexer that feeds it must not be in an error state or have a failed lastResult. Only data-plane metadata is read: index definition, index statistics and indexer status. Documents are never queried or returned.

## RECOMMENDATION

Create or repair the index with the expected schema, run the indexer and review its execution errors, and confirm the agent's search connection and index name match. Grant the doctor identity Search Service Contributor only if you want schema and indexer checks.

## EXAMPLES

```
// Expected vector field shape in the index definition (REST, api-version 2026-04-01)
{
  "name": "contentVector",
  "type": "Collection(Edm.Single)",
  "dimensions": 1536,
  "vectorSearchProfile": "default-profile"
}
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Azure AI Search data-plane reads of index, statistics and indexer status.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- The catalogue notes contain unverified items; see the catalogue entry notes field.
- Related PSRule for Azure rules from the catalogue research: Azure.Search.IndexSLA, Azure.Search.QuerySLA.

## LINKS

- [https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/data-plane/Search/stable/2026-04-01/search.json](https://github.com/Azure/azure-rest-api-specs/blob/main/specification/search/data-plane/Search/stable/2026-04-01/search.json) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/search-security-rbac](https://learn.microsoft.com/azure/search/search-security-rbac) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/how-to/tools/ai-search](https://learn.microsoft.com/azure/foundry/agents/how-to/tools/ai-search) (last verified 2026-10-04)
