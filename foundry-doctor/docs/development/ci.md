# CI and release engineering

Owner: DevOps/release packet (CI-REL). Written 2026-10-05. PRD section 18, ADR-003, ADR-005, ADR-011.

**Status: no workflow in this document has run on GitHub.** They were checked with `actionlint` v1.7.7 (syntax, expressions, action inputs from
the pinned `action.yml` files) and the scripts were run locally. Only a real GitHub run proves a workflow. Treat every "should" below as unproven.

GitHub reads workflows only from the repository root, so the files are in `.github/workflows/` with the prefix `foundry-doctor-`, path filters on
`foundry-doctor/**` and `working-directory: foundry-doctor`. The PRD names (`ci.yml`, `doctor-offline.yml`, ...) are mapped below.

| PRD name | File | Trigger | State |
|---|---|---|---|
| ci.yml | `foundry-doctor-ci.yml` | pull_request, push to main (paths) | written, not run |
| doctor-offline.yml | `foundry-doctor-offline.yml` | workflow_call | written, not run |
| release.yml | `foundry-doctor-release.yml` | tag `foundry-doctor/v*` | written, **never run** |
| dependency-review.yml | `foundry-doctor-dependency-review.yml` | pull_request (go.mod, go.sum, workflows) | written, not run |
| doctor-preflight.yml, runtime-diagnostics.yml | not written | | Phase 1 rejects `--preflight` and `--runtime` (ADR-010 decision 3); they need OIDC, a protected environment and an Azure subscription, so they belong to the phase that enables the flags |

## CI (`foundry-doctor-ci.yml`)

Read-only token, no secrets, no `pull_request_target`: safe for fork pull requests. Jobs:

- `test`: `go test ./...` on ubuntu, macos and windows, `go-version-file: foundry-doctor/go.mod`.
- `quality`: `scripts/install-dev-tools.sh`, then `scripts/claude/verify-phase.sh` (gofmt, vet, test, race, staticcheck, govulncheck, build, rule-catalogue check,
  secret scan, import boundary, `bash -n scripts/ci/*.sh`). Not yet `--strict`: switch it on when `cmd/foundry-doctor` exists, otherwise the skipped build gate would fail CI.
- `import-boundary`: `scripts/ci/check-import-boundary.sh`.
- `azd-namespace`: `scripts/ci/check-azd-namespace.sh`; exit 5 becomes a warning annotation and a "SKIPPED (not a pass)" summary line.
- `build-matrix`: `scripts/ci/build-matrix.sh`; uploads the binaries. Warns "SKIPPED" while `cmd/foundry-doctor` is absent.
- `smoke`: builds the binary on each OS and runs `scripts/ci/smoke-samples.sh`. Warns "SKIPPED" while `cmd/foundry-doctor` or `samples/<project>` is absent.

A skip is always visible (annotation plus step summary). Nothing passes silently, but a skipped job still shows a green check; read the annotations.

## Scripts (`scripts/ci/`)

Exit codes follow `verify-phase.sh`: 0 pass, 1 failure, 5 SKIPPED (the check could not run; never a pass). `verify-phase.sh` reports 5 as SKIPPED.

| Script | Purpose | Run here |
|---|---|---|
| `check-import-boundary.sh` | ADR-005: `go.mod` has no `github.com/azure/azure-dev`; `GOWORK=off go build ./...`; `go list -deps -test ./...` has no azure-dev package | PASS |
| `check-azd-namespace.sh` | ADR-003: `registry.json` and `registry.dev.json` of Azure/azure-dev at the verified commit `afe4b2b4d262` and at `main`, plus the azd command reference (MicrosoftDocs/azure-dev-docs `main`), have no `foundry` / `foundry.*` namespace and no `azd foundry` command; our own `extension.yaml` (when it exists) must not use id `microsoft.foundry` | PASS (live), exit 5 offline, exit 1 against a fake registry |
| `build-matrix.sh` | `CGO_ENABLED=0 -trimpath -ldflags "-s -w -X ..."` for linux, darwin, windows x amd64, arm64; `SHA256SUMS` | PASS with `BUILD_PKG=./cmd/rulecatalog`; exit 5 for the default package (absent) |
| `smoke-samples.sh` | runs `doctor --format sarif --out` inside each `samples/<project>`, accepts exit 0 or 1 (or the code in `smoke.expected-exit`), validates SARIF 2.1.0 | exit 5 (no binary, no samples) |

