# Phase 6 hand-off: annotate outputs

## Delivered
- `foundry-doctor annotate` CLI with `review`, `github`, and `sarif` formats.
- Review-copy generation under `.review` with deterministic inline comments for `azure.yaml` and exact Bicep compiler diagnostics.
- `annotations-manifest.json` plus optional `annotations.diff`.
- Shared canonical location handling in `internal/location` and deterministic unified diffs in `internal/diff`.
- Validation that review-copy YAML still parses and that annotated Bicep preserves compile result, diagnostics, and compiled ARM output.
- Path-confinement, injection, truncation, idempotence, overlap, and output-safety tests.

## Changed contracts
- Added CLI entrypoint `foundry-doctor annotate`.
- Added `app.Annotate` / `AnnotateRequest`.
- Added `schemas/annotations-manifest.schema.json`.
- Shared report path sanitization now delegates to `internal/location`.

## Rule catalogue changes
- None.

## Commands implemented
- `foundry-doctor annotate --format review --out review\sample.review`
- `foundry-doctor annotate --format review --out review\sample.review --diff`
- `foundry-doctor annotate --format github`
- `foundry-doctor annotate --format sarif --out annotate.sarif`

## Tests executed and results
- `gofmt -l .` → pass (empty)
- `go vet ./...` → pass
- `go test -count=1 ./...` → pass
- `staticcheck ./...` → pass
- `govulncheck ./...` → pass (`No vulnerabilities found.`)
- `bash scripts/claude/check-no-secrets.sh` → pass
- `bash scripts/claude/check-rule-catalog.sh` → pass (`catalogue valid: 108 rules`)

## Security and privacy review
- Review-copy writes are root-confined and reject unsafe output overlap, unsafe lexical paths, symlinked output ancestors, symlinked `azure.yaml`, and symlinked `infra` content.
- Workflow-command output escapes `%`, CR/LF, `:`, and `,` in command properties and neutralizes `%`, newlines, and `::` in messages.
- Absolute or traversal source locations are downgraded to manifest-only and no longer reprojected into repo-relative filenames.
- Non-review `--out` targets are confined to the project directory and cannot overwrite deployable inputs.

## Foundry lead review
- Final read-only review confirmed review-copy annotations, manifest/diff generation, path confinement, and exact-diagnostic-only Bicep inline behavior are aligned for supported single-entry Bicep projects.
- Remaining limitation: review-mode Bicep validation returns unavailable for `infra.layers` projects until layered validation support exists.

## Azure black belt review
- Final review found the azd `infra.module` bare-name handling (`main` → `main.bicep`) correct for current samples.
- Remaining limitation: `infra.layers` review validation is intentionally unavailable rather than silently skipped.

## Known limitations
- Review-mode Bicep validation currently supports single-entry module projects only; `infra.layers` projects return unavailable.
- Non-review formats still emit redacted rule/compiler text in GitHub and SARIF outputs; this phase did not introduce a separate allow-listed projection format.
- `go test -race` remains unsupported on windows/arm64.

## Deferred issues with rationale
- Full layered-Bicep review-copy validation is deferred because Phase 6 can safely fail unavailable instead of claiming validated output for configurations not yet modeled.
- Syntax-aware suppression for YAML block scalars and Bicep multiline strings is partially mitigated by semantic post-validation; richer syntax-aware placement remains future work.

## Compatibility matrix
- Validated on Windows ARM64 with Go `1.25.13` and `GOTOOLCHAIN=local`.
- Review mode works offline for YAML-only projects and for Bicep projects when the Bicep CLI is available.
- GitHub/SARIF formats work without Azure credentials and degrade gracefully when Bicep diagnostics are unavailable.

## Artefacts for next phase
- `docs/annotate.md`
- `schemas/annotations-manifest.schema.json`
- `samples/annotate/`
- `internal/annotate/`
- `internal/location/`
- `internal/diff/`

## Exact next-phase prerequisites
- If layered-Bicep projects must be annotated in review mode, add explicit `infra.layers` entry resolution and validation coverage before broadening support.
- If downstream consumers require stricter data-minimization than redacted evidence text, add a dedicated allow-listed annotation projection for GitHub/SARIF transport.

## Definition of Done audit
| DoD item | Status (met / unmet / requirement changed by ADR-NNN) | Current evidence |
|---|---|---|
| Review-copy mode writes `.review` artefacts without mutating deployable originals | met | `internal/annotate/review.go`, `internal/annotate/annotate_test.go` original-file and idempotence coverage |
| `annotations-manifest.json` and optional diff are generated deterministically | met | `internal/annotate/{manifest.go,review.go}`, manifest/diff tests, schema and sample artefacts |
| GitHub/workflow-command and SARIF outputs reuse source locations safely | met | `internal/annotate/github.go`, `internal/location/location.go`, `internal/app/annotate.go`, injection/path tests |
| Annotation text is redacted, bounded, and workflow-command safe | met | `internal/annotate/model.go`, truncation/injection tests |
| Findings without reliable locations remain manifest-only | met | `Collect` eligibility rules, external-path and non-diagnostic Bicep tests |
| Annotated YAML parses and annotated Bicep preserves compile behavior | met | `validateReviewCopy`, YAML parse + semantic comparison, Bicep compile/diagnostic/ARM-output checks |
| Command wiring, docs, samples, schema, and gates are complete | met | `cmd/foundry-doctor/annotate.go`, `docs/annotate.md`, `samples/annotate/`, `schemas/annotations-manifest.schema.json`, green gate run |
