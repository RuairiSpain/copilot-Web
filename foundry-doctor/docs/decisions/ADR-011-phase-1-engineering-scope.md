# ADR-011: Engineering scope for Phase 1

Status: Accepted except the OS list, which is Proposed (owner to confirm)
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
6. **Supported operating systems (Proposed, owner to confirm):** linux, darwin and windows, each on amd64 and arm64. Release builds and CI smoke tests cover all six; the path handling rules (forward slashes in reports, confined file reads) are tested on windows.
7. **Dependencies:** `github.com/spf13/cobra` v1.10.2 is approved as the only new third-party dependency in Phase 1. Its entry goes into `docs/licence-inventory.md` in the same change that adds it. Any other dependency needs a new ADR or an amendment.

## Consequences

- Third parties cannot write rules in Phase 1; they can write adapters and reporters.
- The hand-written SARIF structs and config validation are our code to maintain and test.
- If the owner rejects the OS list, only decision 6 and the release matrix change.
