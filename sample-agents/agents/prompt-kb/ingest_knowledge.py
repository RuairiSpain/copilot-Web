"""Load the knowledge PDFs into a Foundry vector store.

The kb-prompt-agent searches its PDF knowledge with the ``file_search`` tool, and that
tool reads from a vector store. ``azd`` can declare the agent but cannot upload files,
so this script does that one step.

It is idempotent. Run it as often as you like:

* the vector store is found by name, and created only when missing,
* a PDF that is already in the store with the same size is skipped,
* a PDF whose size changed replaces the old copy.

Usage::

    FOUNDRY_PROJECT_ENDPOINT=https://<account>.services.ai.azure.com/api/projects/<p> \\
        uv run python ingest_knowledge.py

The vector store id is the only thing printed to stdout, so ``make ingest`` can store it
in the azd environment as ``KB_VECTOR_STORE_ID``. Progress goes to stderr.
"""

from __future__ import annotations

import argparse
import os
import sys
from collections.abc import Sequence
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

DEFAULT_STORE_NAME = "field-handbook"
DEFAULT_FOLDER = Path(__file__).parent / "knowledge"


class IngestError(RuntimeError):
    """Raised for problems the operator can fix, such as a missing folder."""


@dataclass(frozen=True)
class IngestResult:
    """What an ingestion run did."""

    vector_store_id: str
    uploaded: list[str] = field(default_factory=list)
    skipped: list[str] = field(default_factory=list)
    replaced: list[str] = field(default_factory=list)


def find_pdfs(folder: Path) -> list[Path]:
    """Return the PDFs in ``folder`` in a stable order."""
    if not folder.is_dir():
        raise IngestError(f"Knowledge folder not found: {folder}")
    pdfs = sorted(p for p in folder.iterdir() if p.suffix.lower() == ".pdf" and p.is_file())
    if not pdfs:
        raise IngestError(f"No PDF files found in {folder}")
    return pdfs


def get_or_create_vector_store(client: Any, name: str) -> str:
    """Return the id of the vector store called ``name``, creating it if needed."""
    for store in client.vector_stores.list():
        if store.name == name:
            return str(store.id)
    created = client.vector_stores.create(name=name)
    return str(created.id)


def _existing_files(client: Any, vector_store_id: str) -> dict[str, Any]:
    """Map filename to file object for files already in the vector store."""
    existing: dict[str, Any] = {}
    for item in client.vector_stores.files.list(vector_store_id=vector_store_id):
        file_obj = client.files.retrieve(item.id)
        existing[file_obj.filename] = file_obj
    return existing


def ingest(
    client: Any,
    folder: Path = DEFAULT_FOLDER,
    store_name: str = DEFAULT_STORE_NAME,
) -> IngestResult:
    """Synchronise the PDFs in ``folder`` into the named vector store."""
    pdfs = find_pdfs(folder)
    vector_store_id = get_or_create_vector_store(client, store_name)
    existing = _existing_files(client, vector_store_id)

    uploaded: list[str] = []
    skipped: list[str] = []
    replaced: list[str] = []

    for pdf in pdfs:
        current = existing.get(pdf.name)
        if current is not None and current.bytes == pdf.stat().st_size:
            skipped.append(pdf.name)
            continue
        if current is not None:
            client.vector_stores.files.delete(vector_store_id=vector_store_id, file_id=current.id)
            replaced.append(pdf.name)
        with pdf.open("rb") as handle:
            client.vector_stores.files.upload_and_poll(vector_store_id=vector_store_id, file=handle)
        uploaded.append(pdf.name)

    return IngestResult(vector_store_id, uploaded, skipped, replaced)


def _parse_args(argv: Sequence[str] | None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("--folder", type=Path, default=DEFAULT_FOLDER)
    parser.add_argument("--store-name", default=DEFAULT_STORE_NAME)
    parser.add_argument(
        "--project-endpoint",
        default=os.environ.get("FOUNDRY_PROJECT_ENDPOINT"),
        help="Foundry project endpoint. Defaults to $FOUNDRY_PROJECT_ENDPOINT.",
    )
    return parser.parse_args(argv)


def main(argv: Sequence[str] | None = None) -> int:
    """Command-line entry point. Prints the vector store id to stdout."""
    args = _parse_args(argv)
    if not args.project_endpoint:
        print(
            "error: set FOUNDRY_PROJECT_ENDPOINT or pass --project-endpoint "
            "(find it with `azd env get-values`).",
            file=sys.stderr,
        )
        return 2

    # Imported here so unit tests and --help never need Azure credentials.
    from azure.ai.projects import AIProjectClient
    from azure.identity import DefaultAzureCredential

    project = AIProjectClient(endpoint=args.project_endpoint, credential=DefaultAzureCredential())
    try:
        result = ingest(project.get_openai_client(), args.folder, args.store_name)
    except IngestError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    print(
        f"uploaded={result.uploaded} replaced={result.replaced} skipped={result.skipped}",
        file=sys.stderr,
    )
    print(result.vector_store_id)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
