# ADR-008: Finding fingerprint

Status: Accepted, amended 2026-10-16 (Amendments 1 to 3)
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

## Amendment 3 (2026-10-16): per-finding lower severity and confidence, partial skips, and fingerprints (round-2 review R8, M3)

1. **Overrides.** `sdk.Finding.Severity` and `sdk.Finding.Confidence` already existed (the engine filled them). A rule MAY now set either field on a finding it returns. `rules.Enrich`
   treats the catalogue value for the active profile as a ceiling:
   - a severity override is honoured only when it is valid and strictly lower than the catalogue severity (info < warning < error); an equal, higher or invalid value is ignored;
   - a confidence override is honoured only when it is valid and strictly lower than the catalogue confidence (low < medium < high; the catalogue `certain` maps to high and `likely` to medium);
   - **platform-basis rules stay `error` in every profile**: a severity override on a rule whose basis contains `platform` is ignored (tested in `internal/rules/override_test.go`). A confidence override is
     still honoured for them, because confidence is not part of the "platform rules are errors" guarantee.
   An override can therefore never raise severity or confidence, never skip a rule and never hide a finding.
2. **Fingerprint.** Severity, confidence and the override are not inputs to the fingerprint (unchanged from Amendment 1 decision 5). A baseline entry keeps matching when a rule starts or stops
   lowering a finding, and `--fail-on` thresholds see the lowered severity.
3. **First users.** FND-NET-001 (version 3) lowers severity to `warning` for `policy.network.publicAccess: entra-only` and to `info` for `allowed` (ADR-007); in the dev profile the catalogue value is already `info`,
   so nothing changes there. FND-IDN-003 (version 4) sets confidence `likely` (medium) on a name-only client-ID match; a `.clientId`, `.appId` or `.applicationId` property access stays `certain`.
   FND-IDN-004 (version 4) emits an `info` finding for Contributor at a single resource scope (see the catalogue note on FND-IDN-004).
4. **Partial skips.** `sdk.Result` gains `Skips []sdk.Skip`. A rule that evaluates some resources and cannot evaluate others returns its findings and one skip per unevaluated resource.
   The engine appends them to `Report.Skipped` with the rule ID (and `Required` when the rule was explicitly requested); the rule still counts as evaluated. `Skips` must not be combined with `Skipped`
   (the engine reports `rule-error`). This replaces the ` [note: N resource check(s) not evaluated ...]` suffix on the first finding's evidence, which put information about other resources into a
   finding. Skip reasons keep their reason head (for example `unresolved-expression`), so `ClassifySkip` and `--strict` treat them as before. The engine's `stableEvidence` workaround was removed;
   derived keys (Amendment 2) now see the evidence the rule wrote. Fingerprints are unaffected by skips.
5. **Keys.** The remaining multi-finding paths now set `Finding.Key`: NET-005 (agent subnet region, endpoint region), NET-008 (peered pair and prefixes), NET-009 (one key per SKU check), NET-010 (public telemetry,
   missing Azure Monitor Private Link Scope, per-scope endpoint and access modes), IDN-001, SEC-001/002/003 (`disableLocalAuth`), SEC-008 (`pricingTier`), SEC-009 (`enforcementMode`), SEC-012 and SEC-013.
   Their rule versions were bumped because their fingerprints change once.
