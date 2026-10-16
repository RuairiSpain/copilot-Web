# FND-IQ-008

## SYNOPSIS

Source connection uses managed identity

## DESCRIPTION

Connections that Azure AI Search and Foundry IQ use to reach data sources and models should authenticate with a managed identity instead of a stored key: the knowledge source connectionString should be a ResourceId form (no AccountKey, SharedAccessSignature, password or client secret), vectorizer and knowledge base model parameters should omit apiKey, and the Foundry project connection to the knowledge base MCP endpoint should use authType ProjectManagedIdentity.

## RECOMMENDATION

Enable a system-assigned or user-assigned managed identity on the Search service, grant it the data-plane roles in the knowledge source article (for example Storage Blob Data Reader, Cognitive Services User), use a ResourceId= connection string, and drop apiKey from model parameters.

## EXAMPLES

```
"azureBlobParameters": {
  "connectionString": "ResourceId=/subscriptions/<sub>/resourceGroups/<rg>/providers/Microsoft.Storage/storageAccounts/<account>",
  "containerName": "docs",
  "ingestionParameters": { "identity": null }
}
```

## NOTES

- Severity dev/test/prod: warning/error/error (PSRule: Warning/Error/Error).
- Catalogue status: verified. Catalogue decision: adapt.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Search data-plane reads of connection strings and parameters (classification only), plus the service identity.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.
- Related PSRule for Azure rules from the catalogue research: Azure.Search.ManagedIdentity.

## LINKS

- [https://learn.microsoft.com/azure/search/search-how-to-managed-identities](https://learn.microsoft.com/azure/search/search-how-to-managed-identities) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/agentic-knowledge-source-how-to-blob](https://learn.microsoft.com/azure/search/agentic-knowledge-source-how-to-blob) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/agentic-knowledge-source-how-to-azure-sql](https://learn.microsoft.com/azure/search/agentic-knowledge-source-how-to-azure-sql) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/vector-search-vectorizer-azure-open-ai](https://learn.microsoft.com/azure/search/vector-search-vectorizer-azure-open-ai) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/search-security-rbac](https://learn.microsoft.com/azure/search/search-security-rbac) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/foundry/agents/how-to/foundry-iq-connect](https://learn.microsoft.com/azure/foundry/agents/how-to/foundry-iq-connect) (last verified 2026-10-04)
