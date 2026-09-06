#!/usr/bin/env python3
"""Create a Foundry toolbox containing one of every tool type we can configure.

    python scripts/create_toolbox.py --toolbox hr-tools --project-endpoint "$PROJECT_ENDPOINT"

Three creation paths, because which one you want depends on where you are:

    --via sdk    azure-ai-projects: project.toolboxes.create_version(name, body)
    --via rest   the same payload, straight to the project REST endpoint
    --via yaml   emit YAML for `azd ai toolbox create <name> --from-file`

They all send the same declaration (scripts/toolbox_spec.py), so you can show
the SDK call and then show the YAML a platform team would check into git.

Tools whose prerequisites aren't configured are skipped with a reason rather
than failing the run — an empty A2A_BASE_URL shouldn't cost you the other
eleven tools. Use --require-all to make missing prerequisites fatal instead.

Two details that are easy to get wrong and are enforced here:

  * every emitted `type` is checked against `ToolboxToolType` from
    azure-ai-projects before anything is sent, so a plausible-but-invented name
    fails locally instead of as a 400 from the service;
  * any `*_preview` tool requires `AIProjectClient(..., allow_preview=True)`,
    which is set automatically when the selection contains one.

`skills` is NOT a tool type — it is a separate create_version parameter — so
pass skills with --skill NAME rather than expecting one in the catalogue.

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

from toolbox_spec import CATALOGUE, PREVIEW_TOOL_TYPES, TOOLBOX_TOOL_TYPES, knowledge_base_tool

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


def prune_empty(payload: Any) -> Any:
    """Drop keys that resolved to an empty string, recursively.

    Optional connection ids left unset would otherwise be sent as `""`, which
    the service rejects less helpfully than simply omitting them.
    """
    if isinstance(payload, dict):
        return {key: prune_empty(value) for key, value in payload.items() if value != ""}
    if isinstance(payload, list):
        return [prune_empty(item) for item in payload]
    return payload


def load_openapi_spec(payload: dict[str, Any]) -> dict[str, Any]:
    """Inline the OpenAPI document. `spec` is the document itself; there is no
    `spec_url` field on OpenApiFunctionDefinition."""
    spec_file = os.environ.get("OPENAPI_SPEC_FILE", "")
    if not spec_file:
        return payload
    text = Path(spec_file).read_text(encoding="utf-8")
    try:
        spec = json.loads(text)
    except json.JSONDecodeError:
        try:
            import yaml  # type: ignore[import-not-found]
        except ImportError as err:
            raise SystemExit(f"{spec_file} is not JSON and PyYAML is not installed to parse it") from err
        spec = yaml.safe_load(text)
    payload["openapi"]["spec"] = spec
    return payload


def select_tools(*, only: list[str], skip: list[str], require_all: bool) -> tuple[list[dict[str, Any]], list[str]]:
    chosen: list[dict[str, Any]] = []
    notes: list[str] = []
    for entry in CATALOGUE:
        name = entry["id"]
        if only and name not in only:
            continue
        if name in skip:
            notes.append(f"  skip {name:<28} — excluded by --skip")
            continue
        missing = [var for var in entry["needs"] if not os.environ.get(var)]
        if missing:
            message = f"  skip {name:<28} — needs {', '.join(missing)}"
            if require_all:
                print(message.replace("skip", "FAIL"), file=sys.stderr)
                raise SystemExit(f"missing prerequisites for {name}: {', '.join(missing)}")
            notes.append(message)
            continue
        payload = prune_empty(resolve(entry["payload"]))
        if payload.get("type") == "openapi":
            payload = load_openapi_spec(payload)
        chosen.append(payload)
        notes.append(f"  add  {name:<28} — {entry['about']}")
    return chosen, notes


def validate(tools: list[dict[str, Any]]) -> None:
    """Fail locally on an invalid discriminator rather than as a service 400."""
    unknown = sorted({str(t.get("type")) for t in tools} - TOOLBOX_TOOL_TYPES)
    if unknown:
        raise SystemExit(
            f"not valid ToolboxToolType values: {', '.join(unknown)}\n"
            f"valid values: {', '.join(sorted(TOOLBOX_TOOL_TYPES))}"
        )


def uses_preview(tools: list[dict[str, Any]]) -> bool:
    return any(str(tool.get("type")) in PREVIEW_TOOL_TYPES for tool in tools)


def maybe_add_knowledge_base(tools: list[dict[str, Any]], notes: list[str]) -> None:
    search_endpoint = os.environ.get("SEARCH_ENDPOINT", "").rstrip("/")
    kb_name = os.environ.get("KNOWLEDGE_BASE", "")
    if not search_endpoint or not kb_name:
        notes.append("  skip knowledge_base              — SEARCH_ENDPOINT/KNOWLEDGE_BASE not set")
        return
    try:
        import requests
        from azure.identity import DefaultAzureCredential

        token = DefaultAzureCredential().get_token("https://search.azure.com/.default").token
        api_version = os.environ.get("SEARCH_API_VERSION", "2026-08-01-preview")
        response = requests.get(
            # Data-plane resources are OData-addressed: /knowledgebases('name').
            f"{search_endpoint}/knowledgebases('{kb_name}')?api-version={api_version}",
            headers={"Authorization": f"Bearer {token}"},
            timeout=30,
        )
    except Exception as err:
        notes.append(f"  skip knowledge_base              — could not check ({err})")
        return
    if response.status_code == 404:
        notes.append(f"  skip knowledge_base              — '{kb_name}' not on {search_endpoint} (run the foundry-iq demo first)")
        return
    if not response.ok:
        notes.append(f"  skip knowledge_base              — HTTP {response.status_code} checking for '{kb_name}'")
        return
    mcp_url = f"{search_endpoint}/knowledgebases('{kb_name}')/mcp?api-version={api_version}"
    tools.append(knowledge_base_tool(kb_name, mcp_url))
    notes.append(f"  add  knowledge_base              — Foundry IQ '{kb_name}' as an MCP tool")


def to_yaml(description: str, tools: list[dict[str, Any]], connections: list[str], skills: list[str]) -> str:
    """Minimal YAML emitter — the payload is only dicts, lists, strings, ints,
    floats and bools, so a dependency isn't worth it here."""

    def scalar(value: Any) -> str:
        if isinstance(value, bool):
            return "true" if value else "false"
        if value is None:
            return "null"
        if isinstance(value, (int, float)):
            return str(value)
        text = str(value)
        return f'"{text}"' if re.search(r"[:#{}\[\],&*?|>%@`\"']|^\s|\s$|^$", text) else text

    def emit(value: Any, indent: int) -> str:
        pad = "  " * indent
        if isinstance(value, dict):
            lines = []
            for key, val in value.items():
                if isinstance(val, (dict, list)) and val:
                    lines.append(f"{pad}{key}:\n" + emit(val, indent + 1))
                elif isinstance(val, (dict, list)):
                    lines.append(f"{pad}{key}: {'{}' if isinstance(val, dict) else '[]'}")
                else:
                    lines.append(f"{pad}{key}: {scalar(val)}")
            return "\n".join(lines)
        if isinstance(value, list):
            lines = []
            for item in value:
                if isinstance(item, dict):
                    lines.append(f"{pad}- " + emit(item, indent + 1).lstrip())
                else:
                    lines.append(f"{pad}- {scalar(item)}")
            return "\n".join(lines)
        return f"{pad}{scalar(value)}"

    parts = [f"description: {scalar(description)}"]
    if connections:
        parts.append("connections:")
        parts.extend(f"  - name: {scalar(name)}" for name in connections)
    if skills:
        parts.append("skills:")
        parts.extend(f"  - {scalar(name)}" for name in skills)
    parts.append("tools:")
    parts.append(emit(tools, 1))
    return "\n".join(parts) + "\n"


