---
name: add-foundry-rule
description: Add or revise a Foundry Doctor rule - overlap research, verified sources, metadata, deterministic implementation, tests, docs and profile severities. Use when asked to implement or change a rule such as FND-SEC-001.
argument-hint: "<rule id>"
---

# Add or revise a rule

1. Look up the ID in PRD section 20 and in `foundry-doctor/rules/catalog/`. Confirm the
   phase allocation matches the current phase.
2. Verify the platform behaviour in a current primary source (Microsoft Learn, REST/ARM
   reference, Context7). Record the URL and today's date. If unverifiable, label the
   rule product opinion or stop.
3. Check overlap with the Bicep linter, PSRule for Azure, Azure Policy, Defender,
   Advisor and configured adapters. Decide reuse, wrap, adapt, native or drop.
4. Fill `rule-template.yaml` (in this skill directory) into `rules/catalog/<group>/<id>.yaml`.
5. Implement deterministic evidence extraction in `internal/rules/<group>/`. No LLM, no network.
6. Add tests: positive, negative, missing-input, skipped/uncertain, each profile's severity,
   redaction of evidence, and fingerprint and `ruleVersion` stability.
7. Regenerate `docs/rule-catalog.md` and run `scripts/claude/check-rule-catalog.sh`.
8. Run `scripts/claude/verify-phase.sh`.
9. Request `foundry-lead` and `azure-black-belt` review. You must not be the reviewer.

Changing behaviour of an existing rule increments `version`. Never implement a rule from
a property name you remember but have not verified.
