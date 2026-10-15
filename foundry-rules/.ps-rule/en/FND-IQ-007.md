# FND-IQ-007

## SYNOPSIS

Knowledge source type/API and planning model supported

## DESCRIPTION

Each knowledge source kind must exist in the Search API version the project targets, and the planning model on the knowledge base must be one of the documented Azure OpenAI models for that API version (kind azureOpenAI). Query hints that contain a filter are rejected with HTTP 400 when the planning model is a GPT-4o or GPT-4.1 family model (and effort is not minimal).

## RECOMMENDATION

Target the API version that defines the knowledge source kind you use (most non-blob kinds are preview) and deploy a documented planning model for that version; prefer a gpt-5 family model, which also allows stored filter hints.

## EXAMPLES

```
"models": [ { "kind": "azureOpenAI",
  "azureOpenAIParameters": { "resourceUri": "https://<account>.openai.azure.com",
    "deploymentId": "gpt-5.4-mini", "modelName": "gpt-5.4-mini" } } ]
```

## NOTES

- Severity dev/test/prod: warning/warning/warning (PSRule: Warning/Warning/Warning).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Search data-plane reads of knowledge sources and knowledge base models.
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://learn.microsoft.com/azure/search/agentic-retrieval-how-to-create-knowledge-base](https://learn.microsoft.com/azure/search/agentic-retrieval-how-to-create-knowledge-base) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/agentic-knowledge-source-overview](https://learn.microsoft.com/azure/search/agentic-knowledge-source-overview) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/agentic-knowledge-source-how-to-search-index](https://learn.microsoft.com/azure/search/agentic-knowledge-source-how-to-search-index) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/search/data-plane/Search](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/search/data-plane/Search) (last verified 2026-10-04)
