# Phase 6 review: Azure black belt, round 2

Reviewer: `azure-black-belt` (independent, read-only, did not write the code). Date: 2026-10-18. Round 2.

Inputs read:
- `phase-6-azure-black-belt-round1.md`
- `phase-6-fixlog-loop1.md`
- `docs/decisions/ADR-024-phase-6-annotate-review-copy-decisions.md`
- the rewritten hand-off `docs/development/hand-offs/phase-6.md`
- the code: `internal/annotate/{output,review,model,github,manifest}.go`, `internal/annotate/{yaml,bicep}/annotate.go`,
  `internal/diff/diff.go`, `internal/location/location.go`, `internal/app/annotate.go`
- the callers of `resolveBicepEntry` in `internal/app/wire.go` and `internal/app/preflight.go`
- all the new and changed tests

## Environment constraints

- The sandbox has no Go toolchain and no registry access, so nothing was compiled or run. I checked that the code compiles by
  reading it: imports, signatures, callers and `os.Root` API use.
- The `os.Root` methods used are `OpenRoot`, `Stat`, `Lstat`, `OpenFile`, `Remove`, `RemoveAll`, `MkdirAll`, `ReadFile`, `WriteFile`
  and `FS`. All of them exist in the current `os` package (https://pkg.go.dev/os#Root). `MkdirAll`, `ReadFile`, `WriteFile` and
  `RemoveAll` are Go 1.25 additions. `go.mod` declares `go 1.26.9`.
- The hand-off says honestly that the gates (`go build/vet/test/-race`, `verify-phase.sh --strict`) and the `spike` real-Bicep test
  were **not run**. Per the brief I do not count that as a code defect. It is recorded below as condition C1.

## Compile check by reading

I found nothing that would stop the code from compiling:
- `model.go` imports `crypto/sha256` and `encoding/hex`, and both are used (`bicepFingerprint`).
- Package-level `max` in `github.go` legally shadows the builtin.
- The functions `pathsOverlap`, `validateOutputPlacement` and `ensureNoSymlinkComponents` now live only in `output.go`; there are
  no duplicates left in `review.go`.
- The removed app helpers (`rejectInputOverlap`, `removeReviewCopy`, `compileYAMLSource`) have no remaining callers.
  `normalizeOutPath` and `safeRepoOutputPath` are kept; `policy_test.go` still uses them.
- `WriteReview` uses a named result `err` and explicit `return x, err` statements. Shadowing it inside the loop is legal, and the
  deferred cleanup still sees the error. The cleanup defer runs before `root.Close()` because defers run last-in, first-out.
- The test helpers exist with matching signatures:
  - `writeFile` (`params_test.go:72`), `hostileLoad` (`loadguard_test.go:11`), `newSvc`, `fakeEngine`, `fakeProject`, `minimalYAML`;
  - `bicep.DiscoverOptions{Path, Runner}`, `bicep.RunResult{Stdout, Stderr, ExitCode}`, `bicep.ReasonFileLoadOutsideProject`;
  - `writeTestFile` (`annotate_test.go:293`).
- `armLoader{disc, runner}` with a nil `adapter` and a nil `stderr` is safe: `adapterStore.put` checks for a nil receiver, and
  `warnf` checks for a nil writer.
- I traced each new app test through the code path, for example `TestAnnotateReviewValidation`, the guard test, the alias tests
  and the SARIF region test. Each one should give the expected exit code and error text.

## Round 1 findings

| Id | Status | Evidence |
|---|---|---|
| B1 lexical-only overlap, symlinked `--out`, case aliases | **CLOSED** | `output.go`:<br>• `checkIdentityOverlap` uses `os.SameFile` over the stat chains of the output and of every input in both directions, so hard links and in-root aliases are caught.<br>• `ensureNoSymlinkComponents` refuses `ModeSymlink` and `ModeIrregular` on every existing component, including the final one.<br>• `WriteOutputFile` requires an existing target to be a regular file, removes it and recreates it with `O_CREATE\|O_EXCL`.<br>• `pathsOverlap` and `isWithin` compare case-folded on windows and darwin.<br>• Review mode runs the same checks (`review.go:65-78`).<br>Tests: `TestWriteOutputFile` (symlink, hard link, directory, lexical), `TestWriteReviewRefusesSymlinks`, `TestWriteReviewRefusesCaseAliasOfInfra`, `TestAnnotateFileOutputRefusesInputAliases`, and `cmd` `TestAnnotate` (`--out azure.yaml` gives exit 2 and the original is unchanged). |
| B2 `RemoveAll` on any `*.review` | **CLOSED** | `output.go` `resetReviewDir` and `ownedReviewDir` only delete a directory that is empty, has the `.foundry-doctor-review` marker, or has a manifest with `"tool":"foundry-doctor"`. `RemoveReview` applies the same ownership gate. The cleanup defer in `WriteReview` is registered only after the reset succeeds. Tests: `TestWriteReviewKeepsForeignDirectory`, `TestAnnotateReviewKeepsForeignDirectory`. |
| B3 absolute `--out` skips validation | **CLOSED** | `annotateOutPath` (`app/annotate.go:146-163`) works out the project-relative path once, with an `EvalSymlinks` fallback. That same `out` is then used for `WriteReview`, `validateReviewCopy`, `RemoveReview` and `WriteOutputFile`. An absolute path outside the project is refused for every format. Test: `TestAnnotateReviewAbsoluteOutInsideProject`, which also proves the copy was recompiled. |
| B4 SARIF region for untrusted locations | **CLOSED** | `toFindings` (`app/annotate.go:611-634`) emits line and column only when `e.Inline`. Test: `TestAnnotateSARIFOmitsRegionForManifestOnlyEntries`, which also checks that the text stays plain (`50%`). |
| B5 copies widen permissions | **CLOSED** | `copyFile` uses `src&0o666 \| 0o600`: group and other bits are never added, and only owner read/write is added. Copied directories use the source mode plus owner `0o700`. The review root is `0o750`, the manifest `0o640`, the diff `0o600` and the marker `0o640`. Test: `TestWriteReviewCopiesAreNeverMorePermissive`. |
| B6 claims not backed by tests; gate record contradicted | **CLOSED** (with condition C1) | The tests now exist:<br>• symlink tests: annotate package and app;<br>• permission test;<br>• `validateReviewCopy` with a fake runner covering identical ARM, generator-metadata-only change, ARM drift, compile status and diagnostic drift, each also asserting cleanup;<br>• YAML block-scalar drift test;<br>• real-CLI test behind the `spike` build tag, with a `'''` string and a module.<br>The large-repository test is deferred and disclosed in ADR-024 item 5. The old gate record is withdrawn, and the Go 1.25.13/1.26.9 contradiction is acknowledged. `cmd` `TestAnnotate` was rewritten to not assert stderr, which depends on the host, and to check the copy, the banner, the rename, a rerun and the `--out azure.yaml` refusal. The templateHash question is no longer relied on: `sameARM` removes `metadata._generator` at every level and compares canonical JSON (ADR-024 item 4, marked unverified). |
| N1 double escaping, sample mismatch | CLOSED | `summarise` is plain text only. `escapeCommandData` and `escapeCommandProperty` are applied once, in `GitHubWorkflowCommands`, and mirror the toolkit `escapeData`/`escapeProperty`. The sample is fixed. Test: `TestGitHubEscapesOnceAtTheSink`. |
| N2 Unicode format characters | CLOSED | `isFormatRune` drops `Cf`. `isBreakRune` turns `Cc`, `Zl` and `Zp` into a space. The same is done in `singleLine` and `location.sanitizePath`. Test: `TestFormatCharactersAreStripped`. |
| N3 accidental deployment | CLOSED | Every copied `azure.yaml` and `azure.yml` is renamed to `azure.review.y(a)ml` and gets the banner, and every `.bicep` copy gets the banner. A marker file is written. `.gitignore` guidance is in the docs. |
| N4 YAML checks incomplete; BOM | CLOSED | Every YAML file with `CommentStyle` `yaml` is decoded as a multi-document stream on both sides. An original that does not parse now fails the run instead of skipping the check. The BOM stays first. |
| N5 per-annotation fallback | OPEN, accepted | Deferred in ADR-024 item 6. Failing the run is safe. The `#disable-next-line` placement is fixed. |
| N6 redaction gaps | OPEN, accepted | The redaction package predates this phase and is outside the package scope. |
| N7 GitHub limits and path base | CLOSED (documented) | `docs/annotate.md` "Limitations". The per-step limits are in the toolkit docs: https://github.com/actions/toolkit/blob/main/docs/problem-matchers.md. |
| N8 unstable SARIF fingerprints | CLOSED | `bicepFingerprint` hashes code, file and message, with no line or column. |
| N9 diff header and newline | CLOSED | `hunkRange` gives `0,0` for an empty side. A no-newline marker is written. The full-file nature of the diff is documented. |
| N10 Windows junctions | PARTIALLY | `ModeIrregular` is refused in code (https://go.dev/doc/go1.23#os). The Windows test is deferred (ADR-024 item 5). |
| N11 copy size cap | OPEN, accepted | Deferred, ADR-024 item 5. |
| N12 intermediate `bicepconfig.json` | OPEN, accepted | Documented. Bicep uses the nearest `bicepconfig.json` (https://learn.microsoft.com/azure/azure-resource-manager/bicep/bicep-config). The equivalence check catches any difference and fails the run safely. |
| N13 absolute `--out` consistency | CLOSED | One rule for all formats. |
| N14 schema | PARTIALLY | `files` is now required and `tool` is a const. The other hardening is not done. |
| N15 link to rule docs | OPEN, accepted | Not cheap in this loop. |

## Effect of the resolver change on `doctor` and preflight

`resolveBicepEntry` now delegates to `resolveInfra`. Its callers are `wire.go:190` (`armLoader.Load`) and `preflight.go:228`
(`DeploymentParameters`). Both keep their previous fallback, `path.Join(defaultInfraPath(src.InfraPath), "main.bicep")`, when the
resolver returns false. Differences from before:

1. **Standard layout** (`azure.yaml` at the root, no `infra.path`, or `infra.path` with no `inputs.infraPath` override): the entry is the same.
   `TestResolveBicepEntry` (`app_test.go`) still holds: `deploy` plus `main` gives `deploy/main.bicep`.
2. **A non-default `inputs.infraPath` now beats `azure.yaml` `infra.path`.** Before, `azure.yaml` won. This is a behaviour change,
   but it brings the ARM loader in line with `internal/project`, which already scanned `inputs.infraPath`. It is disclosed in
   ADR-024 item 3. Not a regression.
3. **Provider other than bicep** (`terraform`, `microsoft.foundry`): the resolver returns false, so the loader falls back to
   `infra/main.bicep`. That is the same as before for projects without `infra.path`. The `microsoft.foundry` samples
   (`assess-dev`, `assess-test`, `azure-yaml-only`) have no `infra/` directory, so `doctor` output does not change. ADR-004 says
   these projects synthesise their infrastructure.
4. **`azure.yaml` in a subdirectory** (configured `inputs.azureYaml`): the ARM loader now looks under `app/infra/`, as azd does,
   while `internal/project` still scans `infra/` from the root. See N-R2-1.

I found no regression that blocks phase 6. No existing wire or preflight test depends on the changed cases.

## New findings (round 2)

No new blocking findings.

- **N-R2-1. ARM loader and project walker disagree when `azure.yaml` is in a subdirectory.** For `inputs.azureYaml: app/azure.yaml`,
  the ARM loader (`resolveInfra`) uses `app/infra`, while `project.Discover` walks the root-relative `infra`. In that layout `doctor`
  can now report a missing ARM model where it used to compile `infra/main.bicep`. The new behaviour matches azd, which resolves
  `infra.path` relative to the project directory (https://learn.microsoft.com/azure/developer/azure-developer-cli/azd-schema).
  Fix: have `project.Discover` use the same base, or warn when they differ. The owner of `wire.go` and `project` should handle it.
- **N-R2-2. Silent fallback when `infra.path` or `infra.module` is unsafe.** When `resolveInfra` returns false, for example for an
  escaping `infra.module`, `armLoader.Load` and `DeploymentParameters` silently compile `infra/main.bicep`. azd would not use that
  file. Annotate already reports the case as unavailable; `doctor` should at least warn or skip in the same way.
- **N-R2-3. `canonicalARM` can mask some numeric differences.** It decodes numbers as `float64`, so integers above 2^53 that differ
  could compare equal. Use `json.Decoder.UseNumber()`. Low risk.
- **N-R2-4. Windows cloud-file placeholders may be refused** (unverified). Refusing `ModeIrregular` sources in `copyFile` and
  `copyTree` may reject OneDrive cloud-file reparse points on Windows, which would make review mode unavailable for synced projects.
  I could not verify how Go reports cloud-file reparse tags. If it is confirmed, document the limitation or allow known cloud tags.
- **N-R2-5. No equivalence check for `.bicep` files outside the entry's module graph.** Every `.bicep` copy now gets the banner,
  but only the entry's graph is recompiled. Bicep files that the entry does not reference get a banner comment that is never
  checked. A single `//` line is safe, but the manifest should not imply they were validated.

## Conditions (not blocking for this gate, must be met before release)

- **C1.** The maintainer runs `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./...` (where supported),
  `staticcheck`, `govulncheck`, `verify-phase.sh --strict`, and `go test -tags spike -run TestRealBicepReviewCopyRecompiles ./internal/app/`
  with `BICEP_PATH` set. The output goes in the hand-off. Any failure reopens the matching item above.

## Verdict

All six round-1 blocking findings are closed. There are no new blocking findings. Non-blocking: 5 new (N-R2-1 to N-R2-5), plus the
round-1 items still open or partial and accepted under ADR-024 (N5, N6, N10, N11, N12, N14, N15).

VERDICT: APPROVE
