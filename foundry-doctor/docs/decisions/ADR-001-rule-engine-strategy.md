# ADR-001: Rule engine strategy

Status: Proposed (needs the Foundry lead and Azure black belt reviews recorded in the Phase 0 hand-off)
Date: 2026-10-04

## Context

The PRD makes the rule catalogue the product and says deterministic engines own every pass/fail result.
It also says to avoid duplicating lower-level tools, and to keep the core free of hidden dependencies
(PRD sections 2 and 9; Phase 0 "Tool overlap is higher than expected -> reduce native implementation and
strengthen adapters").

Phase 0 mapped all 108 rules to PSRule for Azure, Azure Policy, Defender for Cloud, Advisor, the Bicep linter
and Checkov (`docs/overlap-analysis.md`, `docs/overlap/*.md`). Result, from the catalogue metadata:

| Decision | Rules | Meaning |
|---|---|---|
| native | 68 | No equivalent, or Foundry-specific correlation is the whole value. |
| adapt | 37 | A lower-level check exists for the same property. We write our own code for the Foundry value: scoping to resources a project uses, cross-file correlation, per-profile severity, one evidence model. |
| wrap | 2 | Run an existing tool and normalise its output (FND-SEC-014 via the Bicep linter, FND-DEP-009 via the Policy restrictions API). |
| reuse | 1 | An existing tool already gives equivalent evidence; we map its ID (FND-SEC-005). |
| drop | 0 | |

86 rules are verified against a primary source and 22 are labelled product opinion.

## Decision

1. **Native Go engine owns pass/fail.** All `native` and `adapt` rules (105 of 108) are Go code in `internal/rules/<group>`.
   Their implementation owner in the catalogue is `native`. The catalogue validator enforces this.
2. **External tools are adapters behind one interface** (`ExternalRuleAdapter`, owned by the consumer in `internal/adapters`).
   They produce normalised `Finding` records with `Adapter` set. Adapters never decide severity; the profile does.
3. **Only `wrap` and `reuse` rules depend on an external owner.** `wrap` and `reuse` rules need the tool to be present for that
   rule; when it is not, the rule is reported as skipped, never passed.
4. **No bundled runtimes.** PowerShell (PSRule), Python (Checkov) and Go (KICS) are detected, not shipped.
   The only required external dependency is the Bicep CLI, and only when Bicep checks are requested (ADR-002).
5. **Canonical IDs and de-duplication.** Every external finding maps to at most one canonical `FND-*` rule through
   the rule's `overlap` lists. When both the native rule and an adapter report the same property on the same resource,
   the native finding wins and the adapter finding is attached as supporting provenance.
6. **Missing-tool behaviour** follows `docs/tool-compatibility.md` section 3:

   | Situation | Behaviour | Exit |
   |---|---|---|
   | Required dependency missing or too old | Error naming tool, detected and required version | 2 |
   | Optional adapter on `auto`, missing | `adapter-unavailable` report entry; dependent checks skipped | 0/1, or 3 with `--strict` |
   | Adapter explicitly enabled, missing | As required | 2 |
   | Adapter crashes or emits unparseable output | Protocol failure with redacted stderr | 4 |

   The report carries a `tools` section (name, path, version, status) so CI can tell "passed" from "not run".
7. **Reverting `adapt` to `reuse`.** The group research notes in `docs/overlap/` say that several `adapt` rules become `reuse`
   if their Foundry-specific scoping is not delivered in its phase (for example FND-SEC-001 to 004, FND-NET-011, FND-REL-001
   to 003 and FND-REL-009). A rule that only repeats a lower-level check without the added value must be converted to `reuse`,
   not shipped as native. The Phase 1 and Phase 4 reviews check this for each such rule.
8. **Terrascan is not an adapter.** The repository is archived (`docs/tool-compatibility.md` section 4). Checkov and KICS
   remain optional adapters.

## Consequences

- The MVP engine is Go-only for 105 rules and works offline without PowerShell or Python.
- Adapter work in Phase 1 is limited to the optional PSRule adapter and the Bicep diagnostics path.
- Catalogue metadata is the contract: `overlap.decision`, `implementation.owner` and the mapped external IDs drive
  de-duplication, so they must be kept accurate (CI runs `scripts/claude/check-rule-catalog.sh`).
- Defender for Cloud and Advisor mappings are empty because their sources were unreachable in Phase 0. Empty means
  not verified, not "no equivalent". They are revisited when those adapters are built (Phase 4).

## Evidence

`rules/catalog/**`, `docs/overlap-analysis.md`, `docs/overlap/*.md`, `docs/tool-compatibility.md`,
`docs/licence-inventory.md`. Counts computed by `go run ./cmd/rulecatalog generate-overlap` on 2026-10-04.
