---
paths:
  - "foundry-doctor/rules/**"
  - "foundry-doctor/internal/rules/**"
  - "foundry-doctor/docs/rule-catalog.md"
  - "foundry-doctor/docs/overlap-analysis.md"
---

# Rule catalogue constraints

Every rule defines: stable ID (`FND-<GROUP>-<NNN>`), version, title, description,
input planes, basis, category, WAF pillar where applicable, severity for dev/test/prod,
supported azd and Foundry extension versions, deterministic evidence, remediation,
fix example, documentation URL, `lastVerified` date, implementation owner, and
positive, negative and skipped test cases.

- A rule cannot be implemented until its product fact has a verified primary source,
  or it is explicitly labelled product opinion.
- Rule IDs and the phase allocation come from PRD section 20. Do not renumber.
- Do not duplicate a lower-level rule (Bicep linter, PSRule for Azure, Azure Policy,
  Defender, Advisor) unless the Foundry rule adds one of: cross-file correlation,
  Foundry-specific semantics, clearer evidence, environment-aware enforcement,
  deployment preflight, or runtime diagnosis. Otherwise map it to a canonical rule.
- Each rule records a decision: reuse, wrap, adapt, native or drop.
- Changing rule behaviour increments `ruleVersion`. Fingerprints must stay stable
  for unchanged behaviour.
- `docs/rule-catalog.md` is generated from `rules/catalog/`. Never edit it by hand.
