#!/usr/bin/env python3
"""Inventory everything inside a Foundry project and write it to Markdown.

    python scripts/inventory.py --all --out inventory.md
    python scripts/inventory.py --models --agents --toolboxes
    python scripts/inventory.py --iq --format json

Two planes are queried, because a "project" spans both:

  * the project data plane  (agents, toolboxes, skills, connections, routines)
  * the search service      (knowledge bases, knowledge sources, indexes)
  * ARM, via `az`           (model deployments, resource-level facts)

**Every collector degrades instead of failing.** If a surface isn't present in
your api-version, isn't enabled on the account, or you lack the role for it,
that row is recorded as unavailable *with the reason* and the run continues.
The report therefore doubles as a capability probe: what answered is what your
project actually exposes. Some of these paths are preview surfaces that move
between api-versions — a 404 here means "not at this path today", not "you
don't have it".
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable

import requests
from azure.identity import DefaultAzureCredential

PROJECT_SCOPE = "https://ai.azure.com/.default"
SEARCH_SCOPE = "https://search.azure.com/.default"


@dataclass
class Section:
    key: str
    title: str
    items: list[dict[str, Any]] = field(default_factory=list)
    columns: list[str] = field(default_factory=list)
    source: str = ""
    error: str | None = None


class Collector:
    def __init__(self, project_endpoint: str, search_endpoint: str, api_version: str, search_api_version: str) -> None:
        self.project_endpoint = project_endpoint.rstrip("/")
        self.search_endpoint = search_endpoint.rstrip("/")
        self.api_version = api_version
        self.search_api_version = search_api_version
        self._credential = DefaultAzureCredential()
        self._tokens: dict[str, str] = {}

    def _token(self, scope: str) -> str:
        if scope not in self._tokens:
            self._tokens[scope] = self._credential.get_token(scope).token
        return self._tokens[scope]

    def _get(self, base: str, path: str, scope: str, api_version: str) -> Any:
        url = f"{base}/{path.lstrip('/')}"
        response = requests.get(
            f"{url}{'&' if '?' in url else '?'}api-version={api_version}",
            headers={"Authorization": f"Bearer {self._token(scope)}"},
            timeout=60,
        )
        if response.status_code == 404:
            raise LookupError(f"not available at {path} (HTTP 404)")
        if response.status_code in (401, 403):
            raise PermissionError(f"HTTP {response.status_code} — check your role assignments")
        if not response.ok:
            raise RuntimeError(f"HTTP {response.status_code}: {response.text[:300]}")
        return response.json()

    def project(self, path: str) -> Any:
        if not self.project_endpoint:
            raise LookupError("PROJECT_ENDPOINT not set")
        return self._get(self.project_endpoint, path, PROJECT_SCOPE, self.api_version)

    def search(self, path: str) -> Any:
        if not self.search_endpoint:
            raise LookupError("SEARCH_ENDPOINT not set")
        return self._get(self.search_endpoint, path, SEARCH_SCOPE, self.search_api_version)


def values(payload: Any) -> list[dict[str, Any]]:
    """Azure list responses use `value`; some preview surfaces use `data`."""
    if isinstance(payload, dict):
        for key in ("value", "data", "items"):
            if isinstance(payload.get(key), list):
                return payload[key]
        return [payload]
    return payload if isinstance(payload, list) else []


def az_json(args: list[str]) -> Any:
    if not shutil.which("az"):
        raise LookupError("az CLI not on PATH")
    completed = subprocess.run([*args, "-o", "json"], capture_output=True, text=True, check=False)
    if completed.returncode != 0:
        raise RuntimeError(completed.stderr.strip().splitlines()[-1][:300] if completed.stderr else "az failed")
    return json.loads(completed.stdout or "[]")


def pick(item: dict[str, Any], *names: str, default: Any = "") -> Any:
    for name in names:
        if item.get(name) not in (None, ""):
            return item[name]
    return default


# --- collectors -------------------------------------------------------------
# Each returns (columns, rows, source-description).

def collect_models(c: Collector, args: argparse.Namespace) -> tuple[list[str], list[dict[str, Any]], str]:
    if not args.account or not args.resource_group:
        raise LookupError("pass --account and --resource-group to list model deployments")
    rows = az_json(
        ["az", "cognitiveservices", "account", "deployment", "list", "-n", args.account, "-g", args.resource_group]
    )
    return (
        ["name", "model", "version", "sku", "capacity"],
        [
            {
                "name": r.get("name", ""),
                "model": (r.get("properties", {}).get("model") or {}).get("name", ""),
                "version": (r.get("properties", {}).get("model") or {}).get("version", ""),
                "sku": (r.get("sku") or {}).get("name", ""),
                "capacity": (r.get("sku") or {}).get("capacity", ""),
            }
            for r in rows
        ],
        f"az cognitiveservices account deployment list -n {args.account}",
    )


def collect_agents(c: Collector, args: argparse.Namespace) -> tuple[list[str], list[dict[str, Any]], str]:
    rows = values(c.project("/agents"))
    return (
        ["name", "id", "model", "tools"],
        [
            {
                "name": pick(r, "name", "displayName"),
                "id": pick(r, "id", "agentId"),
                "model": pick(r, "model", "modelDeploymentName"),
                "tools": len(r.get("tools") or []),
            }
            for r in rows
        ],
        "GET {project}/agents",
    )


def collect_tools(c: Collector, args: argparse.Namespace) -> tuple[list[str], list[dict[str, Any]], str]:
    """There is no project-wide tool list — tools live on agents and in
    toolboxes — so this flattens both into one view."""
    rows: list[dict[str, Any]] = []
    for agent in values(c.project("/agents")):
        for tool in agent.get("tools") or []:
            rows.append(
                {
                    "owner": f"agent:{pick(agent, 'name', 'id')}",
                    "type": pick(tool, "type", default="?"),
                    "name": pick(tool, "name", "server_label", "serverLabel"),
                }
            )
    return ["owner", "type", "name"], rows, "GET {project}/agents -> .tools[]"


def collect_toolboxes(c: Collector, args: argparse.Namespace) -> tuple[list[str], list[dict[str, Any]], str]:
    rows: list[dict[str, Any]] = []
    for toolbox in values(c.project("/toolboxes")):
        name = pick(toolbox, "name", "toolboxName")
        entry = {
            "name": name,
            "defaultVersion": pick(toolbox, "defaultVersion", "default_version"),
            "description": str(pick(toolbox, "description"))[:70],
            "tools": "",
        }
        try:
            version = c.project(f"/toolboxes/{name}/versions/{entry['defaultVersion']}")
            entry["tools"] = ", ".join(sorted({str(t.get("type")) for t in (version.get("tools") or [])}))
        except Exception as err:
            entry["tools"] = f"(unavailable: {err})"
        rows.append(entry)
    return ["name", "defaultVersion", "tools", "description"], rows, "GET {project}/toolboxes"


def collect_skills(c: Collector, args: argparse.Namespace) -> tuple[list[str], list[dict[str, Any]], str]:
    rows = values(c.project("/skills"))
    return (
        ["name", "version", "description"],
        [
            {
                "name": pick(r, "name"),
                "version": pick(r, "version"),
                "description": str(pick(r, "description"))[:80],
            }
            for r in rows
        ],
        "GET {project}/skills",
    )


def collect_connections(c: Collector, args: argparse.Namespace) -> tuple[list[str], list[dict[str, Any]], str]:
    rows = values(c.project("/connections"))
    return (
        ["name", "kind", "target", "authType"],
        [
            {
                "name": pick(r, "name"),
                "kind": pick(r, "kind", "type", "category"),
                "target": str(pick(r, "target", "endpoint"))[:70],
                "authType": pick(r, "authType", "auth_type"),
            }
            for r in rows
        ],
        "GET {project}/connections",
    )


def collect_routines(c: Collector, args: argparse.Namespace) -> tuple[list[str], list[dict[str, Any]], str]:
    rows = values(c.project("/routines"))
    return (
        ["name", "schedule", "enabled", "target"],
        [
            {
                "name": pick(r, "name"),
                "schedule": pick(r, "schedule", "cron", "trigger"),
                "enabled": pick(r, "enabled", default=""),
                "target": pick(r, "agentId", "target"),
            }
            for r in rows
        ],
        "GET {project}/routines",
    )


def collect_gateways(c: Collector, args: argparse.Namespace) -> tuple[list[str], list[dict[str, Any]], str]:
    rows = values(c.project("/aigateways"))
    return (
        ["name", "endpoint", "state"],
        [{"name": pick(r, "name"), "endpoint": pick(r, "endpoint", "target"), "state": pick(r, "state", "status")} for r in rows],
        "GET {project}/aigateways",
    )


def collect_knowledge_bases(c: Collector, args: argparse.Namespace) -> tuple[list[str], list[dict[str, Any]], str]:
    rows = values(c.search("/knowledgebases"))
    return (
        ["name", "sources", "outputMode", "reasoningEffort", "description"],
        [
            {
                "name": pick(r, "name"),
                "sources": ", ".join(s.get("name", "?") for s in (r.get("knowledgeSources") or [])),
                "outputMode": pick(r, "outputMode"),
                "reasoningEffort": (r.get("retrievalReasoningEffort") or {}).get("kind", ""),
                "description": str(pick(r, "description"))[:60],
            }
            for r in rows
        ],
        "GET {search}/knowledgebases",
    )


def collect_knowledge_sources(c: Collector, args: argparse.Namespace) -> tuple[list[str], list[dict[str, Any]], str]:
    rows = values(c.search("/knowledgesources"))
    out = []
    for r in rows:
        kind = pick(r, "kind")
        backing = ""
        if kind == "searchIndex":
            backing = (r.get("searchIndexParameters") or {}).get("searchIndexName", "")
        elif kind == "azureBlob":
            params = r.get("azureBlobParameters") or {}
            backing = f"{params.get('containerName', '')} -> {pick(r, 'name')}-index (generated)"
        out.append({"name": pick(r, "name"), "kind": kind, "backing": backing, "description": str(pick(r, "description"))[:60]})
    return ["name", "kind", "backing", "description"], out, "GET {search}/knowledgesources"


def collect_indexes(c: Collector, args: argparse.Namespace) -> tuple[list[str], list[dict[str, Any]], str]:
    rows = values(c.search("/indexes"))
    out = []
    for r in rows:
        fields = r.get("fields") or []
        out.append(
            {
                "name": pick(r, "name"),
                "fields": len(fields),
                "vector": "yes" if any(f.get("dimensions") for f in fields) else "no",
                "filterable": ", ".join(f["name"] for f in fields if f.get("filterable"))[:70],
                "semantic": ", ".join(cfg.get("name", "") for cfg in ((r.get("semantic") or {}).get("configurations") or [])),
            }
        )
    return ["name", "fields", "vector", "semantic", "filterable"], out, "GET {search}/indexes"


COLLECTORS: dict[str, tuple[str, Callable[[Collector, argparse.Namespace], tuple[list[str], list[dict[str, Any]], str]]]] = {
    "models": ("Model deployments", collect_models),
    "agents": ("Agents", collect_agents),
    "tools": ("Tools (attached to agents)", collect_tools),
    "toolboxes": ("Toolboxes", collect_toolboxes),
    "skills": ("Skills", collect_skills),
    "connections": ("Connections", collect_connections),
    "routines": ("Routines", collect_routines),
    "gateways": ("AI gateways", collect_gateways),
    "iq": ("Foundry IQ knowledge bases", collect_knowledge_bases),
    "sources": ("Foundry IQ knowledge sources", collect_knowledge_sources),
    "indexes": ("Search indexes", collect_indexes),
}


def render_markdown(sections: list[Section], header: dict[str, str]) -> str:
    lines = ["# Foundry project inventory", ""]
    for key, value in header.items():
        lines.append(f"- **{key}**: {value or '_(not set)_'}")
    lines.append("")

    available = [s for s in sections if s.error is None]
    lines.append("| Section | Items | Status |")
    lines.append("| --- | --- | --- |")
    for section in sections:
        status = "ok" if section.error is None else f"unavailable — {section.error}"
        lines.append(f"| {section.title} | {len(section.items) if section.error is None else '—'} | {status} |")
    lines.append("")

    for section in sections:
        lines.append(f"## {section.title}")
        lines.append("")
        if section.source:
            lines.append(f"`{section.source}`")
            lines.append("")
        if section.error is not None:
            lines.append(f"> Not available: {section.error}")
            lines.append("")
            continue
        if not section.items:
            lines.append("_none_")
            lines.append("")
            continue
        lines.append("| " + " | ".join(section.columns) + " |")
        lines.append("| " + " | ".join("---" for _ in section.columns) + " |")
        for item in section.items:
            cells = [str(item.get(column, "")).replace("|", "\\|") for column in section.columns]
            lines.append("| " + " | ".join(cells) + " |")
        lines.append("")

    if len(available) < len(sections):
        lines.append("---")
        lines.append("")
        lines.append(
            "Sections marked unavailable were probed and did not answer. That can mean the surface is not in this "
            "api-version, is not enabled on the account, or your identity lacks the role — the reason column says "
            "which. It does not mean the capability doesn't exist."
        )
    return "\n".join(lines) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--all", action="store_true", help="every section")
    for key, (title, _) in COLLECTORS.items():
        parser.add_argument(f"--{key}", action="store_true", help=title.lower())
    parser.add_argument("--project-endpoint", default=os.environ.get("PROJECT_ENDPOINT", ""))
    parser.add_argument("--search-endpoint", default=os.environ.get("SEARCH_ENDPOINT", ""))
    parser.add_argument("--account", default=os.environ.get("AOAI_ACCOUNT", ""), help="Cognitive Services account, for --models")
    parser.add_argument("--resource-group", default=os.environ.get("RESOURCE_GROUP", ""))
    parser.add_argument("--api-version", default=os.environ.get("PROJECT_API_VERSION", "v1"))
    parser.add_argument("--search-api-version", default=os.environ.get("SEARCH_API_VERSION", "2026-08-01-preview"))
    parser.add_argument("--format", choices=["md", "json"], default="md")
    parser.add_argument("--out", type=Path, help="write here instead of stdout")
    args = parser.parse_args()

    selected = [key for key in COLLECTORS if getattr(args, key)] or (list(COLLECTORS) if args.all else [])
    if not selected:
        parser.error("choose at least one section, or --all")

    collector = Collector(args.project_endpoint, args.search_endpoint, args.api_version, args.search_api_version)
    sections: list[Section] = []
    for key in selected:
        title, fn = COLLECTORS[key]
        section = Section(key=key, title=title)
        try:
            section.columns, section.items, section.source = fn(collector, args)
        except Exception as err:
            section.error = f"{type(err).__name__}: {err}"
        sections.append(section)
        state = f"{len(section.items)} items" if section.error is None else "unavailable"
        print(f"  {key:<12} {state}")

    header = {
        "generated": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "project endpoint": args.project_endpoint,
        "search endpoint": args.search_endpoint,
        "project api-version": args.api_version,
        "search api-version": args.search_api_version,
    }
    if args.format == "json":
        output = json.dumps(
            {"header": header, "sections": [{"key": s.key, "title": s.title, "source": s.source,
                                             "error": s.error, "items": s.items} for s in sections]},
            indent=2,
        )
    else:
        output = render_markdown(sections, header)

    if args.out:
        args.out.write_text(output, encoding="utf-8")
        print(f"\nwrote {args.out}")
    else:
        print("\n" + output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
