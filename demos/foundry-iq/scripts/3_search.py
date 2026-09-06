#!/usr/bin/env python3
"""Command 3 — run an agentic retrieval query against the knowledge base.

    python scripts/3_search.py "What must a written statement of employment contain?"

Flags exist for every dial worth demonstrating:

    --effort minimal|low|medium|auto   how hard the planner works
    --source hr-templates-ks           restrict to one knowledge source
    --filter "doc_type eq 'contract'"  an OData filter added to a source
    --output-mode extractiveData       raw grounding documents, no synthesis
    --follow-up "..."                  a second turn, so you can see the
                                       planner reuse conversation context

Every run writes the full response to `.trace/latest.json`, which is what
`4_trace.py` renders. Nothing is summarised on the way out: the trace file is
the API's own response.
"""

from __future__ import annotations

import argparse
import json
import time
from pathlib import Path
from typing import Any

from _common import API_VERSION, ConfigError, SearchClient, Settings, fail, load_settings
from trace_view import render_activity, summarize

TRACE_DIR = Path(__file__).resolve().parents[1] / ".trace"


def build_request(
    turns: list[tuple[str, str]],
    *,
    effort: str,
    sources: list[str],
    source_kinds: dict[str, str],
    source_filter: str | None,
    output_mode: str,
    max_documents: int,
) -> dict[str, Any]:
    request: dict[str, Any] = {
        "messages": [
            {"role": role, "content": [{"type": "text", "text": text}]} for role, text in turns
        ],
        "outputMode": output_mode,
        "retrievalReasoningEffort": {"kind": effort},
        "maxOutputDocuments": max_documents,
        # Without this the response carries no `activity` array and there is
        # nothing for the trace viewer to show.
        "includeActivity": True,
    }
    if sources:
        params: list[dict[str, Any]] = []
        for name in sources:
            # `kind` is the required discriminator on KnowledgeSourceParams;
            # without it the request is rejected. filterAddOn only exists on
            # the searchIndex variant.
            kind = source_kinds.get(name, "searchIndex")
            entry: dict[str, Any] = {"knowledgeSourceName": name, "kind": kind}
            if source_filter and kind == "searchIndex":
                # filterAddOn is ANDed with the source's own baseFilter, so a
                # demo filter narrows rather than replaces the standing one.
                entry["filterAddOn"] = source_filter
            params.append(entry)
        request["knowledgeSourceParams"] = params
    return request


def extract_answer(response: dict[str, Any]) -> str:
    messages = response.get("response") or []
    for message in messages:
        for part in message.get("content", []):
            if part.get("type") == "text":
                return part.get("text", "")
    return ""


def print_references(response: dict[str, Any], limit: int) -> None:
    references = response.get("references") or []
    if not references:
        return
    print(f"\n\033[1mreferences\033[0m ({len(references)})")
    for reference in references[:limit]:
        source = reference.get("type", "?")
        doc_key = reference.get("docKey", "")
        data = reference.get("sourceData") or {}
        title = data.get("title") or data.get("source_url") or doc_key
        print(f"  [{reference.get('id')}] {source:<12} {str(title)[:88]}")
        if data.get("doc_type"):
            print(f"       doc_type={data['doc_type']}  tags={data.get('tags')}  format={data.get('file_format')}")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("query", help="the question to ask")
    parser.add_argument("--follow-up", help="a second turn issued after the first answer")
    parser.add_argument("--effort", default="auto", choices=["minimal", "low", "medium", "auto"])
    parser.add_argument("--source", action="append", default=[], help="restrict to a knowledge source (repeatable)")
    parser.add_argument("--source-kind", action="append", default=[],
                        help="NAME=KIND for a --source whose kind is not searchIndex (e.g. eu-directives-ks=azureBlob)")
    parser.add_argument("--filter", dest="source_filter", help="OData filter added to the selected sources")
    # KnowledgeRetrievalOutputMode: extractiveData | answerSynthesis.
    parser.add_argument("--output-mode", default="answerSynthesis", choices=["answerSynthesis", "extractiveData"])
    parser.add_argument("--max-documents", type=int, default=8)
    parser.add_argument("--max-references", type=int, default=8)
    parser.add_argument("--json", action="store_true", help="print the raw response and nothing else")
    parser.add_argument("--trace-file", type=Path, default=TRACE_DIR / "latest.json")
    args = parser.parse_args()

    try:
        settings: Settings = load_settings()
    except ConfigError as err:
        fail(str(err))
        return 1

    client = SearchClient(settings.search_endpoint)
    # Default the blob source to its real kind so the common case needs no flag.
    source_kinds = {settings.blob_source: "azureBlob", settings.index_source: "searchIndex"}
    for pair in args.source_kind:
        name, _, kind = pair.partition("=")
        source_kinds[name] = kind or "searchIndex"

    turns = [("user", args.query)]
    request = build_request(
        turns,
        effort=args.effort,
        sources=args.source,
        source_kinds=source_kinds,
        source_filter=args.source_filter,
        output_mode=args.output_mode,
        max_documents=args.max_documents,
    )

    if not args.json:
        print(f"\033[1mknowledge base\033[0m {settings.knowledge_base}   effort={args.effort}   api-version={API_VERSION}")
        print(f"\033[1mquery\033[0m {args.query}")
        if args.source:
            print(f"\033[1msources\033[0m {', '.join(args.source)}")
        if args.source_filter:
            print(f"\033[1mfilter\033[0m {args.source_filter}")

    started = time.time()
    response = client.post(f"{settings.kb_path}/retrieve", request, timeout=180)
    wall_ms = int((time.time() - started) * 1000)

    args.trace_file.parent.mkdir(parents=True, exist_ok=True)
    args.trace_file.write_text(
        json.dumps({"request": request, "response": response, "wallMs": wall_ms}, indent=2), encoding="utf-8"
    )

    if args.json:
        print(json.dumps(response, indent=2))
        return 0

    answer = extract_answer(response)
    if answer:
        print(f"\n\033[1manswer\033[0m\n{answer}")
    print_references(response, args.max_references)
    render_activity(response.get("activity") or [], wall_ms=wall_ms)
    print(f"\n{summarize(response.get('activity') or [], wall_ms)}")
    print(f"\ntrace written to {args.trace_file}  —  render it with:")
    print(f"  python scripts/4_trace.py --replay {args.trace_file}")

    if args.follow_up:
        print("\n" + "=" * 72)
        turns.append(("assistant", answer))
        turns.append(("user", args.follow_up))
        request = build_request(
            turns,
            effort=args.effort,
            sources=args.source,
            source_kinds=source_kinds,
            source_filter=args.source_filter,
            output_mode=args.output_mode,
            max_documents=args.max_documents,
        )
        print(f"\033[1mfollow-up\033[0m {args.follow_up}")
        started = time.time()
        response = client.post(f"{settings.kb_path}/retrieve", request, timeout=180)
        wall_ms = int((time.time() - started) * 1000)
        print(f"\n\033[1manswer\033[0m\n{extract_answer(response)}")
        render_activity(response.get("activity") or [], wall_ms=wall_ms)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
