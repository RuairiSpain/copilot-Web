---
name: research-rule-overlap
description: Compare proposed Foundry Doctor rules with PSRule for Azure, Azure Policy, Defender for Cloud, Advisor, the Bicep linter and third-party scanners; record reuse/wrap/adapt/native/drop decisions and update the overlap matrix. Use for Phase 0 and when proposing new rules.
argument-hint: "<rule group or rule ids>"
---

# Research rule overlap

Delegate groups to `rule-catalogue-owner` agents in parallel (one per rule group, e.g.
CFG, SEC, NET, IDN) and have `researcher` check tool coverage claims. For each rule:

1. Read its PRD entry and any catalogue metadata.
2. Search current primary documentation for the platform fact.
3. Inspect the current rule sets of PSRule for Azure, Azure Policy built-ins, Defender,
   Advisor and the Bicep linter. Inspect Checkov or KICS only if configured.
4. Record coverage as full, partial or none, plus differences in evidence, profile
   handling, source-location quality, and deployment or runtime limits.
5. Recommend reuse, wrap, adapt, native or drop, with the reason.
6. Propose new high-value rules for developers and product managers: deployment
   feasibility, supportability, schema lifecycle, release readiness, cost visibility.

Update `docs/overlap-analysis.md`, `rules/catalog/` and regenerate `docs/rule-catalog.md`.
Every row needs a source URL and verification date, or an explicit "product opinion" label.
Phase 1's MVP must end up with 35-40 rules. Do not copy PSRule implementation code.
