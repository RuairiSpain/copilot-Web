# ADR-010: Command-surface decisions for Phase 1

Status: Accepted, amended 2026-10-16 (Amendments 1 to 3)
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

9. **`--strict` counts only skips that hid a check.** Exit 3 applies when at least one skip is classified `SkipHidesCheck` by `rules.ClassifySkip`
   (`internal/rules/skipclass.go`, the single classification). Hiding: `input-unavailable` (missing input or capability), `unsupported-version`, `azd-version-unknown`,
   `rule-error`, `invalid-catalogue-metadata`, `unsupported-profile`, and any reason not listed below (fail safe). By design, not hiding: `not-implemented`, `not-applicable`,
   `synthetic-infrastructure`, `profile-key-missing:*`, a small list of rule-specific "nothing to check" reasons, and `input-unavailable` details that state the project has no such
   resource (listed in the source with tests). Every skip is still printed in every report. A required skip (explicit `--rules` ID) still exits 2. A new skip reason must be added to
   the source list in the same change as the rule that emits it; rule packages should use the `not-applicable` head for "nothing to check".
10. **azd version supply.** `doctor --azd-version <semver>` and the environment variable `FOUNDRY_DOCTOR_AZD_VERSION` give the azd version when the `azd` binary is not on PATH
    (CI without azd, Bicep-only checks). Precedence: flag, environment variable, `azd version --output json`. A value that is not a semantic version exits 2. The value is trusted as stated;
    it is not verified. When no source yields a version, rules with an azd range skip as `azd-version-unknown` and the report header says `versions not detected: azd`.
    This adds one flag to `doctor`; it does not enable any rejected later-phase flag.
11. **`validation.bicep: required` is enforced.** `test` and `prod` curated profiles default to `required` (`internal/config/profiles.go`). When it is `required`, a Bicep CLI that is
    missing or unsupported, or a compile failure or unreadable template on a project that has Bicep or ARM input, ends the run with exit 2 and an error. When it is `optional` or
    `disabled`, the same condition is a stderr warning and Bicep-dependent rules skip. A project with no Bicep or ARM input (synthetic infrastructure) is never an error. `annotate`
    and the other commands keep the warning behaviour.

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

## Amendment 1 (2026-10-15): open items

- Decision 2 orders exit codes 4, 2, 1, 3, 0. `internal/report.ClassifyExit` returns 3 before 1 (strict skip beats findings) and `internal/app` tests assert that. The code and this ADR
  disagree; the owner of `internal/report` must pick one. This amendment does not change the behaviour.
- Decision 6 says the default profile is `foundry-dev`; `config.DefaultProfile` is `foundry-prod`. Also unresolved.

## Amendment 2 (2026-10-16): by-design skip classification, round-2 review R9 and R10

- `profile-key-missing:network.cloud` (NET-003) stays by design. NET-003 needs the cloud (public, usgov or china) to choose the private DNS zone table; when the
  profile sets no `network.cloud` and the regions are not literal, the rule is hidden completely for that run, and `--strict` does not count it. Whether a private endpoint is in
  scope is decided inside the rule, not by `ClassifySkip`, which sees only the reason text; the classifier therefore cannot tell the two cases apart, and the reason text already
  names the key to set. The skip is still printed in every report.
- R10: no reason was added. The reasons the review names mean a required input is missing, not that there is nothing to evaluate, so they remain hiding (fail safe):
  COST-001 `budget-managed-outside-repository` (the catalogue text says a budget may exist outside the repository), SEC-008 and SEC-009 in Bicep-only mode
  (`input-unavailable`: subscription Defender pricing state and policy assignments live outside the template), REL-004 `uncertain: no lock resources are declared...`
  (locks can be applied later or at parent scope), and the IQ reasons `knowledge-source-kind-without-backing-index-input`, `index-metadata-not-readable` and
  `knowledge-base-metadata-not-readable`. The "nothing to evaluate" reasons the rules emit today are already by design: `no-split-skill-in-project; no-explicit-query-definitions`
  (IQ), `project-has-no-agent-services`, `no-resilience-objective-declared`, `no-access-requirement-declared`, `no-emit-token-metric-policy`, `schedule-null`,
  `no-supported-fixed-capacity-resources` and the `not-applicable` head. A clean Foundry project under `--strict` can therefore still exit 3 while such inputs are missing; that is
  intended, and the fix is to supply the input (a Bicep build, live reads), not to reclassify the skip.
- R6: CFG-010 with no ARM model emits `input-unavailable`, because the deployments may be declared in Bicep that could not be read; `not-applicable` is emitted only when the ARM
  model is present and declares no deployment.

## Amendment 3 (2026-10-16): per-resource skips next to findings (round-2 review M3)

- A rule that returns findings may also return `Result.Skips` (ADR-008 amendment 3). The engine adds each entry to the skipped list. Every reporter (console, JSON, Markdown, SARIF) already prints
  the whole skipped list with the rule ID, so a partial skip appears in the "Skipped checks" section next to the rule's findings; no reporter field was added.
- Decision 9 (`--strict`) is unchanged: a partial skip is classified by its reason head like any other skip. A rule with findings and an `unresolved-expression` or `conditional-resource` partial skip
  therefore makes `--strict` exit 3, because a resource could not be checked. Findings alone still exit 1 first only when `ClassifyExit` says so (see Amendment 1 on the 3-before-1 order).
- Readiness (`BuildReadiness`) is unchanged: a rule with an active finding is `blocked` whatever its partial skips.
