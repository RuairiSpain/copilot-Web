# Conventions for people and coding agents

Read this before changing the repository.

## Layout

* `azure.yaml` is the only deployment definition. Do not add `agent.yaml` or
  `agent.manifest.yaml`. They are deprecated.
* `agents/<name>/` is one independently built Python project: its own `pyproject.toml`,
  `uv.lock`, tests and README. Do not share code between agent folders. Each folder is
  zipped or built on its own, so imports across folders break at deploy time.
* `tests/` holds the repo-level contract tests for `azure.yaml`.

## Rules

1. **Configuration lives in `azure.yaml`.** Keep the comment above every service. The contract
   test fails without it.
2. **Secrets never appear in files.** Use `${VAR}` in `azure.yaml` and `azd env set`. Do not
   set `FOUNDRY_*` or `AGENT_*` variables. The platform owns them.
3. **No work at import time.** Build clients, credentials and agents in functions. Tests
   import every module.
4. **Build per-request agents in a factory** when they hold a toolbox or MCP connection. This
   keeps each caller's identity separate.
5. **Every change needs a test.** Use fakes. Unit tests must not need network or Azure access.
6. **Prompts live in files** (`instructions.md`, `prompts/*.md`) so reviews can read them.
7. **Tool output is untrusted data.** Prompts must say so. Keep MCP tools on an allow-list and
   keep approval on for tools that write.

## Before you commit

```bash
make lock    # only if dependencies changed
make lint
make test
```

## Style

British English in prose and comments. Short sentences. Type hints everywhere. Docstrings on
public classes and functions. No em-dashes as separators.
