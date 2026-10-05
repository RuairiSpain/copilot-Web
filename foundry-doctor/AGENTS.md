# Agent Instructions

Tool-neutral summary for coding agents. `CLAUDE.md` has the full version.

## Objective

Implement Foundry Doctor, a Go rule platform that validates Microsoft Foundry
`azure.yaml`, Bicep, deployed Azure resources and selected runtime dependencies,
according to `docs/requirements/Foundry_Doctor_Implementation_PRD.md`.

## Workflow

1. Read `CLAUDE.md`, the active PRD phase, applicable ADRs and the latest hand-off.
2. Inspect the existing code before proposing changes.
3. Write a plan mapped to the phase Definition of Done.
4. Implement code, tests, documentation and samples.
5. Run `scripts/claude/verify-phase.sh`.
6. Obtain independent Foundry and Azure architecture reviews.
7. Resolve findings, then write the phase hand-off.

## Constraints

- Deterministic rules own all pass/fail decisions.
- Azure checks are read-only.
- Skipped does not mean passed.
- Do not parse Bicep by hand.
- Do not expose secrets or retrieved document content.
- Do not invent Azure or Foundry APIs, properties or limits.
- Do not mark a phase complete with unmet Definition of Done items.
