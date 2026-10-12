---
name: run-acceptance-gates
description: Run and report the complete Foundry Doctor verification sequence after implementation, before a pull request, or when checking whether the tree is green.
---

# Run Foundry Doctor acceptance gates

Run `foundry-doctor/scripts/claude/verify-phase.sh` and report a table containing gate, command, pass/fail/skipped result, and the first actionable error.

The expected sequence is `gofmt -l`, `git diff --check`, `go vet`, `go test ./...`, `go test -race ./...`, `staticcheck`, `govulncheck`, product build when present, rule-catalogue checks, and secret scan.

A gate that cannot run is skipped with its reason, never passed. Install repository-declared development tools only when they are missing. Never make a gate pass by weakening a test, changing a correct golden file to wrong output, or changing a requirement.
