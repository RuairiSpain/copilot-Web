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
| PreToolUse Bash | `guard-bash.sh` | Blocks `azd down/up/provision/deploy`, `az` writes and deletes, force-push, hard resets, and credential-file reads, including inside compound commands. |
| PostToolUse Edit/Write | `post-edit.sh` | Runs `gofmt -w` on edited Go files; blocks if the file contains secret-shaped content. |
| Stop | `stop-check.sh` | Blocks ending the turn if `foundry-doctor/` contains secret-shaped content. |

Hooks enforce; the rules and skills guide. Permission `deny` entries are a second layer,
but they match command prefixes, which is why the hook also exists.

`guard-bash.sh` matches the command text, not its intent. A command that merely mentions a
blocked phrase (in a heredoc or commit message) is blocked too. Write such text with the
Write tool, or reword it.

## Deferred

`.claude/workflows/*.js` (dynamic workflows) and plugin packaging are deferred until
the agents and skills have run one phase reliably.
