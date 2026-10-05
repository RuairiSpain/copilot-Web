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

## Accepted risks (Phase 0 security review)

- **Context7 runs with the session environment.** `.mcp.json` pins `@upstash/context7-mcp@4.1.1`, but `npx -y` still executes
  the published package with the variables the session holds, and it sends query text to a third party. Do not put repository
  content or secrets in Context7 queries. Scrubbing the environment (`env -i` plus an explicit variable list) was not done
  because it can break the server's start-up; revisit with an integrity pin or a vendored install when the server is
  needed for a release workflow.
- **Interpreters are opaque to the Bash guard.** See `agent-swarm.md`. The mitigation is the credential the session holds.
- **GitHub Actions are tag-pinned** (`actions/checkout@v4`, `actions/setup-go@v5`) in `foundry-doctor-ci.yml`, matching the
  repository's other workflows. The PRD requires immutable SHA pins for release branches; `release.yml` (Phase 1) does that.
  The workflow has `permissions: contents: read` and no secrets or OIDC.
- **`Bash(git diff *)`, `git log *`, `git status *` stay wildcard allows.** The guard blocks `--output`, `--ext-diff` and
  `git -c` options that run programs or write files.
