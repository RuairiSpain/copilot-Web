"""Schema for the HR templates index.

This index is built by hand rather than generated, and that is the whole point
of the second knowledge source. A blob knowledge source hands index design to
Foundry IQ; a `searchIndex` knowledge source lets you keep it. The demo needs
hybrid retrieval with exact-match metadata filters applied *before* the vector
search, which means owning the field definitions — see README, "What you can
and cannot customise".

Field roles:
  content / content_vector  hybrid retrieval: BM25 over text plus HNSW vectors
  doc_type, language, jurisdiction, publisher, file_format, tags
                            filterable and facetable, so `$filter` narrows the
                            candidate set before vectors are compared
  published_date            filterable and sortable for recency windows

The vector profile declares an Azure OpenAI vectorizer, so the *query* side
never has to embed: callers send text and the service vectorises it with the
same deployment used at ingestion.
"""

from __future__ import annotations

from typing import Any

SEMANTIC_CONFIG = "hr-semantic"
VECTOR_PROFILE = "hnsw-aoai"
VECTORIZER = "aoai-vectorizer"

# Every field a caller may use in an OData `$filter`. The retrieval script and
# the knowledge source's query hints both read this, so the filterable surface
# is declared once.
FILTERABLE_METADATA = ["doc_type", "language", "jurisdiction", "publisher", "file_format", "tags", "published_date"]


def build_index(
    name: str,
    *,
    aoai_endpoint: str,
    embedding_deployment: str,
    embedding_model: str,
    dimensions: int = 3072,
) -> dict[str, Any]:
    return {
        "name": name,
        "fields": [
            {"name": "id", "type": "Edm.String", "key": True, "filterable": True, "retrievable": True},
            {"name": "parent_id", "type": "Edm.String", "filterable": True, "retrievable": True},
            {"name": "title", "type": "Edm.String", "searchable": True, "retrievable": True},
            {
                "name": "content",
                "type": "Edm.String",
                "searchable": True,
                "retrievable": True,
                "analyzer": "standard.lucene",
            },
            {
                "name": "content_vector",
                "type": "Collection(Edm.Single)",
                "searchable": True,
                "retrievable": False,
                "stored": False,
                "dimensions": dimensions,
                "vectorSearchProfile": VECTOR_PROFILE,
            },
            {"name": "source_url", "type": "Edm.String", "filterable": True, "retrievable": True},
            {"name": "doc_type", "type": "Edm.String", "filterable": True, "facetable": True, "retrievable": True},
            {"name": "language", "type": "Edm.String", "filterable": True, "facetable": True, "retrievable": True},
            {"name": "jurisdiction", "type": "Edm.String", "filterable": True, "facetable": True, "retrievable": True},
            {"name": "publisher", "type": "Edm.String", "filterable": True, "facetable": True, "retrievable": True},
            {"name": "file_format", "type": "Edm.String", "filterable": True, "facetable": True, "retrievable": True},
            {
                "name": "tags",
                "type": "Collection(Edm.String)",
                "searchable": True,
                "filterable": True,
                "facetable": True,
                "retrievable": True,
            },
            {
                "name": "published_date",
                "type": "Edm.DateTimeOffset",
                "filterable": True,
                "sortable": True,
                "retrievable": True,
            },
        ],
        "vectorSearch": {
            "algorithms": [
                {
                    "name": "hnsw",
                    "kind": "hnsw",
                    "hnswParameters": {"m": 4, "efConstruction": 400, "efSearch": 500, "metric": "cosine"},
                }
            ],
            "profiles": [{"name": VECTOR_PROFILE, "algorithm": "hnsw", "vectorizer": VECTORIZER}],
            "vectorizers": [
                {
                    "name": VECTORIZER,
                    "kind": "azureOpenAI",
                    "azureOpenAIParameters": {
                        "resourceUri": aoai_endpoint,
                        "deploymentId": embedding_deployment,
                        "modelName": embedding_model,
                    },
                }
            ],
        },
        "semantic": {
            "configurations": [
                {
                    "name": SEMANTIC_CONFIG,
                    "prioritizedFields": {
                        "titleField": {"fieldName": "title"},
                        "prioritizedContentFields": [{"fieldName": "content"}],
                        "prioritizedKeywordsFields": [{"fieldName": "tags"}],
                    },
                }
            ]
        },
    }
