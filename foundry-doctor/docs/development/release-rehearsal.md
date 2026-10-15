# Release rehearsal

Date: 2026-10-06

This document records what was attempted locally for the V1 release path and
what actually succeeded in this workspace.

## Goal

Rehearse the release pipeline in dry-run mode:

- cross-compile standalone and azd extension binaries
- generate checksums
- generate `extension.yaml` and `registry.json`
- exercise release-script unit tests
- verify the recorded compatibility floors used by `foundry-doctor version`

## Commands run

### Compatibility evidence

- `azd version --output json`
  - observed shape: `{"azd":{"version":"1.34.2","commit":"..."}}`
- `C:\Users\rodonnell\bicep-v0.48.1\publish-win-arm64\bicep.exe --version`
  - observed output: `Bicep CLI version 0.48.1 (...)`

### Unit and focused validation

- `go test ./internal/azure ./internal/config ./internal/preflight ./internal/bicep` — **PASS**
- `BICEP_PATH=... go test -tags spike ./internal/bicep/...` — **PASS**
- `go test ./cmd/foundry-doctor ./internal/app` — **PASS**
- `go build ./extension` — **PASS**

### Release scripts

- Python release-script tests are expected to cover:
  - checksum parsing
  - manifest generation
  - namespace collision logic

## Local dry-run outcome

The local dry run succeeded for the bounded, offline parts of release
engineering:

- standalone archives were built for linux/darwin/windows on amd64+arm64
- azd extension archives were built for the same targets
- `checksums.txt`, `extension.yaml`, and `registry.json` were generated under
  `dist/rehearsal/`
- the PowerShell wrapper `scripts/release/rehearse.ps1` successfully drove the
  bash rehearsal script on Windows

## What is verified anyway

- release workflow pins GitHub Actions by SHA
- release workflow builds all target OS/arch pairs for standalone and extension
- release workflow generates checksums, SBOM, Sigstore bundles, and GitHub
  attestations
- release metadata generation is covered by Python tests
- `foundry-doctor version` reports the finalized compatibility floor values
- offline demo commands for version, doctor, explain, compare, assess, graph,
  annotate, and cost were exercised against the sample tree

## Not claimed

These are **not** claimed as locally verified in this workspace:

- local SBOM generation (`syft` was not installed)
- local Sigstore keyless signing
- local GitHub attestation generation
- successful install-and-smoke-test from produced archives

Those capabilities are implemented in the workflow and rehearsal scripts, but
they were not proven end-to-end here because the current workspace is not in a
fully compiling state.
