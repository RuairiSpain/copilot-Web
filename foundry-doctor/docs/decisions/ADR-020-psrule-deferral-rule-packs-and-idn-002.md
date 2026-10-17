# ADR-020: PSRule adapter deferral, reserved rule packs and FND-IDN-002 in Phase 1

Status: Accepted
Date: 2026-10-16

## Context

The Phase 1 Definition of Done audit found three configuration and scope items that the code parses or lists but does not act on:

- `advanced.psrule.enabled` is parsed and resolved (default `auto`) but no PSRule adapter exists. PRD section 9 describes the PSRule adapter as optional.
- `rules.packs` is parsed and defaults to `[foundry-core]`, and `rules/packs/foundry-core.yaml` exists, but nothing selects rules by pack. Every registered rule runs.
- FND-IDN-002 is allocated to Phase 1 in the catalogue and the pack, but needs effective permission data (ADR-001, `docs/overlap/net-idn.md`) that the offline Phase 1 inputs do not provide.

A setting that does nothing, with no message, would let a user believe a check ran. The project principle is that skipped is never passed and gaps are visible.

## Decision

1. **The PSRule adapter is deferred out of Phase 1.** Reasons: the user decided to defer PSRule wiring; PRD section 9 marks the adapter optional; PSRule needs PowerShell and the PSRule for Azure module, which ADR-001 says are detected and never bundled, so it can only be an opt-in
   runtime dependency; and a separate rules-only PSRule folder, `foundry-rules/`, now exists in the repository, so the PSRule rule content has a home that does not depend on Go adapter code. `FND-SEC-005` (reuse, owner PSRule)
   keeps being reported as skipped. The adapter is picked up in a later phase by a new ADR.
2. **`advanced.psrule.enabled: true` produces a visible warning.** `configAdapter.Resolve` (`internal/app/wire.go`) writes
   `warning: advanced.psrule.enabled is true but the PSRule adapter is not available in this release (ADR-020); no PSRule checks run and none are reported as passed`
   to stderr. `auto` and `false` are silent: `auto` means "if available", and it is not available. The `FND-SYS-ADAPTER-UNAVAILABLE` finding of ADR-009 is not emitted for this case in Phase 1 because it would add an info finding to
   every report; revisit when the adapter exists.
3. **`rules.packs` is reserved.** In Phase 1 every registered rule runs, subject to `rules.include`, `rules.exclude`, the profile and `--rules`. Naming any pack other than `foundry-core` writes
   `warning: rules.packs names <names> but rule packs are reserved and not applied (ADR-020); every registered rule runs`. `foundry-core` is the default and is silent. `rules/packs/foundry-core.yaml` stays as the documented list of
   Phase 1 rules and is validated by the catalogue check; it is not read at run time.
4. **FND-IDN-002 stays catalogue-only in Phase 1.** It remains in the catalogue, the profiles and the pack with its sources, but no check is registered, so the engine reports it as skipped (`not-implemented`), never passed. Phase 1
   therefore implements **37 of the 38** allocated rules, which satisfies the PRD target of 35 to 40 rules (PRD section 12). Implementing it needs role-assignment data (Cosmos DB data roles, the Foundry Agent Consumer role, storage
   container names) whose GUIDs and naming are recorded as open questions in `docs/overlap/net-idn.md`; they need a verified primary source first. The package comment of `internal/rules/idn` says so.
5. **Counts in documents.** "38" means the Phase 1 allocation (the catalogue and pack size); "37" means rules implemented in Phase 1. Documents that said 29 implemented or 38 implemented are corrected to 37 of 38.

## Consequences

- No silent no-op settings: the two inert configuration keys explain themselves on stderr.
- A later phase can add the adapter or pack selection without changing configuration files, because the keys already exist.
- The two warnings go to stderr only; JSON and SARIF reports are unchanged, so existing goldens hold.
- Tests: `internal/app/wire_config_warn_test.go` covers the silent defaults, `psrule: true` and an extra pack.
