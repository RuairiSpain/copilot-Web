# Phase execution

Run a phase with the `implement-prd-phase` skill: `/implement-prd-phase 0`.

Order: 0, then 1. After 1, phases 2, 4, 5 and 6 can proceed; 3 needs 2; 7 and 8 need 2 and 3
(see PRD section 21). Each phase starts from a merged previous phase and its hand-off.

Branch per phase: `claude/foundry-doctor-phase-<n>`. Draft PR per phase.

## Gates

`foundry-doctor/scripts/claude/verify-phase.sh [--strict] [--phase0]`.

| Exit | Meaning |
|---|---|
| 0 | Every runnable gate passed; in non-strict mode, skips may still be present. |
| 1 | At least one gate failed. |
| 2 | Invalid command-line usage. |
| 3 | At least one gate was skipped and `--strict` was given. |
| 4 | The harness could not enter the Foundry Doctor repository root. |

`--phase0` changes only the absent `cmd/foundry-doctor` build gate from `SKIPPED` to the explicit
Phase 0 `EXEMPT` state. It does not waive any other skipped or failed gate. Skipped gates are listed
in the output and must be listed in the hand-off. Use `--strict` for the final run of a phase that
has Go code. A non-strict exit 0 means only that every *runnable* gate passed; it is not final
acceptance when anything was skipped. Phase 0 remains reopened under ADR-008.

The current local validation record ran the individual formatting, diff, module, test, vet,
static-analysis, vulnerability, build, catalogue/document, dependency-audit, Python-unit-test and
secret-scan commands successfully. `go test -race ./...` could not run because the Go race detector
is unsupported on Windows ARM64. Tagged spike commands were blocked by the repository preTool hook.
No CI run was performed or evidenced.

## Dependency and licence audit

Run `python3 scripts/dependency_audit.py --check-inventory` from `foundry-doctor/`. It requires
Python 3, the Go toolchain, read access to `docs/licence-inventory.md`, and either a populated module
cache or network access to download every selected module. It exits 0 when every selected non-main
module has an immutable version, an approved recognised root licence, and an exactly matching
inventory marker; 1 when the audited graph and inventory differ; and 2 for invalid usage, tool or
filesystem errors, download/metadata failures, duplicate markers, missing immutable versions, or
missing/unknown/unapproved licences. The audit is fail-closed but does not assemble release NOTICE
files or prove CI passed.

## PRD inconsistencies and the choice made

Record changes as ADRs in `docs/decisions/` when Phase 0 confirms or reverses them.

| Topic | PRD says | Used for now |
|---|---|---|
| Entry-point directory | Section 5: `cmd/foundry`; Appendix A: `cmd/foundry-doctor` | `cmd/foundry-doctor` (matches the binary name and Appendix A). |
| Catalogue doc name | `docs/rule-catalog.md` (US spelling) | PRD spelling `rule-catalog`. |
| Public command prefix | `azd foundry`, but Phase 0 must confirm no conflict | Keep the prefix in one place (command layer) until ADR-003. |
| Output path and comparison | Original command surface conflicted with azd's output flag and also described comparison as a doctor mode | The azd extension SDK reserves `-o/--output` and `-e/--environment` and refuses to start if they are reused. Decided 2026-10-05: use `--out <path>` everywhere and a separate `compare` command; see ADR-003 and ADR-007. |
| `azd ai agent doctor` | Not mentioned | Microsoft's own `azd ai agent doctor` exists (read-only, overlaps FND-RUN-001, 003, 005). Not a name conflict; documented in ADR-003 and the RUN overlap notes. |
| What-if titles | FND-DEP-008 "delete/replace" | The what-if ChangeType enum has no Replace value, so the rule is retitled "no unexpected delete". |
| Repo location | Appendix A assumes a standalone `foundry-doctor/` repo | Subdirectory of the `copilot-Web` monorepo; workflows go in repo-root `.github/workflows/` with path filters. |
| Exit 1 / 2 | 1 findings; 2 requested validation could not run | Preserve these meanings in both hosts; translate external-tool codes at adapter boundaries. |

## Phase 0 outputs the harness expects

`cmd/rulecatalog` (subcommands `validate`, `generate-docs`, `generate-overlap`) exists since Phase 0.
`check-rule-catalog.sh` validates the catalogue and fails if either generated document is stale; it reports SKIPPED only
where the command is absent.

## Source document

The PRD was supplied as a Word file and converted to Markdown for
`docs/requirements/Foundry_Doctor_Implementation_PRD.md`. The conversion flattens code
blocks and diagrams onto single lines; the section text, tables and rule catalogue are intact.
