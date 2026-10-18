# Phase 6 review: Foundry lead, round 2

Reviewer: `foundry-lead` (independent; did not write the code). Date: 2026-10-18. Round 2.
Phase: 6, annotated review copies (PRD section 14).
Inputs reviewed:
- my round 1 review (`phase-6-foundry-lead-round1.md`);
- the fixer's log (`phase-6-fixlog-loop1.md`);
- ADR-024;
- the rewritten hand-off;
- `internal/annotate/**` (including the new `output.go`), `internal/location`, `internal/diff`;
- `internal/app/annotate.go` and the new `internal/app/annotate_test.go` / `annotate_bicep_real_test.go`;
- `cmd/foundry-doctor/annotate_test.go`, `docs/annotate.md`, `samples/annotate/`;
- the callers of the changed `resolveBicepEntry`: `internal/app/wire.go` (`armLoader.Load`) and `internal/app/preflight.go`
  (`armLoader.DeploymentParameters`).

Environment constraints: no Go toolchain and no package registry, so nothing was built or run. I checked compile-correctness by
reading the code: imports, signatures, callers, and the `os.Root` methods used (`OpenRoot`, `Lstat`, `Stat`, `OpenFile`, `ReadFile`,
`WriteFile`, `MkdirAll`, `Remove`, `RemoveAll`, `FS`). `go.mod` declares `go 1.26.9`, and every one of these methods exists from
Go 1.25 onwards. The fix loop did not run the gates either (the hand-off says so plainly). This approval therefore depends on the
maintainer's gate run listed at the end.

## Round 1 findings

| ID | Status | Evidence |
|---|---|---|
| **B1** infra copy and compile resolved two different ways; `infra.path` not relative to the `azure.yaml` directory; `infra.provider` ignored | **CLOSED** | See the B1 notes below. |
| **B2** annotate compiles without the ADR-018 file-load guard | **CLOSED** | See the B2 notes below. |
| **B3** review copy can be found and deployed by azd; banner only on annotated files | **CLOSED** | See the B3 notes below. |
| **B4** hand-off evidence not backed | **CLOSED** | See the B4 notes below. |
| N1 sink-specific escaping applied everywhere | CLOSED | `summarise` (`model.go`) no longer escapes. GitHub escaping is done only in `escapeCommandData` and `escapeCommandProperty` (`github.go:32-46`). The SARIF test checks `50%` with no `%25`. |
| N2 Bicep diagnostic duplicated | CLOSED | `Collect` drops a `bicep/<code>` adapter finding when a diagnostic matches on code, file and line (`model.go`, `bicepKey`). This matches `bicepRulePrefix = "bicep/"` and `BicepAdapterName = "bicep"` (`wire_bicep.go:18,22`). Covered by `TestCollectDropsDuplicateBicepAdapterFinding`. |
| N3 absolute `--out` | CLOSED | `annotateOutPath` (`annotate.go:146-163`) makes the path relative once. Write, validation and `RemoveReview` all use the same relative path. Covered by `TestAnnotateReviewAbsoluteOutInsideProject`. |
| N4 GitHub path base | OPEN, accepted | Deferred in ADR-024 item 6 and documented under "Limitations" in `docs/annotate.md`. |
| N5 outside-root diagnostic reduced to its base name | OPEN, accepted | ADR-024 item 6. The code is owned by `internal/bicep`. |
| N6 `#disable-next-line` placement; fallback per annotation | PARTIALLY | Comments now go above the directives (`bicep/annotate.go`, `isDisableNextLine`). The per-annotation fallback is deferred in ADR-024 item 6. |
| N7 diff | CLOSED | `0,0` ranges, the `\ No newline` marker, and documentation that the diff is a full-file replacement. |
| N8 YAML-only fallback for layered projects or a missing CLI | OPEN, accepted | ADR-024 item 6, documented. |
| N9 double compile; performance tests | OPEN, accepted | ADR-024 item 5 moves the performance tests to the Phase 9 audit. |
| N10 samples | CLOSED | The samples now match the escaping and separator the code writes. |
| N11 duplicate extension command | OPEN, accepted | ADR-024 item 6; owned by `extension/`. |
| N12 docs | CLOSED | `docs/annotate.md`: "Which infra directory is copied and compiled" and "The review copy". |
| N13 schema `files` | CLOSED | `files` is now in `required`. |

