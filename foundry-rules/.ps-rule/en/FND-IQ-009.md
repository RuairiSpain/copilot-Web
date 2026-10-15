# FND-IQ-009

## SYNOPSIS

Document-level access control configured when required

## DESCRIPTION

When a project declares that retrieved content is permission-protected, every indexed knowledge source (blob or ADLS Gen2, OneLake, indexed SharePoint) must set ingestionPermissionOptions, the knowledge source must not also set assetStore, the API version must be 2026-08-01-preview, and the agent's MCP connection must forward the end-user token. Without ingestionPermissionOptions the index holds no permission metadata and the service returns results unfiltered regardless of the user token.

## RECOMMENDATION

Recreate the indexed knowledge source with ingestionPermissionOptions that match your source (for example userIds, groupIds, rbacScope for ADLS Gen2), remove assetStore, call with 2026-08-01-preview, and pass the signed-in user's token in the x-ms-query-source-authorization header from the MCP connection.

## EXAMPLES

```
"ingestionParameters": {
  "identity": null,
  "ingestionPermissionOptions": ["userIds", "groupIds", "rbacScope"]
}
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: product-opinion. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Search data-plane reads of ingestion permission options, and a project requirement declaration.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.
- The catalogue notes contain unverified items; see the catalogue entry notes field.

## LINKS

- [https://learn.microsoft.com/azure/search/agentic-retrieval-how-to-retrieve](https://learn.microsoft.com/azure/search/agentic-retrieval-how-to-retrieve) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/agentic-knowledge-source-how-to-blob](https://learn.microsoft.com/azure/search/agentic-knowledge-source-how-to-blob) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/search-document-level-access-overview](https://learn.microsoft.com/azure/search/search-document-level-access-overview) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/how-to/foundry-iq-connect](https://learn.microsoft.com/azure/foundry/agents/how-to/foundry-iq-connect) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/search/data-plane/Search](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/search/data-plane/Search) (last verified 2026-10-04)
