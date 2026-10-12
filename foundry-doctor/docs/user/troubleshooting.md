# Troubleshooting

Exit codes are defined in [cli.md](cli.md#exit-codes). This page lists causes and fixes. Messages below were produced by running
the built binary on copies of the fixtures in `test/fixtures/project/`, unless marked otherwise. Errors go to stderr, prefixed
`error:`. Add `--debug` to print the profile, effective policy, tool states and the Bicep command (without file names) to
stderr. `--debug` never prints environment values.

## Exit 1: findings

The run found at least one unsuppressed, non-baselined finding at or above `--fail-on` (default `error`). Note:

- `--min-severity` only hides findings from the report. A finding hidden by it still counts towards exit 1. The summary shows
  the count as `Hidden by --min-severity`.
- Engine diagnostics count. `FND-SYS-SUPPRESSION-EXPIRED`, `FND-SYS-SUPPRESSION-INVALID` and `FND-SYS-INVALID-AZURE-YAML` are
  errors in every profile.

## Exit 2: the run could not start or finish

| Message (start) | Cause | Fix |
|---|---|---|
| `find the project: no azure.yaml found` | No `azure.yaml` or `azure.yml` in the current directory or any parent (at most 128 levels). | Run from the project folder. |
| `read azure.yaml: ...` | The file exists but cannot be read: a symbolic link that leaves the project, not a regular file, or larger than 4 MiB. | Fix the file. A file that reads but does not parse is not exit 2: it is the finding `FND-SYS-INVALID-AZURE-YAML` (exit 1). |
| `config <file>:<line>: <key>: unknown key "<key>"; allowed keys here: ...` | The config file has a key that is not in the closed schema, a duplicate key, a wrong type or a bad value. | Fix the named line. All problems are listed together. See [config-reference.md](config-reference.md). |
| `config: unknown profile "staging": want dev, test, ...` | `--profile` or `profile:` is not one of the six accepted names. | Use `dev`, `test`, `prod` or the long form. |
| `--fail-on: unknown severity "bad": want info, warning or error` | Bad value. The same applies to `--min-severity`. | Use `info`, `warning` or `error`. |
| `--rules: ...` or `select rules: rule selector "..." matches no catalogue rule` | A selector is malformed or matches no rule. | See the grammar in [config-reference.md](config-reference.md#rules). |
| `--preflight: not available in this version` (also `--runtime`, `--security`, `--what-if`, `--export-tool-config`, `--llm-explain`) | The flag exists in the help but is not implemented in Phase 1. | Remove it. |
| `unknown flag: --output` | The flag is `--out`, not `--output` ([ADR-007](../decisions/ADR-007-profile-configuration-keys.md)). | Use `--out`. |
| `environment "<name>": ...` | You named an environment with `--environment` and `.azure/<name>/.env` cannot be read. | Create it with `azd env new`, or drop the flag. (When the environment comes from the config file or from azd's default, the run continues and the rules that need it are skipped; a note appears with `--debug`.) |
| `the Bicep CLI is required (validation.bicep: required) but is missing: bicep was not found on PATH` (the state can also be `unsupported-version` or `failed`) | The project has `.bicep` templates, `validation.bicep` is `required` (the default), and the CLI is missing, older than 0.24.24, not executable, or `bicep --version` failed. The report is still written and shows the tool state. | Install Bicep 0.24.24 or newer on `PATH`; or set `validation.bicep: optional` (ARM rules are then skipped with `tool-missing:bicep`); or `disabled`. |
| `compile <file>: ... timed out` / `input file is not allowed` | The compiler ran longer than 60 seconds, or the entry file is not a `.bicep`/`.bicepparam` file inside the project (for example a symbolic link that leaves it). | Simplify the template, or fix the link. |
| `baseline <file>: ...` | The baseline file named by `--baseline` does not exist, or any baseline file is invalid, too large (4 MiB), a symlink, outside the project, or has an unknown version or fingerprint scheme. | Regenerate it with `--write-baseline`, or fix the entry named in the message. |
| `suppressions <file>: ...` | The file named by `--suppressions` does not exist. (An existing but invalid file is not exit 2; see below.) | Create it or drop the flag. |
| `write the report: unsafe output path: ...` | `--out` leaves the project directory, or a path component is a symbolic link, or the parent of an absolute path does not exist. | Use a path inside the project, or an absolute path whose parent exists. |
| `--out writes one report: give exactly one --format` | `--out` was given and the run has more than one output format (formats from the config file count). | Give exactly one `--format`. |
| `the run was interrupted` | The process received an interrupt before all rules ran. | Re-run. |

A usage error also prints `Run 'foundry-doctor --help' for usage`.

An invalid suppressions file is not exit 2. It gives the error finding `FND-SYS-SUPPRESSION-INVALID` and no suppression
from that file is applied, so the exit code is 1.

## Exit 3: skipped checks with `--strict`

Exit 3 means: no finding reached `--fail-on`, no error stopped the run, and at least one check was skipped. Skipped checks
include `not-applicable` ones. In this build, the fixture projects all had skips that you cannot remove:

- FND-CFG-005 skips its check of whether `.env` is tracked by git (`unresolved-value:git-tracked-files-unavailable`), because the
  doctor runs no git process. This appeared in every fixture run.
- FND-CFG-011 skips (`installed-extension-versions-unavailable`) in every fixture run.
- FND-SEC and FND-IDN rules skip with `control-plane-unavailable` or `synthetic-infrastructure`.
- FND-CFG-005 skipped its PSRule and Checkov sub-checks (`tool-missing:psrule`, `tool-missing:checkov`) in the Bicep and ARM JSON fixtures.

Run the command with `--format json` and read `skippedByReason` to see them. In the fixture
`synthetic-foundry`, `--strict` gave exit 3. Use `--strict` only when you accept this, or leave it off and review the skipped
list instead. Do not read a clean exit 0 without `--strict` as "everything was checked".

## Exit 4: internal error

- A rule returned an error or panicked. The message is printed with `error:` (redacted), the report is still written, and the
  exit code is 4. This is a bug: report it ([SECURITY.md](../../SECURITY.md) for security-relevant cases, otherwise an issue) with
  the rule ID and, if you can, a minimal `azure.yaml`.
- The Bicep CLI exited non-zero without printing a diagnostic, or printed output that is not JSON.
- The compiled template could not be normalised, or the rule catalogue or profiles failed to load.

Exit 4 wins over every other exit code.

## Fewer findings than expected

1. Read the **Skipped checks** list first. A skipped rule produced no finding.
2. Check the **Effective policy** header for `unset` keys. Rules that need an organisation fact are skipped until you set it.
3. Check `Tools:` for `bicep`. With `validation.bicep: optional` and no CLI, every ARM rule is skipped.
4. Check whether a baseline or suppression covers the finding (`baselined` / `suppressed` in the summary). The default files
   `.foundry-doctor/baseline.yaml` and `.foundry-doctor/suppressions.yaml` are read without any flag.
5. For an `azure.yaml`-only Foundry project, ARM rules are skipped by design (`synthetic-infrastructure`).

## Findings you do not expect to be new

- A rule version change produces new fingerprints, so a baseline entry for the old version stops matching.
- Renaming a resource changes its fingerprint.
- An expired suppression shows the finding again.

See [baseline-and-suppressions.md](baseline-and-suppressions.md).

## Windows and paths

Reports use forward slashes. Path rules (no `..`, no drive letters, no reserved device names such as `CON` or `NUL`) apply on all
systems. The CI workflow is set up to run the tests on Windows, but it has not run on GitHub yet
([docs/development/ci.md](../development/ci.md)), and the tests were not run on Windows for this documentation.
