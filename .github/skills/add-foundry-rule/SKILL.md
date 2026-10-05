---
name: add-foundry-rule
description: Add or revise a Foundry Doctor rule with source verification, overlap research, deterministic implementation, tests, generated docs, and profile severities. Use for FND-* rule changes.
---

# Add or revise a Foundry Doctor rule

1. Find the rule ID in PRD section 20 and `foundry-doctor/rules/catalog/`; confirm phase allocation.
2. Verify the platform behavior in a current primary source and record its URL and today's date. If unverifiable, label it product opinion or stop.
3. Check overlap with Bicep linter, PSRule for Azure, Azure Policy, Defender, Advisor, and configured adapters. Decide reuse, wrap, adapt, native, or drop.
4. Copy [rule-template.yaml](rule-template.yaml) to the appropriate `foundry-doctor/rules/catalog/<group>/` path and complete every field.
5. Implement deterministic evidence extraction in `foundry-doctor/internal/rules/<group>/`. Do not use an LLM or network call for pass/fail decisions.
6. Add positive, negative, missing-input, skipped or uncertain, profile severity, evidence redaction, fingerprint, and rule-version stability tests.
7. Regenerate `foundry-doctor/docs/rule-catalog.md` and run `foundry-doctor/scripts/claude/check-rule-catalog.sh`.
8. Run `foundry-doctor/scripts/claude/verify-phase.sh`.
9. Request independent `foundry-lead` and `azure-black-belt` reviews.

Increment the rule version when behavior changes. Never implement a remembered property name without verifying it.
