"""The tool catalogue this demo puts into a toolbox.

Tools are declared as plain dicts in the REST/YAML shape rather than SDK model
classes, for two reasons: the same declaration then feeds all three creation
paths (SDK, REST, `azd ai toolbox create --from-file`), and the audience sees
the literal payload the service takes.

One correction worth making loudly, because it is a common assumption:

    Tool search is NOT automatic.

A toolbox does not route by intent just because it has many tools. You have to
add `{"type": "toolbox_search"}` to the version's tool list. Doing so swaps the
full tool-definition dump for two meta-tools — `tool_search` (find a capability
in natural language) and `call_tool` (invoke it by name) — with BM25 over tool
metadata behind them. Without it, every definition is passed to the model on
every turn, which is the token problem the feature exists to solve.
"""

from __future__ import annotations

from typing import Any

# Each entry: (id, description shown in the CLI, payload).
# `needs` names the environment variables required before the tool can be added,
# so `create_toolbox.py` can skip cleanly instead of failing the whole run.
CATALOGUE: list[dict[str, Any]] = [
    {
        "id": "toolbox_search",
        "about": "Intent-based tool routing. NOT enabled by default — this entry is what enables it.",
        "needs": [],
        "payload": {"type": "toolbox_search"},
    },
    {
        "id": "web_search",
        "about": "Grounded web results.",
        "needs": [],
        "payload": {"type": "web_search"},
    },
    {
        "id": "code_interpreter",
        "about": "Sandboxed Python execution.",
        "needs": [],
        "payload": {"type": "code_interpreter"},
    },
    {
        "id": "file_search",
        "about": "Search over files uploaded to the project.",
        "needs": [],
        "payload": {"type": "file_search"},
    },
    {
        "id": "azure_ai_search",
        "about": "Direct index search — you own the index and the query type.",
        "needs": ["AI_SEARCH_CONNECTION_ID", "AI_SEARCH_INDEX"],
        "payload": {
            "type": "azure_ai_search",
            "name": "hr_index_search",
            "description": "Hybrid search over the HR templates index with metadata filters.",
            "project_connection_id": "${AI_SEARCH_CONNECTION_ID}",
            "index_name": "${AI_SEARCH_INDEX}",
            "query_type": "vector_semantic_hybrid",
            "top_k": 5,
        },
    },
    {
        "id": "mcp",
        "about": "Any MCP server, including one you host yourself.",
        "needs": ["MCP_SERVER_URL"],
        "payload": {
            "type": "mcp",
            "server_label": "demo_mcp",
            "server_url": "${MCP_SERVER_URL}",
            "require_approval": "never",
            "project_connection_id": "${MCP_CONNECTION_NAME}",
        },
    },
    {
        "id": "openapi",
        "about": "Any REST API described by an OpenAPI spec.",
        "needs": ["OPENAPI_SPEC_URL"],
        "payload": {
            "type": "openapi",
            "name": "demo_openapi",
            "description": "A REST API exposed to the agent from its OpenAPI description.",
            "spec_url": "${OPENAPI_SPEC_URL}",
            "project_connection_id": "${OPENAPI_CONNECTION_NAME}",
        },
    },
    {
        "id": "agent_to_agent",
        "about": "Delegate to another agent — the routing boundary that also crosses a model boundary.",
        "needs": ["A2A_AGENT_ID"],
        "payload": {
            "type": "agent_to_agent",
            "name": "specialist_agent",
            "description": "Delegate specialist questions to a connected agent.",
            "agent_id": "${A2A_AGENT_ID}",
        },
    },
    {
        "id": "browser_automation",
        "about": "Drive a real browser for sites with no API.",
        "needs": ["BROWSER_CONNECTION_NAME"],
        "payload": {
            "type": "browser_automation",
            "project_connection_id": "${BROWSER_CONNECTION_NAME}",
        },
    },
    {
        "id": "fabric_iq",
        "about": "Governed, schema-aware analytics over Fabric.",
        "needs": ["FABRIC_CONNECTION_NAME"],
        "payload": {"type": "fabric_iq", "project_connection_id": "${FABRIC_CONNECTION_NAME}"},
    },
    {
        "id": "work_iq",
        "about": "Microsoft 365 work context.",
        "needs": ["WORK_IQ_CONNECTION_NAME"],
        "payload": {"type": "work_iq", "project_connection_id": "${WORK_IQ_CONNECTION_NAME}"},
    },
    {
        "id": "skills",
        "about": "Foundry skills attached as MCP resources.",
        "needs": ["SKILL_NAME"],
        "payload": {"type": "skills", "skills": ["${SKILL_NAME}"]},
    },
]


def knowledge_base_tool(kb_name: str, mcp_url: str) -> dict[str, Any]:
    """The Foundry IQ knowledge base, added to the toolbox as an MCP tool.

    A knowledge base is not one of the toolbox's own tool types, but it *is* an
    MCP endpoint exposing a single `knowledge_base_retrieve` tool — and `mcp` is
    a supported type. So it goes in as a plain MCP server, with the allowed
    tool list pinned so the model can't reach anything else on that endpoint.
    """
    return {
        "type": "mcp",
        "server_label": kb_name.replace("-", "_"),
        "server_url": mcp_url,
        "require_approval": "never",
        "allowed_tools": ["knowledge_base_retrieve"],
    }
