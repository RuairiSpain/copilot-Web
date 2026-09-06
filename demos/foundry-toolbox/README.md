# Foundry Toolbox demo — every tool type, one MCP endpoint

A toolbox is a curated, versioned collection of tools exposed behind **one
MCP-compatible endpoint** that any runtime can consume — Foundry Agent Service,
Agent Framework, LangGraph, the Copilot SDK, or a bare MCP client like the one
in `call_toolbox.py`. Auth, governance and versioning live with the toolbox
rather than with each agent.

## ⚠️ Tool search is not automatic

You assumed the tool-search feature kicks in whenever you request a tool from
the toolbox. **It doesn't — you have to opt in**, by adding
`{"type": "toolbox_search"}` to the toolbox version's tool list.

Without it, every tool definition is passed to the model on every turn. The
docs are blunt about why that hurts: token cost grows with each tool added,
the context fills with definitions the task doesn't need, and the model picks
worse from a crowded list.

With it, the endpoint advertises **two** meta-tools instead of the catalogue:

| Tool | What it does |
| --- | --- |
| `tool_search` | model describes the capability it needs; BM25 over tool metadata returns ranked candidates |
| `call_tool` | model invokes the winner by name |

`create_toolbox.py` puts it in by default and warns if you exclude it.
`call_toolbox.py` shows the two-step routing happening live.

## SDK version

Everything here targets **azure-ai-projects 2.x** (verified against 2.6.0), which
is where `project.toolboxes`, the `ToolboxToolType` discriminators and the
`allow_preview` flag live. `requirements.txt` pins `>=2.6.0,<3`.

One caveat on the published SDK snippets: the docs show
`update(toolbox_name=…)`, `delete_toolbox_version(…)` and
`list_toolbox_versions(…)`. In 2.6.0 the real methods are `update(name, *,
default_version)`, `delete_version(name, version)` and `list_versions(name)`.
Check `dir(project.toolboxes)` rather than the prose.

## Create the toolbox

```bash
pip install -r requirements.txt
export PROJECT_ENDPOINT="https://<account>.services.ai.azure.com/api/projects/<project>"

# emit YAML for the CLI (default — nothing to install, reviewable in a PR)
python scripts/create_toolbox.py --toolbox hr-tools --via yaml
azd ai toolbox create hr-tools --from-file toolbox.yaml

# or go straight to the service
python scripts/create_toolbox.py --toolbox hr-tools --via sdk
python scripts/create_toolbox.py --toolbox hr-tools --via rest
```

All three paths send the same declaration from `scripts/toolbox_spec.py`, so
you can show the SDK call and then show the YAML a platform team would check in.

**Tool types included**, with the exact `ToolboxToolType` discriminators from
azure-ai-projects 2.6.0:

`toolbox_search` · `web_search` · `code_interpreter` · `file_search` · `shell` ·
`azure_ai_search` · `mcp` · `openapi` · `a2a` · `browser_automation_preview` ·
`fabric_iq_preview` · `work_iq_preview` · `web_iq_preview` · `reminder_preview`

### Four names that read naturally and are wrong

These were verified against the SDK enum rather than doc prose, and four of the
obvious spellings are invalid:

| Reads right | Actually is |
| --- | --- |
| `agent_to_agent` | **`a2a`** (also `a2a_preview`) |
| `browser_automation` | **`browser_automation_preview`** |
| `fabric_iq` | **`fabric_iq_preview`** |
| `work_iq` | **`work_iq_preview`** |

And `skills` is **not a tool type at all** — it's a separate `create_version`
parameter, so pass it with `--skill NAME`.

`create_toolbox.py` validates every emitted `type` against the enum before
sending anything, so an invented name fails locally instead of as a service
400. Any `*_preview` type additionally requires
`AIProjectClient(..., allow_preview=True)`, which the script sets automatically
when the selection contains one.

### Two shapes that are nested, not flat

