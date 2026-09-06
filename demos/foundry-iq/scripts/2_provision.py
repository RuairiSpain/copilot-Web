#!/usr/bin/env python3
"""Command 2 — build the knowledge base and its two knowledge sources.

    python scripts/2_provision.py --project my-foundry-project

Six steps, each printing the REST payload it sends so the audience can see the
actual Foundry IQ surface rather than an abstraction over it:

  1. create the HR index (hand-built: hybrid + vector + filterable metadata)
  2. push the HR corpus into it, with client-side embeddings
  3. upload the EU directives to blob storage
  4. create the `azureBlob` knowledge source  -> IQ generates the pipeline
  5. create the `searchIndex` knowledge source -> over the index from step 1
  6. create the knowledge base over both, with routing instructions

The asymmetry between steps 4 and 5 is the lesson, not an accident. See the
README section "What you can and cannot customise".
"""

from __future__ import annotations

import argparse
import json
import re
import unicodedata
from pathlib import Path
from typing import Any, Iterable

from azure.identity import DefaultAzureCredential

from _common import API_VERSION, CORPUS_DIR, ConfigError, SearchClient, Settings, dump, embed, fail, load_settings, odata
from index_schema import FILTERABLE_METADATA, SEMANTIC_CONFIG, build_index

CHUNK_CHARS = 2000
CHUNK_OVERLAP = 200
EMBED_BATCH = 16

# Cheap, explainable classification so the demo has metadata worth filtering
# on. A real corpus would carry this from the source system.
DOC_TYPE_PATTERNS = [
    ("contract", r"\b(contract of employment|employment contract|written statement|terms and conditions)\b"),
    ("policy", r"\b(policy|code of conduct|handbook)\b"),
    ("procedure", r"\b(procedure|grievance|disciplinary|process)\b"),
    ("letter", r"\b(letter|notice|invitation to)\b"),
    ("form", r"\b(form|checklist|template|questionnaire)\b"),
    ("guidance", r"\b(guidance|guide|advice|factsheet)\b"),
]

TAG_PATTERNS = [
    ("working-time", r"\b(working time|hours of work|rest break|overtime)\b"),
    ("dismissal", r"\b(dismissal|redundancy|termination|notice period)\b"),
    ("leave", r"\b(annual leave|holiday|parental leave|sick leave|maternity|paternity)\b"),
    ("pay", r"\b(pay|salary|wage|remuneration|bonus)\b"),
    ("equality", r"\b(discriminat|equal treatment|equality|harassment)\b"),
    ("health-safety", r"\b(health and safety|risk assessment|occupational)\b"),
    ("data-protection", r"\b(personal data|gdpr|data protection|privacy)\b"),
]


def slugify(value: str) -> str:
    value = unicodedata.normalize("NFKD", value).encode("ascii", "ignore").decode()
    return re.sub(r"[^a-zA-Z0-9_\-=]+", "_", value).strip("_")[:200] or "doc"


def extract_text(path: Path, fmt: str) -> str:
    """Best-effort text extraction. Unsupported binaries are skipped, loudly."""
    if fmt in {"markdown", "txt", "csv", "html"}:
        return path.read_text(encoding="utf-8", errors="replace")
    if fmt == "pdf":
        try:
            from pypdf import PdfReader  # type: ignore[import-not-found]
        except ImportError:
            print(f"  skip {path.name} — install pypdf to index PDFs")
            return ""
        try:
            return "\n".join((page.extract_text() or "") for page in PdfReader(str(path)).pages)
        except Exception as err:  # a malformed PDF shouldn't end the run
            print(f"  skip {path.name} — {err}")
            return ""
    if fmt == "docx":
        try:
            from docx import Document  # type: ignore[import-not-found]
        except ImportError:
            print(f"  skip {path.name} — install python-docx to index .docx")
            return ""
        return "\n".join(p.text for p in Document(str(path)).paragraphs)
    if fmt == "pptx":
        try:
            from pptx import Presentation  # type: ignore[import-not-found]
        except ImportError:
            print(f"  skip {path.name} — install python-pptx to index .pptx")
            return ""
        parts: list[str] = []
        for slide in Presentation(str(path)).slides:
            for shape in slide.shapes:
                if shape.has_text_frame:
                    parts.append(shape.text_frame.text)
        return "\n".join(parts)
    print(f"  skip {path.name} — no extractor for '{fmt}'")
    return ""


