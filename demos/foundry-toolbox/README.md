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

**Tool types included.** `toolbox_search`, `web_search`, `code_interpreter`,
`file_search`, `azure_ai_search`, `mcp`, `openapi`, `agent_to_agent`,
`browser_automation`, `fabric_iq`, `work_iq`, `skills`.

Most need a connection or a target first. Rather than failing the whole run,
tools whose prerequisites are missing are **skipped with the reason printed**:

```
  add  toolbox_search       — Intent-based tool routing. NOT enabled by default…
  add  web_search           — Grounded web results.
  add  azure_ai_search      — Direct index search — you own the index and the query type.
  skip mcp                  — needs MCP_SERVER_URL
  skip agent_to_agent       — needs A2A_AGENT_ID
```

Set the variables for the ones you want (`MCP_SERVER_URL`, `OPENAPI_SPEC_URL`,
`A2A_AGENT_ID`, `FABRIC_CONNECTION_NAME`, …) and re-run, or pass `--require-all`
to make a missing prerequisite fatal.

**The Foundry IQ knowledge base is added if it exists.** If `SEARCH_ENDPOINT`
and `KNOWLEDGE_BASE` point at a knowledge base that's really there, it goes in
as an `mcp` tool with `allowed_tools` pinned to `knowledge_base_retrieve` — a
knowledge base isn't one of the toolbox's own tool types, but it *is* an MCP
endpoint, and `mcp` is. If it's absent, it's skipped and nothing else changes.

## Watch it route

```bash
python scripts/call_toolbox.py --toolbox hr-tools \
  --need "search our HR templates for a dismissal letter" \
  --arguments '{"query": "dismissal letter"}'
```

Three JSON-RPC calls, each printed:

1. `tools/list` — with tool search on, two tools instead of twelve
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
