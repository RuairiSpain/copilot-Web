---
name: implement-prd-phase
description: Implement one Foundry Doctor PRD phase (0-9) from plan through parallel implementation, acceptance gates, independent reviews and hand-off. Use when asked to build, continue, complete or verify a numbered phase of the Foundry Doctor PRD.
argument-hint: "<phase number>"
---

# Implement a Foundry Doctor PRD phase

Argument: the phase number. You run this from the main session, because subagents
cannot spawn subagents. Work on branch `claude/foundry-doctor-phase-<n>`.

## 1. Orient
Read `foundry-doctor/CLAUDE.md`, the PRD section for the phase, PRD sections 7, 19, 20
and 22, every ADR in `foundry-doctor/docs/decisions/`, the newest file in
`foundry-doctor/docs/development/hand-offs/`, and the catalogue entries for the phase.
Check the phase's "Dependencies" list. If a prerequisite phase is not merged or its
hand-off is missing, stop and say so.

## 2. Plan
Invoke the `implementation-coordinator` agent. It returns a table of DoD item ->
work packet -> packages -> tests. Show the plan. Record each PRD conflict as an ADR issue.

## 3. Verify facts first
For every Azure or Foundry fact the phase needs, check the catalogue for a verified
source. Send unverified facts to `rule-catalogue-owner` (and `researcher` for library
or tool questions) before any code uses them. Use Context7 for SDK APIs.

## 4. Contracts, then parallel implementation
If the phase changes a contract, `lead-architect` settles it first and commits it.
Then launch one `go-implementer` per packet, in parallel, only for packets with
disjoint packages (use the PRD "Agent-swarm assignments" table). Add `bicep-specialist`,
`devops-release` and `test-engineer` where the phase calls for them. Merge their
worktrees one at a time, resolving conflicts yourself.

## 5. Gates
Run `foundry-doctor/scripts/claude/verify-phase.sh`. Fix failures in the code.
Never weaken a test or a requirement to pass a gate.

## 6. Independent reviews (parallel, read-only)
Launch `foundry-lead` and `azure-black-belt` always; `go-principal-engineer` when Go
production code changed; `security-reviewer` when the phase touches Azure, data-plane,
adapters, reports, annotation or LLM code. Give each only the phase DoD and the diff,
not other reviewers' findings. Keep disagreements; do not average them.

## 7. Remediate
Fix blocking findings or record an ADR with evidence explaining why not. Re-run step 5
and re-request review from any reviewer who rejected.

## 8. Hand-off and audit
Create `foundry-doctor/docs/development/hand-offs/phase-<n>.md` from `TEMPLATE.md`.
Run `implementation-coordinator` in audit mode. Report each DoD item as met, unmet or
waived-by-ADR. Do not call the phase complete while any item is unmet and unwaived.
Then commit, push, and open a draft PR.
