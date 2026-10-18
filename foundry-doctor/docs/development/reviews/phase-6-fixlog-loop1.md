# Phase 6 fix log, loop 1

Date: 2026-10-18. Fixer: go-implementer (fix loop 1). Reviews addressed: `phase-6-foundry-lead-round1.md` (FL) and
`phase-6-azure-black-belt-round1.md` (ABB). No Go toolchain was available: nothing was compiled or run. The maintainer must run
`go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./...` and `scripts/claude/verify-phase.sh --strict`.

## Blocking findings

| Finding | Status | Files changed |
|---|---|---|
| FL B1 infra tree and Bicep entry resolved two ways; `infra.path` not joined to the `azure.yaml` directory; `infra.provider` ignored | FIXED (precedence recorded in ADR-024) | `internal/app/annotate.go` (`resolveInfra`, `infraTarget`, `resolveBicepEntry`, `compileDiagnostics`, `collectAnnotationRun`), `internal/app/annotate_test.go` |
| FL B2 annotate compiles without the ADR-018 file-load guard | FIXED | `internal/app/annotate.go` (`doCompile` guards original and copy), `internal/app/annotate_test.go` (`TestAnnotateFileLoadGuardBlocksBothCompiles`) |
| FL B3 review copy is azd-discoverable; banner only on annotated files | FIXED (ADR-024) | `internal/annotate/review.go` (`reviewName`, banner on every `azure.yaml` and `.bicep` copy, marker), `internal/annotate/yaml/annotate.go`, `internal/annotate/bicep/annotate.go`, tests in `internal/annotate`, `internal/app`, `cmd/foundry-doctor/annotate_test.go`, `docs/annotate.md` |
| FL B4 hand-off evidence not backed | FIXED (tests added; hand-off rewritten; gate runs withdrawn and marked not run) | `docs/development/hand-offs/phase-6.md`, tests listed there |
| ABB B1 lexical-only overlap; symlinked `--out`; case-insensitive file systems | FIXED | `internal/annotate/output.go` (`checkIdentityOverlap`, `ensureNoSymlinkComponents` with `ModeIrregular`, `WriteOutputFile` with `O_EXCL`, case-folded lexical check), `internal/annotate/review.go`, `internal/app/annotate.go` (`emitAnnotateOutput`), tests |
| ABB B2 `RemoveAll` on any `*.review` directory | FIXED | `internal/annotate/output.go` (`resetReviewDir`, `ownedReviewDir`, `RemoveReview`, marker), `internal/app/annotate.go`, tests |
| ABB B3 absolute `--out` skips validation | FIXED | `internal/app/annotate.go` (`annotateOutPath` used for every format, write, validation and cleanup), `TestAnnotateReviewAbsoluteOutInsideProject` |
| ABB B4 SARIF gives exact lines for untrusted locations | FIXED | `internal/app/annotate.go` (`toFindings`), `TestAnnotateSARIFOmitsRegionForManifestOnlyEntries` |
| ABB B5 copies widen permissions | FIXED | `internal/annotate/review.go` (`copyFile`, directory modes), `internal/annotate/output.go` (perm constants), `TestWriteReviewCopiesAreNeverMorePermissive` |
| ABB B6 claims not backed by tests; gate record contradicted | FIXED for 1, 2, 3, 5, 6; ADR-024 for 4 (large-repository performance test deferred) | tests listed in the hand-off; `internal/app/annotate_bicep_real_test.go` (tag `spike`, not run); ARM comparison ignores `metadata._generator` (ADR-024); hand-off gate record withdrawn |

## Non-blocking findings

