# Phase 6 hand-off: annotate outputs

Status: **loop 1 of the post-merge gate (Go 1.26.9, 2026-10-19): both reviewers REJECT; blockers fixed in the same loop, pending
loop 2 re-review.** Blockers fixed: line breaks the YAML scanner counts (NEL, LS, PS, lone CR) now shift comments correctly;
original line endings are kept (CRLF no longer changes a multi-line string's compiled value); a relative `--dir` no longer
drops every Bicep diagnostic location; an unparseable `azure.yaml` is bannered with its findings in the manifest only
(`source-not-parseable`) instead of exit 4; credential-like and oversized files are not copied, the tree has its own
`.gitignore` (`*`), and the tree is capped (8 MiB per file, 256 MiB total).

## Delivered
- `foundry-doctor annotate` CLI with `review`, `github`, and `sarif` formats.
- Review-copy generation under `.review` with deterministic inline comments for `azure.yaml` and exact Bicep compiler diagnostics.
- `annotations-manifest.json` plus optional `annotations.diff`.
- Shared canonical location handling in `internal/location` and deterministic unified diffs in `internal/diff`.
- Validation that review-copy YAML still parses and that annotated Bicep preserves compile result, diagnostics, and compiled ARM output.
- Path-confinement, injection, truncation, idempotence, overlap, ownership, permission and symlink tests (added or extended in fix loop 1;
  not yet executed, see below).
- Fix loop 1: one infra resolver honouring `azure.yaml` `infra.path`/`infra.module`/`infra.provider`; ADR-018 guard before both annotate
  compiles; review copies renamed `azure.review.yaml` with an always-present banner; ownership marker; identity-based overlap checks.

## Changed contracts
- Added CLI entrypoint `foundry-doctor annotate`.
- Fix loop 1: the review copy of `azure.yaml` is `azure.review.yaml`; `annotations-manifest.json` `files[]` lists every rewritten copy
  (annotated or bannered) and `files` is now `required` in the schema; `Entry` text is plain (no `%25`/`∷`); Bicep diagnostic entries
  carry a line-independent `fingerprint`; `annotate.WriteOutputFile`, `annotate.RemoveReview` and `annotate.ErrUnsafeOutput` added;
  `resolveBicepEntry` (used by the ARM loader and preflight) now resolves `infra.path` against the `azure.yaml` directory, lets a
  non-default `inputs.infraPath` win, and returns false for a non-Bicep provider (ADR-024).
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
Run on Go 1.26.9: `go build`, `go vet`, `gofmt -l`, `go test ./...` and the catalogue and secret checks pass. The real-Bicep
tests (`go test -tags spike -run TestRealBicep ./internal/app` with `BICEP_PATH` set to Bicep 0.48.1) pass, including the two
added in this loop. CI runs `-race`, staticcheck and govulncheck. The earlier "not run" and "Go 1.25.13" statements are withdrawn.
Added in the gate (each verified to fail without its fix): YAML NEL/LS/PS/lone-CR line counting and CRLF preservation; Bicep
CRLF and lone-CR; `lines` package; relative-root `safePath`; real-Bicep relative `--dir` and CRLF multi-line string;
unparseable YAML exits non-4 with a manifest-only copy; credential-like and oversized files skipped; `.gitignore` written;
a 1500-file tree copy.

## Security and privacy review
- Output overlap is decided by file identity (`os.SameFile` over every existing ancestor) as well as by name (case-folded on Windows and
  macOS). Output paths through symbolic links or reparse points (`ModeIrregular`, Windows junctions) are refused. github/sarif `--out`
  refuses an existing link or non-regular target and creates the file with `O_EXCL`.
- An existing `.review` directory is deleted only when it is empty, carries the `.foundry-doctor-review` marker, or holds a manifest
  with `"tool": "foundry-doctor"`. A failed run removes only its own tree.
- Copies keep the source group/other bits (owner read/write added); the review root is `0750`, the manifest `0640`, the diff `0600`.
- The ADR-018 file-load guard runs before both annotate compiles; a refused entry never reaches the Bicep CLI.
- Annotation text strips format characters (bidirectional overrides, `Cf`) and line/paragraph separators; GitHub escaping is applied
  once, at the GitHub sink. SARIF carries no line or column for manifest-only entries.
- Windows junction refusal is implemented but not tested (needs a Windows runner, ADR-024).

## Foundry lead review
- Round 1 (`docs/development/reviews/phase-6-foundry-lead-round1.md`): REJECT. Fixes applied in fix loop 1
  (`docs/development/reviews/phase-6-fixlog-loop1.md`); pending re-review.

## Azure black belt review
- Round 1 (`docs/development/reviews/phase-6-azure-black-belt-round1.md`): REJECT. Fixes applied in fix loop 1
  (`docs/development/reviews/phase-6-fixlog-loop1.md`); pending re-review.

## Known limitations
- Review-mode Bicep validation supports a single entry module; `infra.layers` projects, and Bicep projects without a Bicep CLI, return
  unavailable (no YAML-only fallback yet).
- A comment that changes the meaning of the copy fails the whole run; there is no per-annotation fallback yet. Bicep comments are moved
  above `#disable-next-line` directives.
- GitHub `file=` and SARIF URIs are relative to `--dir`; no path prefix option; no cap on the number of GitHub annotations.
- A `bicepconfig.json` between the project root and a nested infra directory is not copied.
- The doctor ARM loader (shared `wire.go`) still compiles `infra/main.bicep` for a non-Bicep provider when that file exists (unchanged
  from before Phase 6); annotate itself never compiles a non-Bicep provider (ADR-024).
- A Bicep diagnostic in a file outside the project root is reduced to its base name by `internal/bicep` before annotate sees it (lead N5,
  owned by `internal/bicep`).
- `go test -race` remains unsupported on windows/arm64.

## Deferred issues with rationale
- Large-repository performance tests and an infra copy size cap: not delivered; carried to the Phase 9 audit (ADR-024).
- Windows junction test: needs a Windows runner (ADR-024).
- Layered-Bicep review validation: deferred; Phase 6 fails unavailable instead of claiming validated output.

## Compatibility matrix
- Built and tested on Go 1.26.9 in the post-merge gate; the code uses `os.Root` methods from Go 1.24/1.25.
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
DoD items are PRD section 14, verbatim. "Met in code" means the code and tests exist but have not been executed in fix loop 1.

| DoD item | Status (met / unmet / requirement changed by ADR-NNN) | Current evidence |
|---|---|---|
| Original files are byte-for-byte unchanged. | met (loop 1 fixes applied; pending loop 2 re-review) | All writes go through `os.Root` under the `.review` directory or a confined `--out`; overlap by identity and name; symlink/junction refusal; `bicep build --stdout`. Tests: `TestWriteReview`, `TestWriteOutputFile`, `TestAnnotateFileOutputRefusesInputAliases`, `TestAnnotateReviewValidation`, cmd `TestAnnotate`. |
| Annotated YAML parses and annotated Bicep compiles. | met (loop 1 fixes applied; pending loop 2 re-review) | One infra resolver (`resolveInfra`) for copy and compile; `validateReviewCopy` decodes every bannered/annotated YAML file and compares documents, recompiles the copy behind the ADR-018 guard and compares status, ARM (generator metadata aside, ADR-024) and diagnostics. Tests: `TestAnnotateReviewValidation`, `TestAnnotateReviewDetectsYAMLBlockScalarDrift`, `TestAnnotateReviewUsesAzureYAMLInfraPathAndModule`, `TestAnnotateReviewAzureYAMLInSubdirectory`; real-CLI `TestRealBicepReviewCopyRecompiles` (tag `spike`) not run. |
| Rerunning produces no duplicate comments. | met (loop 1 fixes applied; pending loop 2 re-review) | The owned tree is reset and recopied from the originals. Tests: `TestWriteReviewRerunIsIdempotent`, cmd `TestAnnotate` rerun. |
| Findings without reliable locations are placed only in report/manifest. | met (loop 1 fixes applied; pending loop 2 re-review) | `Collect` eligibility; `toFindings` drops line/column for non-inline entries. Tests: `TestCollectAndGitHubWorkflowCommands`, `TestAnnotateSARIFOmitsRegionForManifestOnlyEntries`. Out-of-root Bicep diagnostic paths: see Known limitations (lead N5). |
| Output names include .review to prevent accidental deployment. | met (loop 1 fixes applied; pending loop 2 re-review) | `--out` must end in `.review`; `azure.yaml` copies are renamed `azure.review.yaml`; every copied `azure.yaml` and `.bicep` carries the banner (ADR-024). Tests: `TestWriteReviewZeroFindingManifestUsesArrays`, `TestWriteReview`, cmd `TestAnnotate`. |

PRD testing-strategy categories: round-trip YAML, Bicep recompile (fake runner; real CLI gated), idempotence and permission/symlink tests
exist; large-repository performance tests are not delivered (ADR-024).

## Open non-blocking items (gate loop 1)
- Prior tool-owned comments in an input are not stripped; idempotence comes from rebuilding the tree from the originals (the PRD
  "remove prior doctor-owned comments" step is not implemented).
- `annotations.diff` is a whole-file replacement hunk, not a line diff.
- Whole-run fail-closed: a missing Bicep CLI for a Bicep project, `infra.layers`, or an `infra.path` outside the project gives
  exit 2 instead of a YAML-only copy; only the entry module is recompiled (unreferenced `.bicep` files are bannered but not).
- Manifest `line` values refer to the original file; the copy shifts by the banner and inserted lines.
- Comment text uses positional labels and truncates at 240 bytes; rule recommendations are generic text.
- The `.review` suffix is on the directory only; `infra/main.bicep` keeps its name inside it.
- Windows junction behaviour is untested; `extension/commands_extra.go` duplicates the annotate command definition.
