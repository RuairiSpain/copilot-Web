# ADR-001: Rule engine strategy

Status: Accepted architecture; implementation and adapter evidence remain provisional (ADR-008)
Date: 2026-10-04

## Context

The PRD makes the rule catalogue the product and says deterministic engines own every pass/fail result.
It also says to avoid duplicating lower-level tools, and to keep the core free of hidden dependencies
(PRD sections 2 and 9; Phase 0 "Tool overlap is higher than expected -> reduce native implementation and
strengthen adapters").

Phase 0 recorded metadata mappings for all 108 rules to PSRule for Azure, Azure Policy, Defender for Cloud, Advisor, the Bicep linter
and Checkov (`docs/overlap-analysis.md`, `docs/overlap/*.md`). Result, from the catalogue metadata:

| Decision | Rules | Meaning |
|---|---|---|
| native | 68 | No equivalent, or Foundry-specific correlation is the whole value. |
| adapt | 38 | A lower-level check exists for the same property. We write our own code for the Foundry value: scoping to resources a project uses, cross-file correlation, per-profile severity, one evidence model. |
| wrap | 1 | Run an existing tool and normalise its output (FND-SEC-014 via the Bicep linter). |
| reuse | 1 | An existing tool already gives equivalent evidence; we map its ID (FND-SEC-005, owner PSRule). |
| drop | 0 | |

The current catalogue has **82** rules verified against a primary source and **26** labelled
product opinion. The earlier Phase 0 snapshot had 85 verified and 23 product-opinion rules; those
historical counts must not be used for the current tree.

## Decision

1. **Native Go engine owns pass/fail.** All `native` and `adapt` rules (106 of 108) are Go code in `internal/rules/<group>`.
   Their implementation owner in the catalogue is `native`. The catalogue validator enforces this.
2. **External tools are adapters behind one interface** (`ExternalRuleAdapter`, owned by the consumer in `internal/adapters`).
   They produce normalised `Finding` records with `Adapter` set. Adapters never decide severity; the profile does.
3. **Only `wrap` and `reuse` rules depend on an external owner.** `wrap` and `reuse` rules need the tool to be present for that
   rule; when it is not, the rule is reported as skipped, never passed. Concretely, FND-SEC-005 (reuse, PSRule) never runs in a
   Go-only install, and FND-SEC-014 (wrap, Bicep linter) runs whenever the Bicep CLI is present. Calls to Azure APIs such as
   `checkPolicyRestrictions` (FND-DEP-009, `adapt`) are not adapters: they go through the native Azure client under ADR-006.
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
   | Explicitly requested adapter cannot start or its required input/permission is unavailable | Requested validation could not run | 2 |
   | Adapter starts but violates its output protocol or Foundry Doctor fails internally | Protocol/internal failure with redacted stderr | 4 |

   The report carries a `tools` section (name, path, version, status) so CI can tell "passed" from "not run".
7. **Reverting `adapt` to `reuse`.** The group research notes in `docs/overlap/` say that several `adapt` rules become `reuse`
   if their Foundry-specific scoping is not delivered in its phase (for example FND-SEC-001 to 004, FND-NET-011, FND-REL-001
   to 003 and FND-REL-009). A rule that only repeats a lower-level check without the added value must be converted to `reuse`,
   not shipped as native. The Phase 1 and Phase 4 reviews check this for each such rule.
8. **Terrascan is not an adapter.** The repository is archived (`docs/tool-compatibility.md` section 4). Checkov and KICS
   remain optional adapters.

## Consequences

- The MVP engine is Go-only for 106 rules and works offline without PowerShell or Python.
- Adapter work in Phase 1 is limited to the optional PSRule adapter and the Bicep diagnostics path.
- Catalogue metadata is the contract: `overlap.decision`, `implementation.owner` and the mapped external IDs drive
  de-duplication, so they must be kept accurate (CI runs `scripts/claude/check-rule-catalog.sh`).
- Defender for Cloud and Advisor mappings are empty because their sources were unreachable in Phase 0. Empty means
  not verified, not "no equivalent". They are revisited when those adapters are built (Phase 4).

## Evidence

`rules/catalog/**`, `docs/overlap-analysis.md`, `docs/overlap/*.md`, `docs/tool-compatibility.md`,
`docs/licence-inventory.md`. The 85/23 counts were computed by
`go run ./cmd/rulecatalog generate-overlap` on 2026-10-04 and are historical. The current 82/26
counts come from the current catalogue metadata and passed the local catalogue/document checks
recorded in the Phase 0 hand-off. Counts describe catalogue metadata, not executed adapter
compatibility. See ADR-008 for the structured-output, XF, synthetic-spike and release-licence
evidence still required.
