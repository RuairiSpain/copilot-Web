# MCP setup

Configured in the repo-root `.mcp.json`. Check with `/mcp` in Claude Code.

| Server | Why | Setup |
|---|---|---|
| gopls | Symbol-aware navigation, references and diagnostics for the Go module | Run `foundry-doctor/scripts/install-dev-tools.sh`. The server starts in `foundry-doctor/` and needs `go.mod` to be useful (created in Phase 0/1). Docs: https://go.dev/gopls/features/mcp |
| context7 | Current docs for Go packages, Azure SDK for Go, Cobra, Viper, azd | Runs `npx -y @upstash/context7-mcp`. Set `CONTEXT7_API_KEY` in your shell for higher rate limits. Never commit the key. |

Not added, with reasons:

- GitHub: the Claude Code on the web session already provides it.
- Git and Filesystem: Claude Code's built-in Bash, Read, Grep and Glob cover these, and `.claude/settings.json` constrains them.
- Azure MCP: it can reach live subscriptions, which conflicts with the read-only, offline-first
  principle for development. Add it later, with a read-only identity, if Phase 2 needs it.
- Postgres/SQLite: nothing stores baselines in a database; they are YAML files.
