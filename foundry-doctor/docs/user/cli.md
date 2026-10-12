# foundry-doctor command line

The standalone binary is read-only and offline. Run it with the project as the current directory (the nearest
parent holding `azure.yaml` is used). It reads `azure.yaml`, Bicep (compiled with the Bicep CLI) and
`.azure/<env>/.env`; it never calls Azure.

## Commands

| Command | Purpose |
|---|---|
| `foundry-doctor doctor` | Check the project and write a report. |
| `foundry-doctor explain <rule-id>` | Print the catalogue entry of a rule (`--format console\|markdown`). Also `FND-SYS-*` and `bicep/<code>`. |
| `foundry-doctor compare <left-env> <right-env>` | List key names that differ between two azd environments (`--format console\|json\|markdown`). Values and value hashes are never printed. |
| `foundry-doctor version`, `--version` | Print the version. |

Global flags: `--debug` (effective policy, tool versions and the redacted adapter command, on stderr), `--no-color`
(`NO_COLOR` is respected; colour is used only on a terminal).

## doctor flags

`--profile dev|test|prod` (default `dev`), `--environment`, `--config` (default `.foundry-doctor/config.yaml`),
`--rules <selector>` (repeatable; replaces the configured selection), `--min-severity` (display filter, default `info`),
`--fail-on` (exit 1 threshold, default `error`), `--baseline`, `--suppressions`, `--write-baseline <file>`, `--strict`,
`--format console|json|markdown|sarif` (repeatable), `--out <file>`, `--local` (the default).

`--preflight`, `--runtime`, `--security`, `--what-if`, `--export-tool-config` and `--llm-explain` are listed in the
help but exit 2 with `not available in this version`. The flag is `--out`, not `--output`.

Relative paths for `--baseline`, `--suppressions` and `--write-baseline` are relative to the project root. `--out` is
relative to the working directory and must stay inside the project; an absolute path is accepted when its parent
directory exists. Symbolic links on the path are refused. `--out` takes exactly one format. With several formats
and no `--out`, `console` goes to stdout and the others to `outputs.directory` (default `.foundry-doctor/out`).

## Exit codes

| Code | Meaning |
|---|---|
| 0 | No unsuppressed, non-baselined finding at or above `--fail-on`. |
| 1 | At least one such finding. |
| 2 | Cannot run: no project, bad flag or config, a required tool (Bicep with `validation.bicep: required`) is missing. |
| 3 | A check was skipped and `--strict` was given (only when nothing else applies). |
| 4 | Internal error (a rule panicked or returned an error, Bicep protocol failure). |

Precedence when several apply: 4, 2, 1, 3, 0 (ADR-010).

## Skipped is not passed

Skipped checks are listed with a reason and the missing capability. Reasons include `tool-missing:bicep`
(`validation.bicep: optional` and no usable CLI), `synthetic-infrastructure`, `profile-key-missing:<key>` and
`not-implemented` (a catalogued rule this build cannot run yet).

## Baseline and suppressions

`--write-baseline <file>` writes a baseline of the current findings and accepts them for that run. A baseline hides
only findings with an exact fingerprint match; a new finding gives exit 1. A suppression needs a reason, an owner and
an expiry; after its expiry the finding is visible again and `FND-SYS-SUPPRESSION-EXPIRED` is reported. An invalid
suppression file reports `FND-SYS-SUPPRESSION-INVALID`.
