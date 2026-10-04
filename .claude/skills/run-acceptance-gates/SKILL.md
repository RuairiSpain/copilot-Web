---
name: run-acceptance-gates
description: Run the complete Foundry Doctor verification sequence and report exact results. Use after implementation, before a PR, or when asked whether the tree is green. Never changes requirements or weakens tests.
allowed-tools: Bash, Read, Grep
---

# Run acceptance gates

Run `foundry-doctor/scripts/claude/verify-phase.sh` and report its output verbatim in a table:
gate, command, result (pass / fail / skipped), and first actionable error.

The script runs, in order: `gofmt -l`, `git diff --check`, `go vet`, `go test ./...`,
`go test -race ./...`, `staticcheck`, `govulncheck`, `go build ./cmd/foundry-doctor`,
the rule-catalogue check, and the secret scan. A gate that cannot run (no `go.mod` yet,
tool not installed) is reported as skipped with the reason, never as passed.

If a tool is missing, run `foundry-doctor/scripts/install-dev-tools.sh` and re-run.
Do not repair a failure by weakening a test, editing a golden file to match wrong
output, or changing the requirement.
