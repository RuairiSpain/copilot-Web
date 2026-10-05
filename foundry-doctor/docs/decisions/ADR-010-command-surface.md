# ADR-010: Command-surface decisions for Phase 1

Status: Accepted
Date: 2026-10-05

## Context

PRD section 4 lists flags for Phase 2 to Phase 5 features and PRD section 9 builds only the offline MVP. The PRD also leaves several things open or inconsistent.
Each is recorded here instead of being chosen silently.

## Decisions

1. **`--min-severity` filters display; `--fail-on` decides exit 1.** `--min-severity` (default `info`) hides findings below the level in console, Markdown and
   JSON `findings`; the JSON summary still counts hidden findings in a `hidden` figure so nothing disappears silently. SARIF applies the same filter.
   `--fail-on` (default `error`) sets the threshold for exit 1 and is evaluated on all unsuppressed, non-baselined findings, whatever `--min-severity` shows.
   PRD section 7 says "at or above --fail-on" and does not mention `--min-severity`; this keeps the two independent.
2. **Exit-code precedence** when several apply: 4, then 2, then 1, then 3, then 0. PRD section 7 does not order them.
3. **Only `--local` is accepted in Phase 1.** `--local` is the default and may be given explicitly. `--preflight`, `--runtime`, `--security`, `--what-if`,
   `--export-tool-config` and `--llm-explain` are registered so the help text is stable, but using any of them exits 2 with
   `not available in this version`. The message never suggests the feature works. (PRD section 4 shows them; PRD section 9 excludes them.)
4. **Package name `internal/project`, not `internal/source`.** PRD section 5 names `internal/source`; PRD section 9 names `internal/project`. They are the same job
   (discovery and source loading). Phase 1 uses `internal/project`, the later and more specific table. Record: PRD section 5 is out of date.
5. **Config file `.foundry-doctor/config.yaml`.** The PRD shows the config content (section 6) and puts baseline, suppressions and outputs under `.foundry-doctor/`, but
   never names the config file. Using the same directory keeps one hidden folder to ignore or commit; `config.yaml` beside `baseline.yaml` and `suppressions.yaml` is predictable.
   Alternative `foundry-doctor.yaml` at the root was rejected because it adds a root-level file to every repository. `--config <path>` overrides it. Absence of the file is normal.
6. **Profile names.** User-facing `--profile dev|test|prod`; internal and config names are `foundry-dev|foundry-test|foundry-prod` (PRD section 6 example `profile: foundry-prod`).
   The `profile:` key and `--profile` accept either form; the engine normalises to the long form, and reports print the long form. Any other value exits 2.
   The default when neither is given is `foundry-dev`, the least disruptive for adoption; the PRD names no default (owner to confirm).
7. **Per-environment layout** is `environments.<name>.policy` in the same file, as ADR-007 decision 4. `<name>` is an azd environment name. Only `policy` is allowed there in Phase 1.
8. **Output path flag** is `--out`, as ADR-007. PRD `cmd/foundry` is `cmd/foundry-doctor` (CLAUDE.md layout, ADR-003).

## PRD inconsistencies recorded

| PRD place | Inconsistency | Resolution |
|---|---|---|
| Section 5 vs 9 | `internal/source` vs `internal/project` | `internal/project` |
| Section 5 vs 9 | `cmd/foundry` vs `cmd/foundry-doctor` | `cmd/foundry-doctor` |
| Section 4 vs 9 | Flags for later phases listed in the doctor command | Rejected with exit 2 |
| Section 4 | `--output` | `--out` (ADR-007) |
| Section 7 | No exit-code precedence; `--min-severity` and `--fail-on` relation unstated | Decisions 1 and 2 |
| Section 5 vs 7 | `internal/findings` owns the model; Finding is public in `pkg/sdk` | `pkg/sdk` holds the types (ADR-011); `internal/findings` holds fingerprints and redaction |

## Consequences

- CLI contract tests cover each rejected flag and the precedence order.
- Later phases enable the rejected flags without renaming anything.