def chunk(text: str) -> Iterable[str]:
    text = re.sub(r"\n{3,}", "\n\n", text).strip()
    if not text:
        return
    start = 0
    while start < len(text):
        end = min(len(text), start + CHUNK_CHARS)
        # Prefer a paragraph boundary so chunks don't split mid-sentence.
        if end < len(text):
            boundary = text.rfind("\n\n", start + CHUNK_CHARS // 2, end)
            if boundary > start:
                end = boundary
        piece = text[start:end].strip()
        if piece:
            yield piece
        if end >= len(text):
            return
        start = max(end - CHUNK_OVERLAP, start + 1)


def classify(text: str, fallback: str) -> tuple[str, list[str]]:
    lowered = text.lower()
    doc_type = next((label for label, pattern in DOC_TYPE_PATTERNS if re.search(pattern, lowered)), fallback)
    tags = [label for label, pattern in TAG_PATTERNS if re.search(pattern, lowered)]
    return doc_type, tags


def load_manifest() -> list[dict[str, Any]]:
    manifest = CORPUS_DIR / "manifest.json"
    if not manifest.exists():
        fail("corpus/manifest.json not found — run `python scripts/1_scrape.py` first")
    return json.loads(manifest.read_text(encoding="utf-8"))


def create_index(client: SearchClient, settings: Settings, dimensions: int) -> None:
    schema = build_index(
        settings.hr_index,
        aoai_endpoint=settings.aoai_endpoint,
        embedding_deployment=settings.aoai_embedding_deployment,
        embedding_model=settings.aoai_embedding_model,
        dimensions=dimensions,
    )
    path = f"/indexes{odata(settings.hr_index)}"
    dump(f"PUT {path}", schema)
    client.put(path, schema)
    print(f"  index '{settings.hr_index}' created ({len(schema['fields'])} fields, "
          f"{len(FILTERABLE_METADATA)} filterable metadata fields)")


def upload_hr_documents(client: SearchClient, settings: Settings, credential: DefaultAzureCredential) -> int:
    records = [r for r in load_manifest() if r["corpus"] == "hr-templates"]
    if not records:
        fail("no hr-templates documents in the manifest — re-run the scraper")

    documents: list[dict[str, Any]] = []
    for record in records:
        text = extract_text(CORPUS_DIR / record["path"], record["fmt"])
        if not text.strip():
            continue
        doc_type, tags = classify(text, fallback="guidance")
        publisher = re.sub(r"^www\.", "", record["url"].split("/")[2]) if "//" in record["url"] else "unknown"
        parent = slugify(record["path"])
        for position, piece in enumerate(chunk(text)):
            documents.append(
                {
                    "@search.action": "mergeOrUpload",
                    "id": f"{parent}-{position}",
                    "parent_id": parent,
                    "title": record["title"][:300],
                    "content": piece,
                    "source_url": record["url"],
                    "doc_type": doc_type,
                    "language": "en",
                    "jurisdiction": "EU" if "europa.eu" in publisher else "UK",
                    "publisher": publisher,
                    "file_format": record["fmt"],
                    "tags": tags,
                    "published_date": None,
                }
            )

    if not documents:
        fail("every HR document extracted to empty text — install pypdf/python-docx/python-pptx and retry")

    print(f"  embedding {len(documents)} chunks in batches of {EMBED_BATCH}")
    for start in range(0, len(documents), EMBED_BATCH):
        batch = documents[start : start + EMBED_BATCH]
        vectors = embed([d["content"] for d in batch], settings, credential)
        for document, vector in zip(batch, vectors):
            document["content_vector"] = vector

    # The index API caps a batch at 1000 documents; stay well under it.
    for start in range(0, len(documents), 500):
        # The documented action is `docs/search.index`; the OData form is what
        # the spec defines for this api-version.
        client.post(
            f"/indexes{odata(settings.hr_index)}/docs/search.index",
            {"value": documents[start : start + 500]},
        )
    print(f"  uploaded {len(documents)} chunks from {len(records)} source documents")
    return len(documents)


def upload_directives(settings: Settings, credential: DefaultAzureCredential) -> int:
    if not settings.storage_account:
        fail("STORAGE_ACCOUNT is not set — run scripts/provision_infra.sh first")
    from azure.storage.blob import BlobServiceClient  # imported late: only this step needs it

    service = BlobServiceClient(f"https://{settings.storage_account}.blob.core.windows.net", credential=credential)
    container = service.get_container_client(settings.storage_container)
    try:
        container.create_container()
    except Exception:
        pass  # already exists

    uploaded = 0
    for record in load_manifest():
        if record["corpus"] != "eu-directives":
            continue
        path = CORPUS_DIR / record["path"]
        with path.open("rb") as handle:
            container.upload_blob(name=path.name, data=handle, overwrite=True)
        uploaded += 1
    print(f"  uploaded {uploaded} directives to {settings.storage_account}/{settings.storage_container}")
    return uploaded


def create_blob_knowledge_source(client: SearchClient, settings: Settings) -> None:
    """Foundry IQ generates the whole pipeline here: datasource, skillset,
    index and indexer, named `{source}-datasource` .. `{source}-index`.

    Note what this payload does *not* contain: any field, analyzer, vector
    profile or semantic configuration. That is the trade-off of a blob
    knowledge source — see the README flag.
    """
    if not settings.storage_resource_id:
        fail("STORAGE_RESOURCE_ID is not set — run scripts/provision_infra.sh first")
    payload = {
        "name": settings.blob_source,
        "kind": "azureBlob",
        "description": "EU directives bearing on Spanish employment law (PDF, DOC, Markdown).",
        "azureBlobParameters": {
            "connectionString": f"ResourceId={settings.storage_resource_id}",
            "containerName": settings.storage_container,
            "folderPath": None,
            "isADLSGen2": False,
            "ingestionParameters": {
                "networkAccessMode": "public",
                "chatCompletionModel": {
                    "kind": "azureOpenAI",
                    "azureOpenAIParameters": {
                        "resourceUri": settings.aoai_endpoint,
                        "deploymentId": settings.aoai_chat_deployment,
                        "modelName": settings.aoai_chat_model,
                    },
                },
                "embeddingModel": {
                    "kind": "azureOpenAI",
                    "azureOpenAIParameters": {
                        "resourceUri": settings.aoai_endpoint,
                        "deploymentId": settings.aoai_embedding_deployment,
                        "modelName": settings.aoai_embedding_model,
                    },
                },
                "contentExtractionMode": "minimal",
                "ingestionSchedule": None,
            },
        },
    }
    path = f"/knowledgesources{odata(settings.blob_source)}"
    dump(f"PUT {path}", payload)
    client.put(path, payload)
    print(f"  blob knowledge source created — IQ is generating "
          f"{settings.blob_source}-{{datasource,skillset,index,indexer}}")


def create_index_knowledge_source(client: SearchClient, settings: Settings) -> None:
    """Points at the index we designed, so retrieval keeps our hybrid config.

    `queryHints` is how the query planner learns which metadata fields are
    worth filtering on before the vector search runs — it turns "show me
    dismissal templates" into a `$filter` on doc_type/tags plus a vector query,
    rather than a vector query over everything.
    """
    payload = {
        "name": settings.index_source,
        "kind": "searchIndex",
        "description": "HR document samples and templates: contracts, policies, procedures, letters and forms.",
        "searchIndexParameters": {
            "searchIndexName": settings.hr_index,
            "semanticConfigurationName": SEMANTIC_CONFIG,
            "sourceDataFields": [
                {"name": "title"},
                {"name": "content"},
                {"name": "source_url"},
                {"name": "doc_type"},
                {"name": "publisher"},
                {"name": "file_format"},
                {"name": "tags"},
            ],
            # Persistent narrowing applied to every query against this source.
            "baseFilter": "language eq 'en'",
            # SearchIndexKnowledgeSourceQueryHints: `filters` (not filterHints),
            # each a SearchIndexKnowledgeSourceFilterHint of
            # {field, fieldValues, filterInstructions}. `fieldValues` is
            # required — the planner needs the vocabulary, not just the field
            # name, to turn "dismissal letters" into an equality filter.
            "queryHints": {
                "filters": [
                    {
                        "field": "doc_type",
                        "fieldValues": ["contract", "policy", "procedure", "letter", "form", "guidance"],
                        "filterInstructions": "Filter on the kind of document the user is asking for.",
                    },
                    {
                        "field": "tags",
                        "fieldValues": [
                            "working-time", "dismissal", "leave", "pay",
                            "equality", "health-safety", "data-protection",
                        ],
                        "filterInstructions": "Filter on subject matter when the question names one.",
                    },
                    {
                        "field": "jurisdiction",
                        "fieldValues": ["EU", "UK"],
                        "filterInstructions": "Filter when the question is specific to a jurisdiction.",
                    },
                    {
                        "field": "file_format",
                        "fieldValues": ["pdf", "docx", "pptx", "markdown", "txt"],
                        "filterInstructions": "Filter only when the user asks for a particular file format.",
                    },
                ],
            },
        },
    }
    path = f"/knowledgesources{odata(settings.index_source)}"
    dump(f"PUT {path}", payload)
    client.put(path, payload)
    print(f"  search index knowledge source created over '{settings.hr_index}'")


def create_knowledge_base(client: SearchClient, settings: Settings, effort: str) -> None:
    payload = {
        "name": settings.knowledge_base,
        "description": "Spanish employment law (EU directives) plus HR document templates.",
        # This is the routing instruction the planner reads when deciding which
        # source answers a subquery. It is the whole 'route to the best source'
        # behaviour — there is no separate router to configure.
        "retrievalInstructions": (
            "Route questions about legal obligations, directives, transposition deadlines or statutory "
            f"entitlements to '{settings.blob_source}'. Route requests for document samples, templates, "
            f"wording, clauses or HR process paperwork to '{settings.index_source}'. When a question asks "
            "what a document must contain in order to comply, query both and combine them."
        ),
        "answerInstructions": (
            "Answer concisely and cite every claim. Name the directive by CELEX number where relevant. "
            "Do not give legal advice; describe what the sources say."
        ),
        "outputMode": "answerSynthesis",
        "knowledgeSources": [{"name": settings.blob_source}, {"name": settings.index_source}],
        "models": [
            {
                "kind": "azureOpenAI",
                "azureOpenAIParameters": {
                    "resourceUri": settings.aoai_endpoint,
                    "deploymentId": settings.aoai_chat_deployment,
                    "modelName": settings.aoai_chat_model,
                },
            }
        ],
        "retrievalReasoningEffort": {"kind": effort},
        "retrieveDefaults": {"maxRuntimeInSeconds": 45, "maxOutputDocuments": 8, "maxOutputSizeInTokens": 12000},
    }
    dump(f"PUT {settings.kb_path}", payload)
    client.put(settings.kb_path, payload)
    print(f"  knowledge base '{settings.knowledge_base}' created over 2 sources")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--project", required=True, help="Foundry project name (recorded in output; resources are addressed by endpoint)")
    parser.add_argument("--dimensions", type=int, default=3072, help="embedding dimensions (text-embedding-3-large = 3072)")
    parser.add_argument("--reasoning-effort", default="auto", choices=["minimal", "low", "medium", "auto"])
    parser.add_argument("--skip-upload", action="store_true", help="reuse content already in the index and container")
    parser.add_argument("--only", choices=["index", "hr", "blob", "sources", "kb"], help="run a single step")
    args = parser.parse_args()

    try:
        settings = load_settings()
    except ConfigError as err:
        fail(str(err))
        return 1

    credential = DefaultAzureCredential()
    client = SearchClient(settings.search_endpoint, credential)
    print(f"project      {args.project}")
    print(f"search       {settings.search_endpoint}  (api-version {API_VERSION})")
    print(f"knowledge base {settings.knowledge_base}\n")

    steps = args.only
    if steps in (None, "index"):
        print("1/6 HR index")
        create_index(client, settings, args.dimensions)
    if steps in (None, "hr") and not args.skip_upload:
        print("\n2/6 HR documents")
        upload_hr_documents(client, settings, credential)
    if steps in (None, "blob") and not args.skip_upload:
        print("\n3/6 EU directives -> blob")
        upload_directives(settings, credential)
    if steps in (None, "sources"):
        print("\n4/6 blob knowledge source")
        create_blob_knowledge_source(client, settings)
        print("\n5/6 search index knowledge source")
        create_index_knowledge_source(client, settings)
    if steps in (None, "kb"):
        print("\n6/6 knowledge base")
        create_knowledge_base(client, settings, args.reasoning_effort)

    print(f"\nready — try:\n  python scripts/3_search.py \"What must a written statement of employment contain?\"")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
