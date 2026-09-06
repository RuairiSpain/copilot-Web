"""The tool catalogue this demo puts into a toolbox.

Every `type` string and every field below was verified against the
`ToolboxToolType` enum and the `*ToolboxTool` model classes in
**azure-ai-projects 2.6.0**, not against doc prose. Several of the names that
read naturally are wrong — the enum is the authority:

    a2a                          NOT "agent_to_agent"
    browser_automation_preview   NOT "browser_automation"
    fabric_iq_preview            NOT "fabric_iq"
    work_iq_preview              NOT "work_iq"

and `skills` is **not a toolbox tool type at all** — it is a separate
`create_version(..., skills=[...])` parameter, so it is handled there rather
than here.

Two more shapes worth knowing, because both read as flat and are not:

    azure_ai_search  ->  {"type": ..., "azure_ai_search": {"indexes": [ ... ]}}
    openapi          ->  {"type": ..., "openapi": {"name", "spec", "auth", ...}}

`spec` is the OpenAPI document inline as a dict. There is no `spec_url` field.

And one that is smaller than it looks: `MCPToolboxTool` has **no**
`require_approval` and **no** `allowed_tools`. Those belong to `MCPTool`, the
agent-level tool. A toolbox MCP entry carries only server_label, server_url,
connector_id, tunnel_id, authorization, server_description, headers,
defer_loading and project_connection_id.

Finally, the one assumption worth correcting out loud:

    Tool search is NOT automatic.

A toolbox does not route by intent just because it has many tools. You add
`{"type": "toolbox_search"}` to the version's tool list. Doing so swaps the
full tool-definition dump for two meta-tools — `tool_search` (find a capability
in natural language) and `call_tool` (invoke it by name) — with BM25 over tool
metadata behind them. Without it, every definition is passed to the model on
every turn, which is the token problem the feature exists to solve.
"""

from __future__ import annotations

from typing import Any

# The complete set, straight from azure.ai.projects.models._enums.ToolboxToolType.
# Anything not in here will be rejected by the service; the unit tests assert
# that every `type` this module emits is a member.
TOOLBOX_TOOL_TYPES = {
    "code_interpreter",
    "file_search",
    "web_search",
    "mcp",
    "azure_ai_search",
    "openapi",
    "a2a",
    "a2a_preview",
    "browser_automation_preview",
    "reminder_preview",
    "work_iq_preview",
    "fabric_iq_preview",
    "toolbox_search",
    "toolbox_search_preview",
    "shell",
    "web_iq_preview",
}

# Types whose name ends in _preview need AIProjectClient(..., allow_preview=True).
PREVIEW_TOOL_TYPES = {t for t in TOOLBOX_TOOL_TYPES if t.endswith("_preview")}