The registry check treats `microsoft.foundry` in the registry as expected (ADR-003 finding 3: a pack without commands). It does not use the pinned commit as the only
source because ADR-003 requires re-checking at each release; the release workflow runs it with no tolerance for exit 5.

Contracts other packets must honour:

- **Version variable.** `build-matrix.sh` sets `-X github.com/ruairispain/copilot-web/foundry-doctor/internal/buildinfo.Version=<tag>` (override with `VERSION_VAR`).
  Go silently ignores `-X` for a variable that does not exist, so the CLI packet must define `internal/buildinfo.Version` (or tell this packet the real name).
- **Project path.** The scripts and the reusable workflow run `foundry-doctor doctor` with the project as the current directory. Update them if the CLI takes a path argument.
- **SARIF paths.** The reusable workflow uploads the doctor SARIF as is. URIs must be relative to the repository root for code scanning; if `path` is not `.`, the CLI must still emit repository-root-relative URIs
  (or the workflow needs a flag). Unverified.

## Reusable workflow (`foundry-doctor-offline.yml`)

Inputs: `path`, `profile`, `fail-on`, `install-mode` (`release` or `source`), `version`, `source-repository`, `source-ref`, `verify-attestation`, `runner`, `upload-sarif`, `category`, `artifact-name`.
Inputs reach the shell only through `env` and are validated first. Permissions: workflow `contents: read`; only the `upload` job has `security-events: write` (plus `contents: read`
and `actions: read`, which `upload-sarif` needs in private repositories). `github/codeql-action/upload-sarif` is pinned to v4.38.2 (`2892aa5e19bbd11bc0cff5427e3b750a04d9e3c2`, the commit behind the annotated tag).

- Release mode downloads `foundry-doctor_<version>_<os>_<arch>` and `SHA256SUMS` from the release `foundry-doctor/<version>`, checks the checksum, and runs `gh attestation verify`. No release exists yet, so use `source` until the first release.
- Source mode checks out `source-repository` at `source-ref` and builds `./cmd/foundry-doctor`. Pin a tag or SHA.
- **Fork behaviour.** On a `pull_request` from a fork, and on Dependabot runs, the token is read-only. The upload job is skipped with a notice; the SARIF stays available as a workflow artifact;
  the doctor job still fails on exit 1, 2, 3 or 4, so the exit code is enforced. This follows GitHub's documented read-only token for fork PRs; the exact upload failure was **not tested** (see `phase-1-tooling-facts.md`).
- The caller must grant `contents: read`, `actions: read`, `security-events: write`; otherwise GitHub rejects the run at start-up. Private repositories need GitHub Code Security for SARIF upload.
- The workflow has no Azure login and no secrets. Do not call it from `pull_request_target`.

`foundry-doctor/samples/github/foundry-doctor-starter.yml` and its README are a copy-paste sample. A real starter-workflow listing needs `workflow-templates/` plus a `.properties.json` in an organisation `.github` repository.

## Release (`foundry-doctor-release.yml`) - authored, never run

