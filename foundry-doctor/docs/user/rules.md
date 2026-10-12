# Reading rules, findings and skipped checks

Commands and flags are in [cli.md](cli.md). This page explains what the report means.

## Where the rule documentation comes from

The rule catalogue in `rules/catalog/` is the source. Two documents are generated from it and must not be edited by hand:

- [docs/rule-catalog.md](../rule-catalog.md) (rule list, status, phases)
- [docs/overlap-analysis.md](../overlap-analysis.md) (how each rule relates to the Bicep linter, PSRule for Azure, Azure Policy, Defender, Advisor and Checkov)

Both are regenerated with `go run ./cmd/rulecatalog generate-docs` and `generate-overlap`; `scripts/claude/check-rule-catalog.sh`
fails when they are stale. For one rule, run `foundry-doctor explain <rule-id>` (for example `FND-OPS-007`). It prints the
rationale, evidence definition, remediation, severity per profile, compatible azd and extension versions, sources with
verification dates, and what is unverified. `explain` also describes the reserved `FND-SYS-*` IDs and `bicep/<code>`.

## How to read a finding

A finding in the console report has these parts:

| Part | Meaning |
|---|---|
| Rule ID and version | `FND-OPS-007` and its catalogue version. A rule version change gives new fingerprints, so a baseline entry for the old version stops matching ([ADR-008](../decisions/ADR-008-finding-fingerprint.md)). |
| Severity | `info`, `warning` or `error`, taken from the active [profile](profiles.md). Engine diagnostics have a fixed severity. |
| Confidence | How sure the deterministic check is. See below. |
| Resource and location | The logical resource (kind, type, name) and a file. Line and column appear only when the source gives them. For ARM-derived findings there is a JSON pointer instead of a line. |
| Evidence | What the check saw. Key names and paths only: never environment values, secrets or document content. |
| Recommendation, fix, docs | What to change, an example, and a documentation link. |
| Fingerprint | `fp1:` plus 32 hex characters. It identifies the finding for baselines and suppressions. |
| Suppressed or baselined | Shown when an entry in your suppression or baseline file covers the finding. |

Finding states in the report summary: `error`, `warning` and `info` count live findings that are displayed. `suppressed`
and `baselined` count covered findings. `hidden` counts live findings below `--min-severity`: they are not displayed but still
count towards exit 1 if they reach `--fail-on`. `passed` (JSON summary only) counts checks that ran and found nothing.

### Confidence values

| Value | Meaning |
|---|---|
| `certain` | The default: the rule reports a fact it read directly. |
| `likely` | The evidence is derived or incomplete. Compiler-derived locations and checks that cannot see everything (for example a CI definition in a system the doctor does not detect) use this. |
| `uncertain` | The check could not decide and reports a question for you to answer. It is still a finding and counts towards `--fail-on` at its severity. |

Confidence is not severity. A `likely` error still fails the run at `--fail-on error`.

## Skipped is not passed

A skipped check was not evaluated. It says nothing about your project. Every skipped check is listed with a reason, the
capability that was missing, and a detail. The summary prints a count per reason. The reasons you will see:

| Reason | Cause | What to do |
|---|---|---|
| `profile-key-missing:<key>` | The rule needs an organisation fact that `.foundry-doctor/config.yaml` does not give ([ADR-007](../decisions/ADR-007-profile-configuration-keys.md)). | Set the key ([config-reference.md](config-reference.md)). |
| `synthetic-infrastructure` | The project has no template on disk (provider `microsoft.foundry`, or no infrastructure) and the rule needs compiled ARM ([ADR-012](../decisions/ADR-012-arm-rules-under-synthetic-infrastructure.md)). | Nothing in this build. Add a Bicep or ARM template to have the rule run. |
| `tool-missing:bicep` | `validation.bicep` is `optional` and no usable Bicep CLI was found. | Install Bicep 0.24.24 or newer. |
| `tool-missing:psrule`, `tool-missing:checkov` | FND-CFG-005 can merge results from these optional adapters. The adapters do not exist in Phase 1, so this sub-check is always skipped. | None; adapters arrive in a later phase. |
| `control-plane-unavailable` | The check needs live Azure access (deployed resources, role assignments). Phase 1 is offline. | None in this build. |
| `unresolved-value:<why>` | A value the rule needs could not be established, for example `git-tracked-files-unavailable`, `installed-extension-versions-unavailable`, `no-location-source`, or a value that depends on a parameter. | Depends on `<why>`; see [troubleshooting.md](troubleshooting.md). |
| `not-applicable` | The condition for the check is not present, for example only one azd environment is visible, or the profile does not expect a pipeline. | None. |
| `unsupported-version` | A detected azd or extension version is outside the range the rule was verified against. | Check `foundry-doctor explain <rule-id>` for the compatible range. |
| `not-implemented` | The rule is in the pack but this build has no Go implementation for it. | None. |

`--strict` turns any skipped check into exit 3 when nothing else applies. In this build that is almost always: even a clean
project has checks that cannot run offline.

## Rules that report an engine diagnostic

Some findings are about the run, not the catalogue. They use the reserved IDs `FND-SYS-*` and `bicep/<code>`
([ADR-009](../decisions/ADR-009-engine-diagnostics-namespace.md)):

| ID | Severity | Meaning |
|---|---|---|
| `FND-SYS-INVALID-AZURE-YAML` | error | `azure.yaml` failed to parse or violates the schema; dependent rules are skipped. |
| `FND-SYS-SUPPRESSION-EXPIRED` | error | A suppression expired; the finding is visible again. |
| `FND-SYS-SUPPRESSION-INVALID` | error | The suppression file cannot be used. |
| `FND-SYS-UNSUPPORTED-HOST` | warning | A service host the engine does not know. Defined by ADR-009; no code path in this build emits it. |
| `FND-SYS-ADAPTER-UNAVAILABLE` | info | An optional adapter is missing. Defined by ADR-009; no code path in this build emits it (missing optional tools show as skipped checks instead). |
| `FND-SYS-DUPLICATE-FINGERPRINT` | warning | Two distinct findings had the same fingerprint. |
| `bicep/<code>` | from the compiler | A Bicep compiler or linter diagnostic. |

They take part in `--fail-on`, cannot be baselined, and `FND-SYS-SUPPRESSION-*` cannot be suppressed.

## What a clean run means

A run with exit 0 means: every check that ran found nothing at or above `--fail-on`, after your baseline and suppressions.
It does not mean the project is correct, secure, WAF compliant, or that it will deploy. Read the skipped list and the effective
policy before relying on it.
