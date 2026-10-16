# ADR-012: ARM-dependent rules under synthetic infrastructure

Status: Accepted, amended 2026-10-15 (Amendment 1)
Date: 2026-10-05

## Context

ADR-004 found that azure.yaml-only Foundry projects have no template on disk: the provisioning provider synthesises it. Phase 1 reads no provider internals and does not
compile the extension's embedded template (a later spike item). Rules that read ARM properties (network ACLs, RBAC assignments, diagnostic settings, SKUs, API versions)
therefore have no input in `iac: synthetic` projects.

## Decision

1. A rule that needs `Input.ARM` and finds `Project.IaC == synthetic` returns a skipped result with reason `synthetic-infrastructure` and
   `MissingCapability: "compiled ARM template"`. The same applies to `iac: none`, with the same reason text distinguishing it in `Detail`.
2. These rules are **never reported as passed**, never as uncertain, and never silently dropped. Each appears in `Report.Skipped`.
3. The report summary lists them: `Summary.SkippedByReason["synthetic-infrastructure"]` holds the count, and the console and Markdown reporters print a line such as
   "N checks need compiled infrastructure and were skipped" followed by the rule IDs. `--strict` turns any such skip into exit 3 (PRD section 7).
4. The engine, not each rule, applies the shortcut: the catalogue `inputs` field lists `bicep-arm`; a rule with that input and no ARM available is skipped by the engine with this reason before `Evaluate` runs. A rule that can run partly on azure.yaml alone may still run and skip only its ARM sub-checks, using the same reason and a `Key` for the sub-check.
5. Rules about azure.yaml, references, environment key names and cross-service wiring run normally in synthetic projects (ADR-004 decision 3).

## Consequences

- Default Foundry projects show many skipped SEC and NET checks. This is correct and visible, not a failure of the tool.
- When a later phase compiles the synthesised template or reads deployed state, the same rules start running without a change to their code.
- Reporter golden tests include a synthetic-project report with the summary line.

## Amendment 1 (2026-10-15): implementation and `--strict`

Review findings Azure N13 and Foundry N19 found that the engine shortcut and the summary were not implemented. They are now:

1. **Engine shortcut.** `rules.Engine.Synthetic` is set by `internal/app` when the Bicep/ARM loader reports `ErrNoInfrastructure` (neither `<infra.path>/main.bicep` nor `main.json`
   exists and `infra.layers` is not used). A rule is skipped before `Evaluate` with reason `synthetic-infrastructure` when `Input.ARM` is nil and `rules.NeedsARMOnly` holds: the catalogue
   `inputs` list contains `bicep-arm` and neither `azure.yaml` nor `azd-env`. Rules that also list `azure.yaml` or `azd-env` run and skip their ARM sub-checks themselves (decision 4, last sentence).
   `sdk.Skip.MissingCapability` carries `compiled ARM template`. `iac: none` and `iac: synthetic` use the same reason (decision 1); `Detail` is not implemented.
   A missing Bicep CLI or a compile failure on a project that does have Bicep is not synthetic: those rules skip themselves as `input-unavailable`, or the run exits 2 when `validation.bicep` is `required` (ADR-010 decision 11).
2. **Summary.** `rules.Report.SkippedByReason` counts skips by reason head. The console and Markdown reports print the line
   `N checks need compiled infrastructure and were skipped (synthetic-infrastructure): <rule IDs>` before the report (`app.SkipSummaryNotes`). The JSON and SARIF reports list every
   skip with its reason; a `Summary.SkippedByReason` field in the JSON summary is **not** implemented in Phase 1 (it needs a change in `internal/report`).
3. **`--strict` supersedes decision 3, last sentence.** A `synthetic-infrastructure` skip is by design: the check could not have run on any input the user can provide, and
   Foundry-only projects without `infra/` are first-class (ADR-004). It does not trigger exit 3. The skip stays in every report. See ADR-010 decision 9.

## Amendment 2 (2026-10-16): compile diagnostics do not change the synthetic decision

A project whose Bicep compiles with warnings is not synthetic and not skipped: the compiler warnings are reported as `bicep/<code>` findings (ADR-009 Amendment 1) and ARM rules run. A project whose Bicep fails to compile
is not synthetic either: ARM-dependent rules skip as `input-unavailable` (or the run exits 2 when `validation.bicep` is `required`), and the compiler's error diagnostics are reported as error findings so the cause is visible in the report.
