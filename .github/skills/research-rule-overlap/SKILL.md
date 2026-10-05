---
name: research-rule-overlap
description: Compare Foundry Doctor rules with PSRule for Azure, Azure Policy, Defender, Advisor, Bicep linter, and configured scanners; record reuse, wrap, adapt, native, or drop decisions.
---

# Research rule overlap

For each requested rule group or rule ID:

1. Read the PRD entry and catalogue metadata.
2. Verify the platform fact in current primary documentation.
3. Inspect current PSRule for Azure, Azure Policy, Defender, Advisor, Bicep linter, and configured third-party coverage.
4. Record coverage as full, partial, or none, including differences in evidence, profiles, source location, deployment preflight, and runtime diagnosis.
5. Recommend reuse, wrap, adapt, native, or drop with a reason.
6. Propose only high-value developer and product-manager rules for deployment feasibility, supportability, schema lifecycle, release readiness, or cost visibility.

Update `foundry-doctor/docs/overlap-analysis.md`, catalogue entries, and generated rule documentation. Every external claim needs a source URL and verification date or an explicit product-opinion label. Do not copy another scanner's implementation.