```yaml
- type: azure_ai_search          # NOT flat index_name/top_k
  azure_ai_search:
    indexes:
      - project_connection_id: my-conn
        index_name: hr-templates-index
        query_type: vector_semantic_hybrid   # simple | semantic | vector |
        top_k: 5                             # vector_simple_hybrid | vector_semantic_hybrid

- type: openapi                  # `spec` is the document inline; there is no spec_url
  openapi:
    name: demo_openapi
    spec: { ... }
    auth: { type: anonymous }
```

`MCPToolboxTool` is also smaller than it looks: it has **no `require_approval`
and no `allowed_tools`**. Both belong to the agent-level `MCPTool`, so pinning
the callable surface of an MCP server happens on the agent that consumes the
toolbox, not in the toolbox. `server_description` is what steers `tool_search`
toward an entry.

Most need a connection or a target first. Rather than failing the whole run,
tools whose prerequisites are missing are **skipped with the reason printed**:

```
  add  toolbox_search               — Intent-based tool routing. NOT enabled by default…
  add  web_search                   — Grounded web results.
  add  azure_ai_search              — Direct index search — you own the index and the query type.
  skip mcp                          — needs MCP_SERVER_URL
  skip a2a                          — needs A2A_BASE_URL
  skip fabric_iq_preview            — needs FABRIC_CONNECTION_NAME
```

Set the variables for the ones you want (`MCP_SERVER_URL`, `OPENAPI_SPEC_FILE`,
`A2A_BASE_URL`, `FABRIC_CONNECTION_NAME`, …) and re-run, or pass `--require-all`
to make a missing prerequisite fatal.

**The Foundry IQ knowledge base is added if it exists.** If `SEARCH_ENDPOINT`
and `KNOWLEDGE_BASE` point at a knowledge base that's really there, it goes in
as an `mcp` tool — a knowledge base isn't one of the toolbox's own tool types,
but it *is* an MCP endpoint, and `mcp` is. If it's absent, it's skipped and
nothing else changes.

## Watch it route

```bash
python scripts/call_toolbox.py --toolbox hr-tools \
  --need "search our HR templates for a dismissal letter" \
  --arguments '{"query": "dismissal letter"}'
```

Three JSON-RPC calls, each printed:

1. `tools/list` — with tool search on, two meta-tools instead of the whole catalogue
2. `tools/call tool_search` — the ranked candidates, i.e. the routing decision
3. `tools/call call_tool` — invoking the winner

Add `--version <id>` to hit the developer endpoint for a specific version
instead of the consumer endpoint, which always serves the default.

Versions are immutable; the first becomes default automatically and later ones
need promoting:

```bash
azd ai toolbox version list hr-tools
azd ai toolbox publish hr-tools <version_id>
```

## Inventory a project

```bash
python scripts/inventory.py --all --out inventory.md
python scripts/inventory.py --models --agents --toolboxes
python scripts/inventory.py --iq --sources --indexes --format json
```

Switches: `--all`, `--models`, `--agents`, `--tools`, `--toolboxes`, `--skills`,
`--connections`, `--routines`, `--gateways`, `--iq`, `--sources`, `--indexes`.

It queries three planes — the project data plane, the search service, and ARM
via `az` — and writes a Markdown report with a status table up front.

**Every collector degrades instead of failing**, and this matters for how you
read the output. A section marked unavailable was probed and didn't answer:
the surface may not exist at that path in your api-version, may not be enabled
on the account, or your identity may lack the role. The reason column says
which. Several of these are preview surfaces that move between api-versions, so
treat the report as a capability probe of *your* project, not as a statement
about the product. `--models` additionally needs `--account` and
`--resource-group`, since deployments are an ARM concern rather than a project
one.

## Tests

```bash
python -m unittest discover -s ../tests -v
```

Covers the YAML emitter (round-tripped through PyYAML, including the
colon-in-a-string case), tool selection and prerequisite skipping, placeholder
pruning, and the inventory's Markdown rendering and pipe escaping. The Azure
calls are not covered — there's no subscription in CI, and `azure.identity` is
stubbed at import.
