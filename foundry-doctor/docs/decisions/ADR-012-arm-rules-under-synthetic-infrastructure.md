# ADR-012: ARM-dependent rules under synthetic infrastructure

Status: Accepted
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