Tag `foundry-doctor/vX.Y.Z[-pre]` on a commit reachable from `main`. Jobs: `verify` (tag format, `verify-phase.sh --strict`, azd namespace check with no tolerance for SKIPPED) -> `build` (matrix, `SHA256SUMS`) ->
`sbom` (`anchore/sbom-action`, CycloneDX JSON, generated from the built binaries so the Go build info lists the exact modules) -> `sign` (cosign keyless `sign-blob` bundles for `SHA256SUMS`, the SBOM and each binary; `actions/attest-build-provenance`
for every file in `SHA256SUMS`) -> `publish` (`gh release create`, environment `release`). `id-token: write` and `attestations: write` exist only on `sign`; `contents: write` only on `publish`.

Verification commands for a consumer (unrun; replace `vX.Y.Z` and the repository):

```
gh release download foundry-doctor/vX.Y.Z --repo RuairiSpain/copilot-Web
sha256sum -c SHA256SUMS --ignore-missing
gh attestation verify foundry-doctor_vX.Y.Z_linux_amd64 --repo RuairiSpain/copilot-Web
cosign verify-blob --bundle foundry-doctor_vX.Y.Z_linux_amd64.sigstore.json \
  --certificate-identity "https://github.com/RuairiSpain/copilot-Web/.github/workflows/foundry-doctor-release.yml@refs/tags/foundry-doctor/vX.Y.Z" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com foundry-doctor_vX.Y.Z_linux_amd64
```

Not in the release workflow, and why:

- **azd extension archives and registry manifest.** The extension module (`foundry-doctor/extension/`, ADR-005) does not exist and the publisher-owned extension id (ADR-003: not `microsoft.foundry`) is undecided.
  The registry entry needs per-platform artifact URLs and checksums that depend on the extension packaging. Add a job after those exist.
- **Third-party notices** (licence-inventory section on redistribution) are not generated.
- `actions/attest-build-provenance` v4 is documented as a thin wrapper over `actions/attest`; new work should move to `actions/attest`. It was kept because the PRD packet named it.
- `cosign-release` is pinned to v3.0.6 (the installer default). The syft version used by `anchore/sbom-action` is the action's own default.

## Dependency review (`foundry-doctor-dependency-review.yml`)

`actions/dependency-review-action` v5.0.0, `fail-on-severity: high`, `allow-licenses: MIT, Apache-2.0, BSD-3-Clause, Unlicense`. **This list must match `docs/licence-inventory.md`**
(only permissive licences; the inventory records MIT, Apache-2.0, BSD-3-Clause and the Unlicense for `go-sarif`). Extend both together. The action reviews all dependency changes in the pull request, not only `foundry-doctor/`;
the path filter limits only when it starts. It needs the dependency graph.

## Pinned actions (resolved with `git ls-remote`, 2026-10-05; each SHA is a commit, annotated tags peeled)

| Action | Tag | Commit |
|---|---|---|
| actions/checkout | v7.0.1 | `3d3c42e5aac5ba805825da76410c181273ba90b1` |
| actions/setup-go | v7.0.0 | `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` |
| actions/upload-artifact | v7.0.1 | `043fb46d1a93c77aae656e7c1c64a875d1fc6a0a` |
| actions/download-artifact | v8.0.1 | `3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c` |
| github/codeql-action/upload-sarif | v4.38.2 | `2892aa5e19bbd11bc0cff5427e3b750a04d9e3c2` |
| actions/dependency-review-action | v5.0.0 | `a1d282b36b6f3519aa1f3fc636f609c47dddb294` |
| actions/attest-build-provenance | v4.2.2 | `4d101475d8b20a2381f78447822ac1eab6504dd8` |
| sigstore/cosign-installer | v4.1.2 | `6f9f17788090df1f26f669e9d70d6ae9567deba6` |
| anchore/sbom-action | v0.24.3 | `66cbf4bc1f1c0d2edc94016e65bc221b6bb0ad6c` |

Inputs used were read from each action's `action.yml` at that commit. `azure/login` (v2.3.1 `7184910d9eb2b1c5e48f7073824a90609bb9b6d6`, node20) is resolved for the future preflight workflow but not used.
The existing repo workflows were not changed.
