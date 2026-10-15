# ADR-008: Finding fingerprint

Status: Accepted, amended 2026-10-15 (Amendment 1)
Date: 2026-10-05

## Context

Baselines hide only matching fingerprints (PRD section 9, `CLAUDE.md`). A baseline entry must survive edits that do not change the
problem (a line moves, the profile changes severity) and must stop matching when the problem changes. The PRD defines `Finding.Fingerprint`
but not how it is computed. ENV rules look at azd environment values, which can be secrets.

## Options considered

1. Hash of rule ID, file and line. Simple, but any edit above the finding invalidates the baseline.
2. Hash of the evidence text. Stable per message, but evidence wording changes with rule versions and may carry values.
3. Hash of rule identity plus logical resource identity plus a rule-chosen key. Stable under moves, explicit about what makes two findings the same.

## Decision

Option 3. `Fingerprint = "fp1:" + hex(SHA-256(canonical))`, truncated to the first 32 hex characters after the prefix. The prefix versions the scheme.
`canonical` is the UTF-8 join, with a single `0x00` separator, of:

1. `RuleID`
2. `RuleVersion` (decimal). A rule version bump therefore re-surfaces old findings on purpose: the rule changed meaning.
3. Logical resource identity: `Resource.Kind`, lower-cased `Resource.Type`, `Resource.Name` (the logical name, not an expression and not an ID that embeds a subscription).
4. `Finding.Key` (empty when a rule produces one finding per resource).
5. The normalised evidence key: the rule-supplied stable property path or token, `Resource.Pointer` if `Key` is empty, with whitespace trimmed and case preserved.

Excluded: line and column, file path when the resource has a logical name, `Profile`, `Severity`, `Confidence`, `Evidence` prose, `Recommendation`, `Adapter`, and anything wall-clock or machine specific.

Rules:

- ENV rules never hash a raw environment value. Their `Key` is the environment key name (and the environment name when the rule is per environment). Evidence and findings print key names only. Value comparison for `compare` uses a separate truncated hash and is not a fingerprint (see `internal/compare`).
- The engine computes the fingerprint after the rule returns; rules cannot set it. A test fails if a rule sets `Fingerprint`, `Severity` or `Profile`.
- Output is deterministic: identical inputs give identical fingerprints on every OS. Paths use forward slashes.
- Two findings with the same fingerprint in one run are merged only if their evidence is identical; otherwise the engine reports an engine diagnostic (ADR-009) because the rule's `Key` is not distinguishing.

## Consequences

- A baseline survives line moves, reformatting and profile changes. It stops matching when the rule version, resource identity or key changes.
- Renaming a resource re-surfaces its findings. This is accepted: a rename is a change.
- Rule authors own the quality of `Key`; the rule test template checks that distinct findings of one rule have distinct fingerprints.
- Fingerprint scheme changes need a new prefix (`fp2:`) and an ADR; the baseline reader rejects unknown prefixes with exit 2.

## Amendment 1 (2026-10-15): implemented form and `Finding.Key`

Review findings (Foundry lead N12, Phase 1 re-review) showed that the code ignored `Key`: FND-CFG-005 and FND-CFG-006 findings on one service shared one fingerprint. The
code also differed from the decision text. This amendment makes the text describe the code and adds the missing discriminator.

1. **Implemented canonical form (`internal/findings.Fingerprint`, scheme tag `v2`).** The SHA-256 input is the `0x00`-joined sequence
   `v2`, `RuleID`, `RuleVersion` (decimal), lower-cased `Resource.Type`, `Resource.Name`, `Resource.ID`, lower-cased `Location.File`, `Key`.
   The output is the first 32 hex characters, **without** the `fp1:` prefix, because the baseline and suppression file schemas accept only `^[0-9a-f]{32}$`
   (`internal/baseline`, `internal/suppress`). The scheme tag is hashed inside the canonical string instead of being printed. `findings.FingerprintVersion` holds it.
   `Resource.Kind` and `Resource.Pointer` do not exist in `pkg/sdk`; `Key` replaces the evidence-key item of decision 5.
2. **`Finding.Key` added to `pkg/sdk`** (JSON `key`, omitted when empty). A rule sets it to a stable, non-secret discriminator: the azure.yaml key path, variable name, or ARM resource
   key. A rule that can emit several findings on one resource MUST set it (the rule test template checks that distinct findings have distinct fingerprints).
3. **Version bump v1 to v2.** The previous tag was `v1` and had no `Key` input. Every fingerprint changes once, including findings whose `Key` is empty. Baselines and
   suppressions that name a fingerprint must be regenerated. Phase 1 is unreleased (`0.1.0-dev`), so no migration is provided.
   A future change to the canonical form needs tag `v3` and an amendment here.
4. **Collision handling in the engine (`internal/rules`).** After enrichment, findings that share a fingerprint and have identical redacted evidence are merged. Findings that share a
   fingerprint with different evidence indicate that the rule set no `Key`: the engine appends `#ev:` plus the first 12 hex characters of SHA-256 of the evidence to `Key`,
   recomputes the fingerprint, and reports a diagnostic (`Report.Diagnostics`, printed as a warning). This fallback depends on evidence wording and is a safety net, not a substitute for a rule-set `Key`.
   The `FND-SYS-DUPLICATE-FINGERPRINT` finding of ADR-009 is not emitted in Phase 1; the diagnostic is a stderr warning.
5. Excluded inputs are unchanged: line, column, profile, severity, confidence, evidence prose, recommendation, adapter, wall-clock and machine-specific data.

## Amendment 2 (2026-10-15): no sibling dependence (Foundry lead R1)

Amendment 1 decision 4 derived a `Key` from evidence only when two findings collided. That made a fingerprint depend on whether a sibling finding existed (a baselined
finding resurfaced when a second finding appeared on the same resource) and merged findings that had equal evidence but different locations (duplicate `host` keys gave one finding).

1. **Scheme tag `v3`.** The canonical form (decision 1 of Amendment 1) is unchanged; the tag is bumped because the `Key` of key-less findings now changes. Every fingerprint changes
   once. Golden files and the frozen-value test were regenerated. Phase 1 is unreleased, so no migration is provided.
2. **Derived key for key-less findings.** After enrichment the engine sets, for every finding whose rule set no `Finding.Key`,
   `Key = "auto:<line>:<column>:<first 12 hex of SHA-256 of the redacted evidence>"`. The value depends only on the finding itself, never on other findings, so adding or removing a
   sibling does not change it. Cost: a key-less finding's fingerprint changes when its line, column or evidence wording changes. Rules that need a baseline stable across line moves MUST set `Key`.
3. **Merge rule.** Only exact duplicates merge: same fingerprint, same location (file, line, column) and same redacted evidence. Findings with equal fingerprints but a different
   location or evidence are all kept.
4. **Diagnostics (stderr warning).** The engine names every rule whose key-less findings would have shared a fingerprint without the derived key ("the rule sets no Finding.Key").
   It also names a rule that set a `Key` which does not separate findings that differ in location or evidence; those findings keep the same fingerprint (a baseline entry for one hides all of them).
5. Amendment 1 decision 4 is superseded by this amendment.
