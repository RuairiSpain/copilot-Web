# ADR-009: V1 command, naming, and config surface

Status: Accepted  
Date: 2026-10-06

## Context

The PRD used provisional names for the command layout and repository
configuration while Phase 0 validated azd namespace compatibility and the
closed configuration schema.

V1 needs one final public surface for:

- the standalone binary
- the azd extension command namespace
- the repository configuration path
- compare exit-code semantics

## Decision

1. The standalone binary remains **`foundry-doctor`**. The earlier PRD wording
   that referred to `cmd/foundry` is superseded.
2. The azd extension entrypoint remains **`azd foundry ...`**, with the
   extension binary entrypoint named **`foundry-doctor-azd`**.
3. The repository configuration path is finalized as
   **`.foundry-doctor/config.yaml`**.
4. `compare` is an **informational** command by default:
   - exit `0` when differences are found
   - exit non-zero only when the caller opts in with `--fail-on-diff`
5. Output-writing commands use **`--out`** for file or directory destinations.
   `--output` remains azd-reserved in extension mode.

## Consequences

- Documentation, release assets, and samples refer to `foundry-doctor` as the
  canonical executable name.
- `azd foundry ...` stays the supported azd namespace while ADR-003's
  collision monitor remains in the release pipeline.
- Repository automation and docs can rely on a single config path and one
  compare contract.
