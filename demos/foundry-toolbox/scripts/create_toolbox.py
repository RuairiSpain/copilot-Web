#!/usr/bin/env python3
"""Create a Foundry toolbox containing one of every tool type we can configure.

    python scripts/create_toolbox.py --toolbox hr-tools --project-endpoint "$PROJECT_ENDPOINT"

Three creation paths, because which one you want depends on where you are:

    --via sdk    azure-ai-projects: project.toolboxes.create_version(...)
    --via rest   the same payload, straight to the project REST endpoint
    --via yaml   emit YAML for `azd ai toolbox create <name> --from-file`

They all send the same declaration (scripts/toolbox_spec.py), so you can show
the SDK call and then show the YAML a platform team would check into git.

Tools whose prerequisites aren't configured are skipped with a reason rather
than failing the run — an empty A2A_AGENT_ID shouldn't cost you the other ten
tools. Use --require-all to make missing prerequisites fatal instead.

If the Foundry IQ knowledge base from ../foundry-iq exists on the search
service, it is added as an MCP tool. If it doesn't, it's skipped silently —
the two demos are independent.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
from pathlib import Path
from typing import Any

from toolbox_spec import CATALOGUE, knowledge_base_tool

PLACEHOLDER = re.compile(r"\$\{([A-Z0-9_]+)\}")


def resolve(payload: Any) -> Any:
    """Substitute ${ENV_VAR} placeholders from the environment."""
    if isinstance(payload, dict):
        return {key: resolve(value) for key, value in payload.items()}
    if isinstance(payload, list):
        return [resolve(item) for item in payload]
    if isinstance(payload, str):
        return PLACEHOLDER.sub(lambda m: os.environ.get(m.group(1), ""), payload)
    return payload


def prune_empty(payload: dict[str, Any]) -> dict[str, Any]:
    """Drop keys that resolved to an empty string, so optional connection ids
    don't get sent as `""` and rejected."""
    return {key: value for key, value in payload.items() if value != ""}


def select_tools(*, only: list[str], skip: list[str], require_all: bool) -> tuple[list[dict[str, Any]], list[str]]:
    chosen: list[dict[str, Any]] = []
    notes: list[str] = []
    for entry in CATALOGUE:
        name = entry["id"]
        if only and name not in only:
            continue
        if name in skip:
            notes.append(f"  skip {name:<20} — excluded by --skip")
            continue
        missing = [var for var in entry["needs"] if not os.environ.get(var)]
        if missing:
            message = f"  skip {name:<20} — needs {', '.join(missing)}"
            if require_all:
                print(message.replace("skip", "FAIL"), file=sys.stderr)
                raise SystemExit(f"missing prerequisites for {name}: {', '.join(missing)}")
            notes.append(message)
            continue
        chosen.append(prune_empty(resolve(entry["payload"])))
        notes.append(f"  add  {name:<20} — {entry['about']}")
    return chosen, notes


def maybe_add_knowledge_base(tools: list[dict[str, Any]], notes: list[str]) -> None:
    search_endpoint = os.environ.get("SEARCH_ENDPOINT", "").rstrip("/")
    kb_name = os.environ.get("KNOWLEDGE_BASE", "")
    if not search_endpoint or not kb_name:
        notes.append("  skip knowledge_base      — SEARCH_ENDPOINT/KNOWLEDGE_BASE not set")
        return
    try:
        import requests
        from azure.identity import DefaultAzureCredential

        token = DefaultAzureCredential().get_token("https://search.azure.com/.default").token
        api_version = os.environ.get("SEARCH_API_VERSION", "2026-08-01-preview")
        response = requests.get(
            f"{search_endpoint}/knowledgebases/{kb_name}?api-version={api_version}",
            headers={"Authorization": f"Bearer {token}"},
            timeout=30,
        )
    except Exception as err:
        notes.append(f"  skip knowledge_base      — could not check ({err})")
        return
    if response.status_code == 404:
        notes.append(f"  skip knowledge_base      — '{kb_name}' not on {search_endpoint} (run the foundry-iq demo first)")
        return
    if not response.ok:
        notes.append(f"  skip knowledge_base      — HTTP {response.status_code} checking for '{kb_name}'")
        return
    mcp_url = f"{search_endpoint}/knowledgebases/{kb_name}/mcp?api-version={api_version}"
    tools.append(knowledge_base_tool(kb_name, mcp_url))
    notes.append(f"  add  knowledge_base      — Foundry IQ '{kb_name}' as an MCP tool")


