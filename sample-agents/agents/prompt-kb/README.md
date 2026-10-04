# kb-prompt-agent (prompt agent)

A **prompt agent** answers from two sources: a PDF (`file_search`) and a remote MCP server.
It has no application code. Foundry runs the agent from its definition in the repo-level
`azure.yaml` (service `kb-prompt-agent`, `kind: prompt`).

## What lives here

| File | Purpose |
| --- | --- |
| `knowledge/field-handbook.pdf` | Sample PDF. Replace or add PDFs in this folder. |
| `ingest_knowledge.py` | Uploads the PDFs into a vector store. Idempotent. |
| `tests/` | Unit tests. They use fakes and need no Azure access. |

## Why the ingestion script exists

`azd` can declare the agent, but it cannot upload files. The `file_search` tool reads from a
vector store, so one step must create it. From the repo root:

```bash
export FOUNDRY_PROJECT_ENDPOINT="https://<account>.services.ai.azure.com/api/projects/<project>"
make ingest      # uploads PDFs and runs: azd env set KB_VECTOR_STORE_ID <id>
azd deploy kb-prompt-agent
```

Re-run `make ingest` whenever a PDF changes. Unchanged files are skipped.

## Design choices

* **Low temperature (0.2)** keeps answers close to the sources.
* **MCP approval is `always`**, with an `allowed_tools` list, so the agent can only call the
  read-only tool it needs and every call is reviewable.
* **The MCP token lives in a project connection**, never in the agent definition.
* **Direct tools, not a toolbox.** This agent is the only consumer of these tools. If a second
  agent needs them, move them into a toolbox like `search-and-code` (see ADR 0002).

## Tests

```bash
cd agents/prompt-kb && uv run pytest
```
