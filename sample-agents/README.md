# sample-agents (azd project: foundry-agents)

> In the `copilot-web` repository this lives in `sample-agents/`. Run every command below
> from this folder, because `azd` looks for `azure.yaml` in the current directory.

Four Microsoft Foundry agents, one `azd` project, with unit tests and a documented
`azure.yaml`.

| Agent | Kind | Protocol | Tools |
| --- | --- | --- | --- |
| `kb-prompt-agent` | prompt | n/a | MCP server, PDF (file search) |
| `responses-agent` | hosted (Python) | Responses | AI Search, code interpreter (shared toolbox) |
| `invocations-agent` | hosted (Python) | Invocations | AI Search, code interpreter (shared toolbox) |
| `dev-team` | hosted (Python, Agent Framework) | Responses | Lead agent plus a Magentic team: planner, code writer, unit tester. Allow-listed CLI and git worktrees. |

Start with [`azure.yaml`](azure.yaml). It is the configuration reference, and every service is
explained in comments. See [`docs/architecture.md`](docs/architecture.md) for diagrams and
[`docs/adr/`](docs/adr) for the reasons behind the main choices.

## Layout

```
azure.yaml                 the whole deployment, documented
tests/                     contract tests for azure.yaml, with vendored azd schemas
agents/
  prompt-kb/               knowledge ingestion script (the prompt agent itself is in azure.yaml)
  responses-agent/         hosted agent, Responses protocol
  invocations-agent/       hosted agent, Invocations protocol, Pydantic contract
  dev-team/                lead agent, intake gate, Magentic team, shell, git worktrees
docs/                      architecture and decision records
AGENTS.md                  conventions for contributors
Makefile                   lock, lint, test, ingest, deploy
```

## Prerequisites

* Python 3.13 and [uv](https://docs.astral.sh/uv/)
* [Azure Developer CLI](https://learn.microsoft.com/azure/developer/azure-developer-cli/install-azd)
  1.32 or later, with the Foundry extensions: `azd ext install microsoft.foundry`
* An Azure subscription with quota for the model in `azure.yaml`
* An Azure AI Search service and index (for the two hosted agents that search)
* Docker, only for the `dev-team` container build

## Deploy

```bash
azd auth login
azd env new dev
azd env set SEARCH_ENDPOINT   https://<service>.search.windows.net
azd env set SEARCH_INDEX_NAME product-docs
azd env set KB_MCP_SERVER_URL https://<your-mcp-server>/mcp
azd env set KB_MCP_TOKEN      <token>            # stored in the connection, not in git

azd provision                                    # project, model, connections
export FOUNDRY_PROJECT_ENDPOINT=$(azd env get-value FOUNDRY_PROJECT_ENDPOINT)
make ingest                                      # upload the PDF, saves KB_VECTOR_STORE_ID
azd deploy --all                                 # toolbox and all four agents
```

Grant the project's managed identity **Search Index Data Reader** on the search service before
the first call. Run `azd deploy --all` again after any toolbox or agent change.

## Try each agent

```bash
azd ai agent invoke responses-agent   "How do I rotate an API key?"
azd ai agent invoke invocations-agent '{"question": "How do I rotate an API key?"}'
azd ai agent invoke dev-team          "I want a small library that makes URL slugs."
```

Run one locally with `azd ai agent run` (it listens on `http://localhost:8088`).
The prompt agent is called through the Responses API with an `agent_reference`.

## Test

```bash
make test      # contract tests plus every agent's unit tests, offline
make lint      # ruff and mypy
```

Tests use fakes. They need no Azure access. The dev-team tests run real `git` and `pytest`
in temporary folders.

## Things to check on first deploy

I wrote and tested this offline. These items follow the documentation but were not exercised
against a live Foundry project:

1. The toolbox endpoint variable `TOOLBOX_SEARCH_AND_CODE_MCP_ENDPOINT` follows the naming in
   Microsoft's toolbox sample. Run `azd env get-values` after the first deploy to confirm it.
2. `allowed_tools: [search_docs]` on the prompt agent is a placeholder. Use the tool name your
   MCP server exposes.
3. The search connection uses `authType: AAD`, and the toolbox maps it to an index with
   `connections[].index`. Check the tool list with `azd ai toolbox show search-and-code`.
4. `dev-team` uses container mode because it needs `git`. Whether the code-mode runtime ships
   git was not confirmed.
5. `agent-framework-foundry-hosting` is a prerelease package. Expect API changes.
6. The model name and version in `azure.yaml` are examples. Pick current values for your region.
