# Foundry Doctor

Foundry Doctor is a read-only validation and diagnostics CLI for `azd` projects
that deploy Microsoft Foundry resources.

- Standalone binary: `foundry-doctor`
- azd extension entrypoint: `azd foundry ...`
- Repository config: `.foundry-doctor/config.yaml`
- Minimum verified azd JSON contract: `azd version --output json` with nested
  `{"azd":{"version":"..."}}`
- Minimum verified Bicep CLI: `0.48.1`

## Status

V1 command and configuration surface are defined. The repo includes:

- offline validation, explain, compare, annotate, graph, cost, preflight, runtime, and WAF assessment commands
- strict repository configuration schema in `schemas/config.schema.json`
- release workflow for cross-platform archives, checksums, SBOM, Sigstore signing, provenance, and azd extension metadata
- local release rehearsal and install scripts under `scripts/`

Foundry Doctor is a community/independent tool. It is not part of Microsoft's
first-party `azd ai` command set.

## Quick start

```bash
go run ./cmd/foundry-doctor version
go run ./cmd/foundry-doctor doctor --local --dir samples/good
go run ./cmd/foundry-doctor compare dev prod --dir <repo>
```

## Documentation

- [Configuration](docs/configuration.md)
- [Demo runbook](docs/demo/README.md)
- [Preflight](docs/preflight.md)
- [Runtime](docs/runtime.md)
- [Graph](docs/graph.md)
- [Cost](docs/cost.md)
- [Architecture decisions](docs/decisions/)
- [V1 product ADR addenda](docs/adr/)

## Authentication

Azure access stays CLI and environment based by design:

1. `AZURE_ACCESS_TOKEN`
2. `azd auth token`
3. `az account get-access-token`

Managed identity and GitHub Actions OIDC guidance are documented in
[Configuration](docs/configuration.md#authentication-and-azure-access).

## Release artefacts

Release automation and rehearsal produce:

- standalone binaries and zip archives for `windows` on `amd64` and `arm64` (Windows is the only supported platform)
- azd extension zip archives for the same targets
- `checksums.txt`
- `extension.yaml` and `registry.json`
- SPDX SBOM
- Sigstore bundles for archives and release metadata
- GitHub provenance attestations

See [release rehearsal notes](docs/development/release-rehearsal.md) and
[CHANGELOG](CHANGELOG.md).

## Exit codes

`doctor` (and `preflight` and `runtime`, which match it) exit with one code, chosen in this order:

| Code | Meaning |
|---|---|
| 4 | Internal error. |
| 2 | The requested validation could not run (invalid input or configuration, unavailable dependency, or a required skip, see below). This wins over findings. |
| 3 | `--strict` was set and a skip hid a check (missing input, capability or version). `not-implemented`, `not-applicable`, `profile-key-missing` and `synthetic-infrastructure` skips do not count. |
| 1 | At least one finding at or above `--fail-on` (default `error`) that is not suppressed or baselined. |
| 0 | None of the above. |

`--rules` with an exact rule ID (for example `--rules FND-CFG-001`) marks that rule as requested. Any skip from a requested rule, including a
`not-implemented`, `not-applicable` or uncertain skip, counts as a required skip and gives exit 2, with or without `--strict`. Globs (`FND-CFG-*`),
groups (`CFG`), pillars and categories do not mark rules as requested. Source: `internal/app/wire.go` `BuildSelector`, `internal/rules/rules.go`
(`Required`) and `internal/report/exit.go`.
