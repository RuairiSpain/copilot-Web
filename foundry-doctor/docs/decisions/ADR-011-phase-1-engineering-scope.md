# ADR-011: Engineering scope for Phase 1

Status: Accepted. Decision 6 (supported operating systems) was decided by the owner on 2026-10-17 and amended the same day (Amendment 2: Windows and Linux)
Date: 2026-10-05

## Context

PRD section 5 gives `pkg/sdk` the job of "stable public interfaces for third-party rules and adapters". Phase 1 needs stable finding, adapter and report
contracts (PRD section 9) but has no third-party rule author yet. The core module has few dependencies (ADR-005) and a new dependency needs a licence entry.

## Decision

1. **`pkg/sdk` holds Finding, Adapter, Reporter and Report only** (plus their value types and the exit-code constants). The Rule interface stays in
   `internal/rules`; a public Rule SDK is deferred until there is a consumer and a stability promise worth making. Everything the engine needs but third parties do not stays internal.
2. **YAML:** `go.yaml.in/yaml/v3` for `azureyaml` (already used by the catalogue loader; supports `yaml.Node` with line and column, and `KnownFields`). `goccy/go-yaml` is not adopted: a second YAML library
   adds a dependency and a second behaviour for anchors and duplicate keys.
3. **No JSON-schema library.** `schemas/config.schema.json` is shipped for editors; the closed-key validation of ADR-007 is Go code over the typed `Policy` struct, tested against the schema's key list.
4. **No go-sarif dependency.** SARIF 2.1.0 output uses a small hand-written set of structs covering only the fields emitted, with golden tests.
5. **Coverage target: 80% statement coverage on engine packages** (`internal/model`, `project`, `config`, `azureyaml`, `bicep`, `graph`, `rules`, `findings`, `baseline`, `suppress`, `report`). Rule packages are held to explicit
   positive, negative and skipped scenarios instead of a percentage (PRD section 9 testing strategy).
6. **Supported operating systems (decided by the owner on 2026-10-17; current text per Amendment 2):** windows and linux, each on amd64 and arm64. macOS is not supported. Tests, CI smoke tests and release builds target these four platforms. The path handling rules (forward slashes in reports, confined file reads) are tested on windows and linux.
7. **Dependencies:** `github.com/spf13/cobra` v1.10.2 is approved as the only new third-party dependency in Phase 1. Its entry goes into `docs/licence-inventory.md` in the same change that adds it. Any other dependency needs a new ADR or an amendment.

## Consequences

- Third parties cannot write rules in Phase 1; they can write adapters and reporters.
- The hand-written SARIF structs and config validation are our code to maintain and test.
- Changing the OS list later changes only decision 6 and the release matrix.

## Amendment 1 (2026-10-17): Windows only (superseded by Amendment 2)

The owner decided that Foundry Doctor is only run and supported on Windows (amd64 and arm64). Linux and macOS are not supported and are not tested by this project. Code that handles paths stays platform-neutral where that costs nothing (for example forward slashes in reports and `os.Root` confinement), but no Linux or macOS behaviour is promised or verified. The earlier proposal (linux, darwin and windows on amd64 and arm64) is withdrawn.

## Amendment 2 (2026-10-17): Windows and Linux

Reason: owner decision, later the same day. This supersedes decision 6 and Amendment 1.

Decision 6 now reads: **Foundry Doctor supports and tests Windows (amd64, arm64) and Linux (amd64, arm64).** macOS (darwin) is not supported, not built, not tested and not released. Linux is also allowed as a target platform for the samples (for example `runs-on: ubuntu-latest` in sample workflows) and as the target operating system of analysed projects.

Consequences:

- CI runs `go test` and the sample smoke test on `ubuntu-latest` and `windows-latest`. The race detector runs on linux/amd64 and windows/amd64. Other jobs run on `ubuntu-latest`.
- Release builds and publishes `linux/amd64`, `linux/arm64`, `windows/amd64`, `windows/arm64`. Standalone binaries are bare files (`.exe` on Windows). azd extension archives are `tar.gz` for Linux and `zip` for Windows.
- ARM64 is built (cross-compiled) but is not executed on any GitHub-hosted runner used here, so linux/arm64 and windows/arm64 behaviour is unproven by CI.
- The official azd extension registry still requires darwin entries. Because macOS stays unsupported, the generated `registry.json` has the four supported platforms only and is for a custom extension source (`azd extension source add`), not for submission to the official registry.
- The reusable offline workflow selects the runner with an `os` input (`linux` or `windows`, default `linux`) and rejects any other value.