# Each entry: id, one-line description, env vars required, and the payload.
# `needs` lets create_toolbox.py skip cleanly instead of failing the whole run.
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
        "payload": {"type": "web_search", "search_context_size": "medium"},
    },
    {
        "id": "code_interpreter",
        "about": "Sandboxed Python execution.",
        "needs": [],
        "payload": {"type": "code_interpreter"},
    },
    {
        "id": "file_search",
        "about": "Search over vector stores in the project.",
        "needs": [],
        "payload": {"type": "file_search", "max_num_results": 5},
    },
    {
        "id": "shell",
        "about": "Shell command execution.",
        "needs": [],
        "payload": {"type": "shell"},
    },
    {
        "id": "azure_ai_search",
        "about": "Direct index search — you own the index and the query type.",
        "needs": ["AI_SEARCH_CONNECTION_ID", "AI_SEARCH_INDEX"],
        # Nested under `azure_ai_search`, per AzureAISearchToolboxTool.
        "payload": {
            "type": "azure_ai_search",
            "azure_ai_search": {
                "indexes": [
                    {
                        "project_connection_id": "${AI_SEARCH_CONNECTION_ID}",
                        "index_name": "${AI_SEARCH_INDEX}",
                        # AzureAISearchQueryType: simple | semantic | vector |
                        # vector_simple_hybrid | vector_semantic_hybrid
                        "query_type": "vector_semantic_hybrid",
                        "top_k": 5,
                    }
                ]
            },
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
            "server_description": "A demo MCP server exposed to the agent through the toolbox.",
            "project_connection_id": "${MCP_CONNECTION_NAME}",
        },
    },
    {
        "id": "openapi",
        "about": "A REST API described by an inline OpenAPI document.",
        "needs": ["OPENAPI_SPEC_FILE"],
        # `spec` is the document itself, not a URL. create_toolbox.py loads
        # OPENAPI_SPEC_FILE into this slot before sending.
        "payload": {
            "type": "openapi",
            "openapi": {
                "name": "demo_openapi",
                "description": "A REST API exposed to the agent from its OpenAPI description.",
                "spec": {},
                "auth": {"type": "anonymous"},
            },
        },
    },
    {
        "id": "a2a",
        "about": "Delegate to another agent — a routing boundary that also crosses a model boundary.",
        "needs": ["A2A_BASE_URL"],
        "payload": {
            "type": "a2a",
            "base_url": "${A2A_BASE_URL}",
            "agent_card_path": "${A2A_AGENT_CARD_PATH}",
            "project_connection_id": "${A2A_CONNECTION_NAME}",
        },
    },
    {
        "id": "browser_automation_preview",
        "about": "Drive a real browser for sites with no API. (preview)",
        "needs": ["BROWSER_CONNECTION_NAME"],
        "payload": {"type": "browser_automation_preview", "project_connection_id": "${BROWSER_CONNECTION_NAME}"},
    },
    {
        "id": "fabric_iq_preview",
        "about": "Governed, schema-aware analytics over Fabric. (preview)",
        "needs": ["FABRIC_CONNECTION_NAME"],
        "payload": {"type": "fabric_iq_preview", "project_connection_id": "${FABRIC_CONNECTION_NAME}"},
    },
    {
        "id": "work_iq_preview",
        "about": "Microsoft 365 work context. (preview)",
        "needs": ["WORK_IQ_CONNECTION_NAME"],
        "payload": {"type": "work_iq_preview", "project_connection_id": "${WORK_IQ_CONNECTION_NAME}"},
    },
    {
        "id": "web_iq_preview",
        "about": "Web IQ knowledge. (preview)",
        "needs": ["WEB_IQ_CONNECTION_NAME"],
        "payload": {"type": "web_iq_preview", "project_connection_id": "${WEB_IQ_CONNECTION_NAME}"},
    },
    {
        "id": "reminder_preview",
        "about": "Schedule a reminder back into the session. (preview)",
        "needs": [],
        "payload": {"type": "reminder_preview"},
    },
]


def knowledge_base_tool(kb_name: str, mcp_url: str) -> dict[str, Any]:
    """The Foundry IQ knowledge base, added to the toolbox as an MCP tool.

    A knowledge base is not one of the toolbox's own tool types, but it *is* an
    MCP endpoint exposing a single `knowledge_base_retrieve` tool — and `mcp` is
    a supported type.

    Note what is deliberately absent: `allowed_tools` and `require_approval`.
    Both exist on the agent-level `MCPTool` and neither exists on
    `MCPToolboxTool`, so pinning the callable surface has to happen on the
    agent that consumes the toolbox, not here. `server_description` is what
    steers `tool_search` toward this entry instead.
    """
    return {
        "type": "mcp",
        "server_label": kb_name.replace("-", "_"),
        "server_url": mcp_url,
        "server_description": (
            f"Foundry IQ knowledge base '{kb_name}'. Exposes knowledge_base_retrieve for grounded, "
            "cited answers over EU employment directives and HR document templates."
        ),
    }