### B1: one infra resolver (CLOSED)

- `resolveInfra` (`annotate.go:328-377`) returns one `infraTarget{Dir, Entry, Provider}`.
- The same target is used by `WriteReview` (`InfraPath: run.infra.Dir`, line 117), by `compileDiagnostics`, by `validateReviewCopy`
  through `compilePair`, and by the ARM loader and preflight through `resolveBicepEntry`.
- `infra.path` and the default are joined to `path.Dir(AzureYAMLAt)`. This follows azd, which treats the directory that contains
  `azure.yaml` as the project directory
  (https://github.com/Azure/azure-dev/blob/main/cli/azd/pkg/environment/azdcontext/azdcontext.go).
- A non-Bicep provider is never compiled (`isBicep`; annotate.go lines 236 and 396). The azd docs list `bicep` and `terraform` as
  built-in providers and say extensions can register more
  (https://learn.microsoft.com/azure/developer/azure-developer-cli/azd-schema).
- New tests: `TestResolveInfra` (11 cases), `TestAnnotateReviewUsesAzureYAMLInfraPathAndModule`,
  `TestAnnotateReviewAzureYAMLInSubdirectory`, `TestAnnotateNonBicepProviderIsNotCompiled`.

### B2: file-load guard (CLOSED)

- `doCompile` (`annotate.go:424-436`) runs `bicep.GuardFileLoads(r, entryRel)` against the original root, and then again against the
  review root, before every `Compile`.
- A refused file returns an error that wraps `ErrUnavailable`.
- `TestAnnotateFileLoadGuardBlocksBothCompiles` covers all three formats and checks that no `build` call reached the runner.

### B3: the review copy is not an azd project (CLOSED)

- `reviewName` (`review.go:225-234`) writes `azure.yaml` as `azure.review.yaml`. This works because azd only looks for
  `ProjectFileName = "azure.yaml"` (azdcontext.go above).
- The banner is written to every copied `azure.yaml` (the renamed file goes through `yaml.Apply` even when it has no comments) and to
  every copied `.bicep` file (`review.go:141-152`). A byte order mark at the start of a file stays first.
- Tests: the app tests and cmd `TestAnnotate` both check that no `azure.yaml` exists in the copy and that the banner is the first line.

### B4: hand-off evidence (CLOSED)

- The symlink tests now exist: `TestWriteReviewRefusesSymlinks`, `TestAnnotateReviewRefusesSymlinkedInputs`,
  `TestAnnotateFileOutputRefusesInputAliases`.
- The validation branches are covered: `TestAnnotateReviewValidation` (identical, generator-only change, ARM drift, compile status,
  diagnostic drift) and `TestAnnotateReviewDetectsYAMLBlockScalarDrift`.
- The hand-off review sections now say "pending re-review".
- The gate results are withdrawn and marked not run.
- The `TestAnnotate` conflict is resolved: the test moved to `cmd/foundry-doctor/annotate_test.go`, it no longer asserts on stderr, and
  it does not expect GitHub output for a fixture that has no inline locations.

## Regression check: `resolveBicepEntry` in `doctor` and `preflight`

`wire.go:188-193` and `preflight.go:226-231` keep the same pattern: start from `infra/main.bicep`, and replace it only when
`resolveBicepEntry` returns ok. What this means for each kind of input:

- **Default projects** (no `infra` block, or `path: infra`, `module: main`, `azure.yaml` at the root): the entry is still
  `infra/main.bicep`. `path.Join(".", "infra")` is `infra`.
  - The samples `bicep-backed`, `cost-fixed`, `good-private`, `bad-private` and `assess-prod` all declare exactly
    `provider: bicep`, `path: infra`, `module: main`, so their behaviour does not change.
- **`provider: microsoft.foundry`** (samples `good`, `bad`, `assess-dev`, `assess-test`, `azure-yaml-only`, and
  `test/fixtures/project/synthetic-foundry`): `resolveBicepEntry` returns false, so the code falls back to `infra/main.bicep` exactly
  as before. None of these samples has an `infra/` directory, so the result is unchanged.
- **Changed behaviour, and correct for azd:**
  - When `azure.yaml` is in a subdirectory and `inputs.infraPath` is left at its default, the entry is now
    `<azure.yaml dir>/infra/main.bicep` instead of the root `infra/main.bicep`.
  - When `azure.yaml` sets a non-default `infra.path` or `infra.module`, that entry is now compiled.
  - Both changes are recorded in ADR-024 item 3.
- **Unsafe paths and non-Bicep providers** still fall back to the old default. ADR-024 item 3 records that `wire.go` was left
  unchanged.

I found no regression for any existing fixture or sample.

## New findings

No new blocking findings.

Non-blocking:

- **N14** `samples/good/review/demo.review/` is an old review-copy artefact from before the fix. It contains an `azure.yaml` that azd
  can discover, and it sits inside a sample project. This works against the B3 fix and could confuse anyone who runs azd from that
  folder. Delete it, and add `*.review/` to the repository `.gitignore`.
- **N15** The two code paths now disagree for non-Bicep providers:
  - Annotate skips the ARM load when the provider is not Bicep (`annotate.go:236`).
  - `doctor` still compiles `infra/main.bicep` for a `terraform` or `microsoft.foundry` project if that file exists.
  - As a result, annotate and doctor can report different ARM findings for such a project.
  - ADR-024 item 3 records this. It should be aligned when the owner of `wire.go` takes it up.
- **N16** `project.Discover` still scans `src.InfraPath` (the root `infra` by default) for parameter and environment warnings, while
  compiles now use the directory chosen by `resolveInfra`. When `infra.path` is set in `azure.yaml`, scanner warnings can refer to a
  different directory from the one that is compiled. This was a pre-existing inconsistency, and Phase 6 made it more visible. Use
  `resolveInfra` in the project loader or `scanInfra` at a later point.
- **N17** `compileDiagnostics` returns unavailable for an invalid `infra.path` or `infra.module` before it checks the provider. A
  `terraform` project with an unusual `infra.path` therefore cannot run `--format review`, even though it would never be compiled. Check
  `isBicep()` first.

## Conditions on this approval (environment, not code defects)

The maintainer must run these on this commit and attach the output to the hand-off:
- `go build ./...`
- `go vet ./...`
- `go test -count=1 ./internal/annotate/... ./internal/location ./internal/diff ./internal/app ./cmd/foundry-doctor`
- `go test -tags spike -run TestRealBicepReviewCopyRecompiles ./internal/app/`, with `BICEP_PATH` set to a CLI at version 0.48.1 or
  later.

If any of these fails, this approval is withdrawn.

## Verdict against the phase 6 DoD

| DoD item | Assessment |
|---|---|
| Originals byte-for-byte unchanged | Met in code. The tests exist but were not run. |
| Annotated YAML parses and annotated Bicep compiles | Met in code, including non-default `infra.path`/`infra.module` and `azure.yaml` in a subdirectory. The real-CLI spike was not run. |
| No duplicate comments on rerun | Met: the tool-owned tree is reset. |
| Imprecise findings stay in the manifest only | Met. N5 is accepted under ADR-024. |
| Output names include `.review` to prevent accidental deployment | Met: the `.review` directory, the `azure.review.yaml` rename, the banner on every copied project and Bicep file, and the marker file. |

VERDICT: APPROVE