def body_for(description: str, tools: list[dict[str, Any]], skills: list[str]) -> dict[str, Any]:
    body: dict[str, Any] = {"description": description, "tools": tools}
    if skills:
        body["skills"] = skills
    return body


def create_via_sdk(endpoint: str, name: str, body: dict[str, Any], allow_preview: bool) -> None:
    from azure.ai.projects import AIProjectClient
    from azure.identity import DefaultAzureCredential

    # allow_preview must be True to use any *_preview tool type.
    project = AIProjectClient(endpoint=endpoint, credential=DefaultAzureCredential(), allow_preview=allow_preview)
    # The JSON-body overload: create_version(name, body). The typed overload
    # wants List[ToolboxTool] model objects, and these are dicts by design so
    # the same declaration can also be emitted as YAML.
    version = project.toolboxes.create_version(name, body)
    print(f"\ncreated toolbox '{name}' version {getattr(version, 'version', '?')}")


def create_via_rest(endpoint: str, name: str, body: dict[str, Any]) -> None:
    import requests
    from azure.identity import DefaultAzureCredential

    token = DefaultAzureCredential().get_token("https://ai.azure.com/.default").token
    url = f"{endpoint.rstrip('/')}/toolboxes/{name}/versions?api-version=v1"
    response = requests.post(
        url,
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
        json=body,
        timeout=120,
    )
    if not response.ok:
        raise SystemExit(f"POST {url} -> HTTP {response.status_code}: {response.text[:1200]}")
    print(f"\ncreated toolbox '{name}': {json.dumps(response.json())[:400]}")


