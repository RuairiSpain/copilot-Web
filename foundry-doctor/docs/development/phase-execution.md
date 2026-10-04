# Phase execution

Run a phase with the `implement-prd-phase` skill: `/implement-prd-phase 0`.

Order: 0, then 1. After 1, phases 2, 4, 5 and 6 can proceed; 3 needs 2; 7 and 8 need 2 and 3
(see PRD section 21). Each phase starts from a merged previous phase and its hand-off.

Branch per phase: `claude/foundry-doctor-phase-<n>`. Draft PR per phase.

## Gates

`foundry-doctor/scripts/claude/verify-phase.sh [--strict]`. Exit 0: all runnable gates pass.
1: a gate failed. 3: gates were skipped and `--strict` was given. Skipped gates are
listed in the output and must be listed in the hand-off. Use `--strict` for the final
run of a phase that has Go code.

## PRD inconsistencies and the choice made

Record changes as ADRs in `docs/decisions/` when Phase 0 confirms or reverses them.

| Topic | PRD says | Used for now |
|---|---|---|
| Entry-point directory | Section 5: `cmd/foundry`; Appendix A: `cmd/foundry-doctor` | `cmd/foundry-doctor` (matches the binary name and Appendix A). |
| Catalogue doc name | `docs/rule-catalog.md` (US spelling) | PRD spelling `rule-catalog`. |
| Public command prefix | `azd foundry`, but Phase 0 must confirm no conflict | Keep the prefix in one place (command layer) until ADR-003. |
| Output path flag | `--output <path>` and `compare` environments (command surface, section 4) | The azd extension SDK reserves `-o/--output` and `-e/--environment` and refuses to start if they are reused. Use `--out <path>` in extension mode; see ADR-003. |
| `azd ai agent doctor` | Not mentioned | Microsoft's own `azd ai agent doctor` exists (read-only, overlaps FND-RUN-001, 003, 005). Not a name conflict; documented in ADR-003 and the RUN overlap notes. |
| What-if titles | FND-DEP-008 "delete/replace" | The what-if ChangeType enum has no Replace value, so the rule is retitled "no unexpected delete". |
| Repo location | Appendix A assumes a standalone `foundry-doctor/` repo | Subdirectory of the `copilot-Web` monorepo; workflows go in repo-root `.github/workflows/` with path filters. |

## Phase 0 outputs the harness expects

`cmd/rulecatalog` (subcommands `validate`, `generate-docs`, `generate-overlap`) exists since Phase 0.
`check-rule-catalog.sh` validates the catalogue and fails if either generated document is stale; it reports SKIPPED only
where the command is absent.

## Source document

The PRD was supplied as a Word file and converted to Markdown for
`docs/requirements/Foundry_Doctor_Implementation_PRD.md`. The conversion flattens code
blocks and diagrams onto single lines; the section text, tables and rule catalogue are intact.
