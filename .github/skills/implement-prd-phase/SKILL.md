---
name: implement-prd-phase
description: Implement or verify one Foundry Doctor PRD phase from planning through implementation, acceptance gates, independent reviews, audit, and hand-off.
---

# Implement a Foundry Doctor PRD phase

Use the phase number from the user's request and work on the current feature branch or an isolated worktree.

1. Read `foundry-doctor/CLAUDE.md`, `foundry-doctor/AGENTS.md`, the phase section, PRD sections 7, 19, 20, and 22, all ADRs, the newest hand-off, and allocated catalogue entries. Stop if a prerequisite phase is missing.
2. Use `implementation-coordinator` to map every Definition of Done item to disjoint work packets, packages, and tests. Record PRD conflicts as ADR issues.
3. Verify every Azure and Foundry fact against catalogue sources or current primary documentation before implementation.
4. Settle contract changes with `lead-architect`, then assign disjoint packets to `go-implementer`; include `bicep-specialist`, `devops-release`, and `test-engineer` when relevant.
5. Run `foundry-doctor/scripts/claude/verify-phase.sh` and fix failures without weakening tests or requirements.
6. Request independent `foundry-lead` and `azure-black-belt` reviews, plus `go-principal-engineer` for Go changes and `security-reviewer` for Azure, adapters, reports, annotation, runtime, or LLM changes.
7. Resolve blocking findings or record an evidence-backed ADR, then rerun gates and rejected reviews.
8. Create the phase hand-off from `foundry-doctor/docs/development/hand-offs/TEMPLATE.md` and have `implementation-coordinator` audit every Definition of Done item.

Do not declare a phase complete while an item is unmet and not waived by ADR. Only commit, push, or open a pull request when the user has requested those actions.