def connection_ids(tools: list[dict[str, Any]]) -> list[str]:
    """project_connection_id appears at the top level on some tools and nested
    inside `azure_ai_search.indexes[]` on others."""
    found: set[str] = set()

    def walk(node: Any) -> None:
        if isinstance(node, dict):
            value = node.get("project_connection_id")
            if isinstance(value, str) and value:
                found.add(value)
            for child in node.values():
                walk(child)
        elif isinstance(node, list):
            for child in node:
                walk(child)

    walk(tools)
    return sorted(found)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--toolbox", required=True, help="toolbox name (parameterised, as requested)")
    parser.add_argument("--project-endpoint", default=os.environ.get("PROJECT_ENDPOINT", ""),
                        help="https://<account>.services.ai.azure.com/api/projects/<project>")
    parser.add_argument("--description", default="Demo toolbox: one of every configurable tool type.")
    parser.add_argument("--via", choices=["sdk", "rest", "yaml"], default="yaml")
    parser.add_argument("--only", action="append", default=[], help="only these tool ids (repeatable)")
    parser.add_argument("--skip", action="append", default=[], help="exclude these tool ids (repeatable)")
    parser.add_argument("--skill", action="append", default=[], help="attach a Foundry skill by name (repeatable)")
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

    validate(tools)
    preview = uses_preview(tools)
    if preview:
        print("\nnote: selection includes preview tool types — allow_preview=True is required and is set for --via sdk.")
    if not any(tool.get("type") == "toolbox_search" for tool in tools):
        print("\nnote: no toolbox_search tool — the model will receive every tool definition on every turn.")

    body = body_for(args.description, tools, args.skill)
    connections = connection_ids(tools)

    if args.via == "yaml":
        args.out.write_text(to_yaml(args.description, tools, connections, args.skill), encoding="utf-8")
        print(f"\nwrote {args.out} ({len(tools)} tools)\n\ncreate it with:")
        for connection in connections:
            print(f"  azd ai connection create {connection} --kind remote-tool --target <url> --auth-type <type>")
        print(f"  azd ai toolbox create {args.toolbox} --from-file {args.out}")
        print(f"  azd ai toolbox version list {args.toolbox}")
        return 0

    if not args.project_endpoint:
        raise SystemExit("--project-endpoint (or PROJECT_ENDPOINT) is required for --via sdk|rest")
    if args.via == "sdk":
        create_via_sdk(args.project_endpoint, args.toolbox, body, preview)
    else:
        create_via_rest(args.project_endpoint, args.toolbox, body)

    print("\nconsumer endpoint (always the default version):")
    print(f"  {args.project_endpoint.rstrip('/')}/toolboxes/{args.toolbox}/mcp?api-version=v1")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
