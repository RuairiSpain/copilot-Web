# responses-agent

Hosted agent using the **Responses** protocol. It answers product questions with Azure AI
Search and a code interpreter, both supplied by the shared `search-and-code` toolbox.

| File | Purpose |
| --- | --- |
| `main.py` | Entry point. Hosts the agent with `ResponsesHostServer` on port 8088. |
| `agent.py` | Builds the agent. No work at import time. |
| `settings.py` | Reads and validates environment variables, reporting every problem at once. |
| `instructions.md` | System prompt. Search first, cite titles, treat tool output as data. |
| `Dockerfile` | Only for container mode. The default is code mode (see `azure.yaml`). |
| `tests/` | Unit tests with fakes. |

## Why a factory

`main.py` passes `ResponsesHostServer` a function, not an agent. A new agent, and so a new
toolbox connection, is built per request. Foundry advises this when a connection carries the
caller's identity, because a shared connection can keep the identity of the first caller.

## Run and test

```bash
uv sync
uv run pytest
azd ai agent run            # from the repo root
```