| Finding | Status | Files changed |
|---|---|---|
| FL N1 / ABB N1 escaping applied to every sink, double escaping | FIXED | `internal/annotate/model.go`, `internal/annotate/github.go`, tests |
| FL N2 Bicep diagnostic duplicated as adapter finding | FIXED | `internal/annotate/model.go` (`Collect`), `TestCollectDropsDuplicateBicepAdapterFinding` |
| FL N3 absolute `--out` | FIXED (same as ABB B3) | as ABB B3 |
| FL N4 / ABB N7 GitHub path base and annotation limits | NOT-FIXED (documented in `docs/annotate.md`; ADR-024 item 6) | `docs/annotate.md` |
| FL N5 out-of-root Bicep diagnostic reduced to base name | NOT-FIXED (owned by `internal/bicep`; ADR-024 item 6; hand-off Known limitations) | — |
| FL N6 / ABB N5 comment between `#disable-next-line` and its line; per-annotation fallback | PARTLY FIXED: Bicep comments go above `#disable-next-line`; per-annotation fallback NOT-FIXED (ADR-024 item 6) | `internal/annotate/bicep/annotate.go`, test |
| FL N7 / ABB N9 diff full-file, no-newline marker, `-1,0` | FIXED (documented as full-file replacement; `-0,0`; no-newline marker) | `internal/diff/diff.go`, `internal/diff/diff_test.go`, `docs/annotate.md` |
| FL N8 YAML-only fallback for layers / missing CLI | NOT-FIXED (ADR-024 item 6; documented) | — |
| FL N9 double compile; performance tests | NOT-FIXED (ADR-024 item 5) | — |
| FL N10 sample mismatch | FIXED | `samples/annotate/github-workflow.example.txt`, `samples/annotate/README.md`, `samples/annotate/review-manifest.example.json` |
| FL N11 duplicate extension `annotateCmd` | NOT-FIXED (owned by `extension/`; ADR-024 item 6) | — |
| FL N12 docs name `infra/`, azd discoverability | FIXED | `docs/annotate.md` |
| FL N13 / ABB N14 `files` not required | FIXED (`files` required, `tool` const); other schema hardening NOT-FIXED | `schemas/annotations-manifest.schema.json` |
| ABB N2 Unicode format characters | FIXED | `internal/annotate/model.go`, `internal/annotate/github.go`, `internal/location/location.go`, tests |
| ABB N3 accidental deployment | FIXED (same as FL B3; `.gitignore` advice documented) | as FL B3 |
| ABB N4 YAML checks incomplete; BOM | FIXED (every bannered/annotated YAML file decoded as a multi-document stream; an unparsable original fails; BOM kept first) | `internal/app/annotate.go` (`validateReviewCopy`, `yamlDocuments`), `internal/annotate/yaml/annotate.go`, `internal/annotate/bicep/annotate.go` |
| ABB N6 redaction gaps | NOT-FIXED (redaction package predates this phase; outside the owned packages) | — |
| ABB N8 unstable SARIF fingerprints for Bicep diagnostics | FIXED (hash of code, file, message) | `internal/annotate/model.go`, `internal/app/annotate.go` (`toFindings`) |
| ABB N10 Windows junctions | FIXED in code (`ModeIrregular` refused); test NOT-FIXED (needs Windows runner, ADR-024 item 5) | `internal/annotate/output.go`, `internal/annotate/review.go` |
| ABB N11 no size/file-count cap | NOT-FIXED (ADR-024 item 5) | — |
| ABB N12 intermediate `bicepconfig.json` | NOT-FIXED (ADR-024 item 6; documented) | `docs/annotate.md` |
| ABB N13 inconsistent absolute `--out` | FIXED (one rule for all formats) | `internal/app/annotate.go` |
| ABB N15 remediation link to `explain` | NOT-FIXED (not cheap; no rule-doc link source in `Entry`) | — |

## Shared files touched

- `docs/decisions/README.md`: ADR-024 row and highest-number note (targeted edit).
- `internal/app/app_test.go`: one expected path changed to `azure.review.yaml` (`TestAnnotateReviewWritesCopy`).
- `cmd/foundry-doctor/main_test.go`: `TestAnnotate` moved to `cmd/foundry-doctor/annotate_test.go`.
- `internal/app/wire.go` and `internal/app/preflight.go` are not edited, but their `resolveBicepEntry` calls now get the new resolver
  semantics (ADR-024 item 3).
