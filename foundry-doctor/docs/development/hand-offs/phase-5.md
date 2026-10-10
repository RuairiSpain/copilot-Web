# Phase 5 hand-off: dependency and deployment graph outputs

## Delivered
- `foundry-doctor graph` CLI with `--source`, `--deployed`, and `--combined` modes.
- Deterministic graph projection in `internal/graph/view` with source, inventory, runtime, and findings overlays.
- Renderers for Mermaid, DOT, JSON, Markdown, and HTML wrappers.
- Size controls with deterministic collapse/edge limits.
- Runtime overlay support for Foundry projects, connections, capability hosts, and deployments.
- Redaction/sanitization for unsafe labels, runtime error text, and optional deterministic identifier obfuscation.
- Golden, determinism, injection, collapse, correlation, truncation, and redaction tests.
- User docs and sample graph artifacts.

## Changed contracts
- Added CLI entrypoint `foundry-doctor graph`.
- Added `app.Graph` / `GraphRequest` request-validation and rendering flow.
- Added graph JSON schema `schemas/graph.schema.json`.
- Added connection `targets` edges and unmatched-findings summary accounting.

## Rule catalogue changes
- None.

## Commands implemented
- `foundry-doctor graph --source`
- `foundry-doctor graph --deployed`
- `foundry-doctor graph --combined`
- Formats: `mermaid`, `json`, `markdown`, `dot`, `html`

## Tests executed and results
- `gofmt -l .` → pass (empty)
- `go vet ./...` → pass
- `go test -count=1 ./...` → pass
- `staticcheck ./...` → pass
- `govulncheck ./...` → pass (`No vulnerabilities found.`)
- `bash scripts/claude/check-no-secrets.sh` → pass
- `bash scripts/claude/check-rule-catalog.sh` → pass (`catalogue valid: 108 rules`)
- `bash scripts/claude/verify-phase.sh` → all runnable gates passed; `go test -race ./...` skipped on windows/arm64

## Security and privacy review
- Mermaid/DOT output uses opaque node IDs and escaped labels; injection regressions added.
- Runtime error text is reduced to sanitized health summaries (`error reported`) rather than raw backend text.
- `--redact-ids` now clears names, resource IDs, health text, and source locations while replacing labels with deterministic obfuscated labels.
- Placeholder external/missing nodes no longer serialize raw target/reference strings in `name`.

## Foundry lead review
- Final signoff review reported no remaining blocking/high/medium issues in the scoped Phase 5 graph changes.

## Azure black belt review
- Final signoff review reported no remaining blocking/high issues in the scoped Phase 5 graph changes.

## Known limitations
- Runtime overlay still depends on the shared Azure adapter’s allow-listed Foundry ARM APIs and inherited API-version policy.
- Account deployments are rendered as account-scoped evidence; project graphs do not infer project ownership of account-wide deployments without explicit evidence.
- `go test -race` remains unsupported on windows/arm64.

## Deferred issues with rationale
- No Phase 5 blockers remain after remediation and gate re-runs.
- Shared Azure transport API-version policy was not widened in this phase because it is cross-cutting adapter infrastructure rather than graph-specific logic.

## Compatibility matrix
- Validated on Windows ARM64 with Go `1.25.13` and `GOTOOLCHAIN=local`.
- Offline/source mode works without Azure credentials.
- Deployed/combined modes require Azure inventory access and fail unavailable on truncated inventory collection.

## Artefacts for next phase
- `docs/graph.md`
- `schemas/graph.schema.json`
- `samples/graph/`
- `internal/graph/{view,mermaid,json,graphviz}/`
- `internal/report/graph/render.go`

## Exact next-phase prerequisites
- Reuse graph JSON schema and renderers for any future richer topology or environment-difference views.
- If future phases need stronger privacy guarantees than deterministic obfuscation, replace the current label mapping with keyed/session-scoped pseudonyms.

## Definition of Done audit
| DoD item | Status (met / unmet / requirement changed by ADR-NNN) | Current evidence |
|---|---|---|
| Mermaid, DOT, JSON, and Markdown graph outputs implemented | met | `internal/graph/{mermaid,json,graphviz}`, `internal/report/graph`, CLI tests |
| Graph output is deterministic across input ordering | met | `internal/graph/view/view_test.go` determinism coverage; stable node/edge sorting |
| Untrusted labels/runtime text are safely escaped or sanitized | met | Mermaid/DOT injection tests; runtime health sanitization tests |
| Optional identifier redaction is available and preserves structure | met | `--redact-ids` implementation, schema/docs updates, redaction tests |
| Large graphs can be collapsed/limited deterministically | met | collapse/edge-limit logic in `view.go`, collapse tests |
| Runtime evidence and findings overlay are included without changing validation semantics | met | `app.Graph`, findings overlay counts/tests, runtime read-only overlay wiring |
| Command wiring, docs, samples, and gates are complete | met | `cmd/foundry-doctor/graph.go`, `docs/graph.md`, `samples/graph/`, green gate runs |
