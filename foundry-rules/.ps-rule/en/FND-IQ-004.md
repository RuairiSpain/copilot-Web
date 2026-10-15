# FND-IQ-004

## SYNOPSIS

Vector dimensions match embedding deployment metadata

## DESCRIPTION

The dimensions of each vector field must equal the dimensions that the embedding model actually emits: the dimensions set on the integrated-vectorization embedding skill (when one is used) and a value inside the range of the model behind the Azure OpenAI deployment named by the field's vectorizer. The vectorizer modelName must also match the model recorded on the Foundry or Azure OpenAI deployment that deploymentId points to.

## RECOMMENDATION

Set the vector field dimensions to the value the embedding skill or model returns, keep vectorizer modelName identical to the deployed model, and rebuild the index if dimensions must change.

## EXAMPLES

```
// skill and field must agree
{ "@odata.type": "#Microsoft.Skills.Text.AzureOpenAIEmbeddingSkill",
  "deploymentId": "text-embedding-3-large", "modelName": "text-embedding-3-large", "dimensions": 1024 }
{ "name": "contentVector", "type": "Collection(Edm.Single)", "dimensions": 1024, "vectorSearchProfile": "hnsw_text_3_large" }
```

## NOTES

- Severity dev/test/prod: error/error/error (PSRule: Error/Error/Error).
- Catalogue status: verified. Catalogue decision: native.
- Evaluation class: needs-live. Status: not-evaluated.
- Not evaluated by PSRule: Search data-plane reads (index, vectorizer, skillset) joined with the account deployment (control plane).
- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.

## LINKS

- [https://learn.microsoft.com/azure/search/vector-search-how-to-create-index](https://learn.microsoft.com/azure/search/vector-search-how-to-create-index) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/cognitive-search-skill-azure-openai-embedding](https://learn.microsoft.com/azure/search/cognitive-search-skill-azure-openai-embedding) (last verified 2026-10-04)
- [https://learn.microsoft.com/azure/search/vector-search-vectorizer-azure-open-ai](https://learn.microsoft.com/azure/search/vector-search-vectorizer-azure-open-ai) (last verified 2026-10-04)
- [https://github.com/Azure/azure-rest-api-specs/tree/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices](https://github.com/Azure/azure-rest-api-specs/tree/main/specification/cognitiveservices/resource-manager/Microsoft.CognitiveServices/CognitiveServices) (last verified 2026-10-04)
