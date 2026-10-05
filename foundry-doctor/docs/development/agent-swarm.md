# Agent swarm

How the Claude Code harness maps to PRD section 19. Files live in the repo-root `.claude/`.

| PRD role | Agent | Writes code | Notes |
|---|---|---|---|
| Orchestrator | main session, via skill `implement-prd-phase` | yes | Subagents cannot spawn subagents, so the main session coordinates. |
| (planning, audit) | `implementation-coordinator` | no | Produces the DoD-to-work map and audits completion. |
| Lead architect | `lead-architect` | ADRs, contracts | Settles contracts before parallel work starts. |
| Implementation | `go-implementer` | yes, in a worktree | One per work packet; disjoint packages. |
| Bicep specialist | `bicep-specialist` | `internal/bicep`, spikes | |
| Rule catalogue owner | `rule-catalogue-owner` | catalogue, overlap docs | |
| QA / adversarial | `test-engineer` | tests only | |
| DevOps / release | `devops-release` | workflows, packaging | |
| Documentation | `documentation-reviewer` | docs only | |
| Foundry lead (review) | `foundry-lead` | no | Independent. |
| Azure black belt (review) | `azure-black-belt` | no | Independent. |
| Go principal (review) | `go-principal-engineer` | no | Independent. |
| Security / privacy review | `security-reviewer` | no | Independent. |

Reviewers have no Edit or Write tool. A reviewer is never given another reviewer's
findings until all reviews are in. Skills: `implement-prd-phase`, `add-foundry-rule`,
`research-rule-overlap`, `review-go-change`, `run-acceptance-gates`, `prepare-release`.

Path-scoped rules (`.claude/rules/foundry-doctor-*.md`) load only when matching files are touched.

## Hooks (`.claude/settings.json`)

| Event | Script | Effect |
|---|---|---|
| PreToolUse Bash | `guard-bash.sh` -> `guard_bash.py` | Tokenises the command and looks through `sh -c`, `eval`, `env`, `sudo`, `xargs`, `find -exec`, `$(...)` and backticks. Allows only read-only `az` and `azd` verbs and `az rest` GET. Blocks force, delete and mirror pushes (all flag and refspec forms), hard resets, mutating requests to Azure endpoints, code piped from the network into an interpreter, commands whose name comes from a variable, and reads of credential files. |
| PostToolUse Edit/Write | `post-edit.sh` | Runs `gofmt -w` on edited Go files; blocks if the file contains secret-shaped content. |
| Stop | `stop-check.sh` | Blocks ending the turn if the project, the Claude harness or this project's workflow contain secret-shaped content. Runs even when the stop hook is already active. |

All three fail closed: empty or malformed hook input, a missing `jq` or `python3`, or a command the guard
cannot parse blocks the action. Secret scans also cover `.claude/`, `.mcp.json` and the project workflow,
not only `foundry-doctor/`.

**The primary control is the credential the session holds.** Run agent sessions with a read-only Azure identity, or none,
and keep `AZURE_*`, `GITHUB_TOKEN` and similar variables and any `az login` or `azd auth` state out of the session
environment. Any command can be hidden inside `python3`, `node` or `awk`, and no command-text guard can see inside them.

**The guard is a speed bump, not a security boundary.** It cannot see inside scripts or interpreters
(`python3 script.py`, `node`), so a determined or compromised agent can bypass it. The real boundaries are the
permission rules in `.claude/settings.json` (exact command forms only, no wildcards on `go test`, `go run` or scripts)
and the credentials the session holds: give the session a read-only identity, or none. The guard's unit tests
(`test_guard_bash.py`) list the bypass forms found in the Phase 0 security review and run in `verify-phase.sh`.

The guard inspects commands, not prose. Quoted arguments and heredoc bodies are not treated as commands, so
a commit message that mentions a blocked phrase is allowed.

## Deferred

`.claude/workflows/*.js` (dynamic workflows) and plugin packaging are deferred until
the agents and skills have run one phase reliably.
