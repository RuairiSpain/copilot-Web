#!/usr/bin/env python3
"""Call a toolbox over MCP and show how it routes to the right tool.

    python scripts/call_toolbox.py --toolbox hr-tools --need "search our HR templates for a dismissal letter"

A toolbox is a single MCP endpoint, so this is a plain MCP client — no Foundry
SDK involved, which is the point: any MCP-capable runtime can consume it.

The demo runs three JSON-RPC calls and prints each one:

  1. tools/list      what the endpoint advertises. With `toolbox_search` in the
                     version you see TWO tools (`tool_search`, `call_tool`)
                     instead of the full catalogue — that substitution IS the
                     context saving.
  2. tools/call tool_search {"query": "<your need>"}
                     BM25 over tool metadata returns ranked candidates. This is
                     the routing decision, made explicit.
  3. tools/call call_tool {"name": "<top hit>", ...}
                     invoke the winner and show the result came from it.

Without `toolbox_search`, step 1 lists everything and the script says so rather
than pretending a routing step happened.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import uuid
from typing import Any

import requests
from azure.identity import DefaultAzureCredential

DIM, BOLD, RESET = "\033[2m", "\033[1m", "\033[0m"


class MCPClient:
    """Minimal MCP client over streamable HTTP.

    Handles both a plain JSON response and an SSE stream, because the transport
    is allowed to answer either way and Foundry endpoints do use SSE.
    """

    def __init__(self, url: str, token: str) -> None:
        self.url = url
        self.session = requests.Session()
        self.session.headers.update(
            {
                "Authorization": f"Bearer {token}",
                "Content-Type": "application/json",
                "Accept": "application/json, text/event-stream",
            }
        )
        self.session_id: str | None = None

    def call(self, method: str, params: dict[str, Any] | None = None, *, notify: bool = False) -> Any:
        payload: dict[str, Any] = {"jsonrpc": "2.0", "method": method}
        if params is not None:
            payload["params"] = params
        if not notify:
            payload["id"] = str(uuid.uuid4())
        headers = {"Mcp-Session-Id": self.session_id} if self.session_id else {}

        response = self.session.post(self.url, json=payload, headers=headers, timeout=120, stream=True)
        if not response.ok:
            raise RuntimeError(f"{method} -> HTTP {response.status_code}: {response.text[:800]}")
        if "Mcp-Session-Id" in response.headers:
            self.session_id = response.headers["Mcp-Session-Id"]
        if notify:
            return None

        content_type = response.headers.get("Content-Type", "")
        if "text/event-stream" in content_type:
            for line in response.iter_lines(decode_unicode=True):
                if line and line.startswith("data:"):
                    message = json.loads(line[5:].strip())
                    if "result" in message or "error" in message:
                        return self._unwrap(message)
            raise RuntimeError(f"{method}: SSE stream ended without a result")
        return self._unwrap(response.json())

    @staticmethod
    def _unwrap(message: dict[str, Any]) -> Any:
        if "error" in message:
            raise RuntimeError(f"MCP error {message['error'].get('code')}: {message['error'].get('message')}")
        return message.get("result")

    def initialize(self) -> Any:
        result = self.call(
            "initialize",
            {
                "protocolVersion": "2025-06-18",
                "capabilities": {},
                "clientInfo": {"name": "foundry-toolbox-demo", "version": "1.0"},
            },
        )
        self.call("notifications/initialized", {}, notify=True)
        return result


def text_of(result: Any) -> str:
    """MCP tool results are content blocks; flatten the text ones."""
    if isinstance(result, dict):
        blocks = result.get("content") or []
        texts = [b.get("text", "") for b in blocks if isinstance(b, dict) and b.get("type") == "text"]
        if texts:
            return "\n".join(texts)
    return json.dumps(result, indent=2)


def rank_from(payload: str) -> list[str]:
    """Pull tool names out of a tool_search result, whatever shape it takes."""
    try:
        parsed = json.loads(payload)
    except json.JSONDecodeError:
        return []
    candidates = parsed.get("tools") if isinstance(parsed, dict) else parsed
    names: list[str] = []
    if isinstance(candidates, list):
        for item in candidates:
            if isinstance(item, dict) and item.get("name"):
                names.append(str(item["name"]))
            elif isinstance(item, str):
                names.append(item)
    return names


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--toolbox", required=True)
    parser.add_argument("--project-endpoint", default=os.environ.get("PROJECT_ENDPOINT", ""))
    parser.add_argument("--need", required=True, help="what you want, in natural language")
    parser.add_argument("--version", help="target a specific version instead of the default")
    parser.add_argument("--arguments", default="{}", help="JSON arguments for the routed tool")
    parser.add_argument("--no-invoke", action="store_true", help="show routing but don't call the winner")
    args = parser.parse_args()

    if not args.project_endpoint:
        print("error: --project-endpoint or PROJECT_ENDPOINT is required", file=sys.stderr)
        return 1

    base = args.project_endpoint.rstrip("/")
    url = (
        f"{base}/toolboxes/{args.toolbox}/versions/{args.version}/mcp?api-version=v1"
        if args.version
        else f"{base}/toolboxes/{args.toolbox}/mcp?api-version=v1"
    )
    token = DefaultAzureCredential().get_token("https://ai.azure.com/.default").token
    client = MCPClient(url, token)

    print(f"{BOLD}endpoint{RESET} {url}")
    info = client.initialize()
    server = (info or {}).get("serverInfo", {})
    print(f"{BOLD}server{RESET}   {server.get('name', '?')} {server.get('version', '')}")

    print(f"\n{BOLD}1. tools/list{RESET}")
    listing = client.call("tools/list", {})
    tools = (listing or {}).get("tools", [])
    for tool in tools:
        print(f"  {str(tool.get('name')):<24} {DIM}{(tool.get('description') or '')[:80]}{RESET}")

    names = {tool.get("name") for tool in tools}
    if "tool_search" not in names:
        print(
            f"\n{DIM}This toolbox has no tool_search meta-tool, so all {len(tools)} definitions are sent to the "
            f"model every turn. Add {{'type': 'toolbox_search'}} to the version to change that.{RESET}"
        )
        return 0

    print(f"\n{BOLD}2. tools/call tool_search{RESET}  query={args.need!r}")
    search_result = client.call("tools/call", {"name": "tool_search", "arguments": {"query": args.need}})
    payload = text_of(search_result)
    print(payload[:1500])

    ranked = rank_from(payload)
    if not ranked:
        print(f"\n{DIM}no ranked tool names parsed from the result — inspect the payload above{RESET}")
        return 0
    winner = ranked[0]
    print(f"\n{BOLD}routed to{RESET} {winner}   {DIM}(candidates: {', '.join(ranked[:5])}){RESET}")

    if args.no_invoke:
        return 0

    print(f"\n{BOLD}3. tools/call call_tool{RESET}  name={winner}")
    invoke_result = client.call(
        "tools/call",
        {"name": "call_tool", "arguments": {"name": winner, "arguments": json.loads(args.arguments)}},
    )
    print(text_of(invoke_result)[:2000])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
