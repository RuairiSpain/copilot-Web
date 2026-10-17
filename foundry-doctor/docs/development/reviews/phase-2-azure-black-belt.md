# Phase 2 review: Azure black belt

Reviewer: `azure-black-belt` (independent; did not write the code). Date: 2026-10-17. Round 1. Verdict: **REJECT**.
Scope: Phase 2 Azure read-only adapters and deployment preflight (FND-DEP-001..012), `docs/permissions-matrix.md`, samples, workflow, ADRs.

This file is a concise summary kept in the repository; the full report was delivered to the parent session. Remediation status is tracked in `docs/development/hand-offs/phase-2.md` (all items fixed in round 1, pending re-review, except the documented exceptions and waivers).

## Blocking findings

1. **B1 Locks.** Lock lists return locks at and below the scope; relevance by ancestor and planned resource, and resource-level reads, were missing.
2. **B2 FND-DEP-002 namespace set.** Namespaces of resources that the template defines are registered by ARM and must not be a finding.
3. **B3 FND-DEP-009 content.** The resource content sent to checkPolicyRestrictions could contain unresolved expressions or secret-like values.
4. **B4 API versions.** Inventory used api-version 2022-12-01, which the spec does not publish for Microsoft.Resources/resources.
5. **B5 RBAC strings and sample role.** Capability strings, link-read action names and the sample role definition were wrong or incomplete.
6. **B6 Missing rule tests.** Several rules lacked positive, negative, skipped or uncertain tests.
7. **B7 What-if status, Incremental, Replace.** What-if status handling, Incremental mode (no deletions reported) and the absent Replace change type were mishandled.

## Non-blocking findings

N1-N12: recorded in the reviewer report held by the parent session; each is tracked as "fixed in round 1, pending re-review" in the hand-off.
