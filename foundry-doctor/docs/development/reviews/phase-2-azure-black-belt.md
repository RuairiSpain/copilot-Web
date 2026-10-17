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

## Round 2 (2026-10-17)

Reviewer: `azure-black-belt`. Verdict: **REJECT**. This is a summary recorded by the catalogue owner from the verdict relayed by the parent session; the full report is held by the parent session.

Blocking findings:

1. **N1 FND-DEP-009 partial content, false fail.** A deny on content from which fields were dropped could fail a resource for a field that was never sent; tags, sku, kind, identity and zones were also not part of the content.
2. **N2 Workflow `--strict` and `--what-if`.** The reusable workflow ran `--strict`, which always exits 3 because FND-DEP-008 and FND-DEP-009 are uncertain by design, and it had no way to request `--what-if`.
3. **N3 What-if poll query allow-list.** The allow-list for the long-running what-if poll request and its query parameters.
4. **N4 ADR-021 (e) exit criterion.** "In the phase in which the catalogue verifies the property path" is vague.

Status: **fixed in round 2, pending re-review.**
- N1: FND-DEP-009 `version: 3`; the content passes through the sanitizer including tags, sku, kind, identity and zones; a deny after any dropped field is an uncertain note; at most 50 checks per run.
- N2: the workflow does not use `--strict` and has a `what-if` input (default false) (not yet run on GitHub).
- N3: owned by the Go agent; the matrix keeps the poll path and Location query parameters UNVERIFIED (ARM Location URLs are opaque); the capability name `Microsoft.Resources/deployments/operationstatuses/read` is VERIFIED.
- N4: ADR-021 (e) now has one owner rule, PRD phase and exit criterion per reference kind (FND-RUN-003, FND-NET-002, FND-IDN-002, FND-SEC-005) and states the covered set exactly (what-if predicted change IDs and the agent network-injection subnet).

Open for the lead: the PRD section 20 allocation leaves FND-NET-002 and FND-SEC-005 without a phase that can carry the live check, and FND-IDN-002 has a Phase 2 slot that the Phase 2 scope does not use. An ADR is needed. Nothing is closed until the reviewer confirms it.