def to_yaml(description: str, tools: list[dict[str, Any]], connections: list[str]) -> str:
    """Minimal YAML emitter — the payload is only dicts, lists, strings, ints
    and bools, so a dependency isn't worth it here."""

    def emit(value: Any, indent: int) -> str:
        pad = "  " * indent
        if isinstance(value, dict):
            return "\n".join(
                f"{pad}{key}:" + (f" {scalar(val)}" if not isinstance(val, (dict, list)) else "\n" + emit(val, indent + 1))
                for key, val in value.items()
            )
        if isinstance(value, list):
            lines = []
            for item in value:
                if isinstance(item, dict):
                    body = emit(item, indent + 1).lstrip()
                    lines.append(f"{pad}- {body}")
                else:
                    lines.append(f"{pad}- {scalar(item)}")
            return "\n".join(lines)
        return f"{pad}{scalar(value)}"

    def scalar(value: Any) -> str:
        if isinstance(value, bool):
            return "true" if value else "false"
        if value is None:
            return "null"
        if isinstance(value, (int, float)):
            return str(value)
        text = str(value)
        return f'"{text}"' if re.search(r"[:#{}\[\],&*?|>%@`\"']|^\s|\s$", text) else text

    parts = [f"description: {scalar(description)}"]
    if connections:
        parts.append("connections:")
        parts.extend(f"  - name: {scalar(name)}" for name in connections)
    parts.append("tools:")
    parts.append(emit(tools, 1))
    return "\n".join(parts) + "\n"


def create_via_sdk(endpoint: str, name: str, description: str, tools: list[dict[str, Any]]) -> None:
    from azure.ai.projects import AIProjectClient
    from azure.identity import DefaultAzureCredential

    project = AIProjectClient(endpoint=endpoint, credential=DefaultAzureCredential())
    version = project.toolboxes.create_version(name=name, description=description, tools=tools)
    print(f"\ncreated toolbox '{name}' version {getattr(version, 'version', '?')}")


def create_via_rest(endpoint: str, name: str, description: str, tools: list[dict[str, Any]]) -> None:
    import requests
    from azure.identity import DefaultAzureCredential

    token = DefaultAzureCredential().get_token("https://ai.azure.com/.default").token
    url = f"{endpoint.rstrip('/')}/toolboxes/{name}/versions?api-version=v1"
    response = requests.post(
        url,
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
        json={"description": description, "tools": tools},
        timeout=120,
    )
    if not response.ok:
        raise SystemExit(f"POST {url} -> HTTP {response.status_code}: {response.text[:1200]}")
    print(f"\ncreated toolbox '{name}': {json.dumps(response.json())[:400]}")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--toolbox", required=True, help="toolbox name (parameterised, as requested)")
    parser.add_argument("--project-endpoint", default=os.environ.get("PROJECT_ENDPOINT", ""),
                        help="https://<account>.services.ai.azure.com/api/projects/<project>")
    parser.add_argument("--description", default="Demo toolbox: one of every configurable tool type.")
    parser.add_argument("--via", choices=["sdk", "rest", "yaml"], default="yaml")
    parser.add_argument("--only", action="append", default=[], help="only these tool ids (repeatable)")
    parser.add_argument("--skip", action="append", default=[], help="exclude these tool ids (repeatable)")
    parser.add_argument("--require-all", action="store_true", help="fail if any tool's prerequisites are missing")
    parser.add_argument("--no-knowledge-base", action="store_true")
    parser.add_argument("--out", type=Path, default=Path("toolbox.yaml"), help="where --via yaml writes")
    args = parser.parse_args()

    print(f"toolbox  {args.toolbox}")
    print("tools")
    tools, notes = select_tools(only=args.only, skip=args.skip, require_all=args.require_all)
    if not args.no_knowledge_base:
        maybe_add_knowledge_base(tools, notes)
    for note in notes:
        print(note)

    if not any(tool.get("type") == "toolbox_search" for tool in tools):
        print("\nnote: no toolbox_search tool — the model will receive every tool definition on every turn.")

    connections = sorted({tool["project_connection_id"] for tool in tools if tool.get("project_connection_id")})

    if args.via == "yaml":
        args.out.write_text(to_yaml(args.description, tools, connections), encoding="utf-8")
        print(f"\nwrote {args.out} ({len(tools)} tools)\n\ncreate it with:")
        for connection in connections:
            print(f"  azd ai connection create {connection} --kind remote-tool --target <url> --auth-type <type>")
        print(f"  azd ai toolbox create {args.toolbox} --from-file {args.out}")
        print(f"  azd ai toolbox version list {args.toolbox}")
        return 0

    if not args.project_endpoint:
        raise SystemExit("--project-endpoint (or PROJECT_ENDPOINT) is required for --via sdk|rest")
    if args.via == "sdk":
        create_via_sdk(args.project_endpoint, args.toolbox, args.description, tools)
    else:
        create_via_rest(args.project_endpoint, args.toolbox, args.description, tools)

    print("\nconsumer endpoint (always the default version):")
    print(f"  {args.project_endpoint.rstrip('/')}/toolboxes/{args.toolbox}/mcp?api-version=v1")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
