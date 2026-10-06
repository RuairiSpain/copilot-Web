# ADR-008: Finding fingerprint

Status: Accepted
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
