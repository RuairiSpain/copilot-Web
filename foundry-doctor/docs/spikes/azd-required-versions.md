# azd `requiredVersions`: grammar and semantics

Verified 2026-10-16 from the azd source at commit `1bd2ca4699c7b1f371db85dd867f18aa4fbc4730` (main) and from the two semver libraries it uses. The source was read, not executed. Paths are relative to `cli/azd/` in https://github.com/Azure/azure-dev.

## Two parsers

| Key | Parser | Library source |
|---|---|---|
| `requiredVersions.azd` | `semver.ParseRange` from `github.com/blang/semver/v4 v4.0.0` | https://raw.githubusercontent.com/blang/semver/v4.0.0/v4/range.go |
| `requiredVersions.extensions.<id>` | `semver.NewConstraint` from `github.com/Masterminds/semver/v3 v3.4.0` | https://raw.githubusercontent.com/Masterminds/semver/v3.4.0/constraints.go |

`pkg/project/project_config.go:50-55` defines `RequiredVersions{ Azd *string; Extensions map[string]*string }`; a nil value means no constraint. The schema (https://raw.githubusercontent.com/Azure/azure-dev/main/schemas/v1.0/azure.yaml.json, lines 1136-1165) has `additionalProperties: false` and `type: string` with no pattern for both values, so the schema alone cannot tell which syntax is valid for which key.

## `requiredVersions.azd` (blang `ParseRange`)

| Form | Accepted |
|---|---|
| `<`, `<=`, `>`, `>=`, `=`, `==`, `!`, `!=`, or no operator (equality) | yes |
| Space between operator and version (`>= 0.6.0-beta.3`, the schema example) | yes |
| Space-separated constraints (`>=1.0.0 <2.0.0`): AND | yes |
| `\|\|` between constraints: OR (AND binds tighter; no parentheses) | yes |
| Prerelease in the constraint (`>=0.6.0-beta.3`); comparison is plain semver precedence, so `>=1.0.0` rejects `1.0.0-beta.1` | yes |
| Wildcard `x` (`1.x`, `1.2.x`), lowercase only | yes |
| Comma separator (`>=1.0.0, <2.0.0`) | no (inferred from `splitAndTrim`) |
| `~`, `~>`, `^`, `=>`, `=<`, `*`, `X`, hyphen range, `latest`, empty string | no |
| Leading or trailing `\|\|` | error |

## `requiredVersions.extensions` (Masterminds `NewConstraint`)

| Form | Accepted |
|---|---|
| `=`, `!=`, `>`, `<`, `>=`, `=>`, `<=`, `=<`, `~`, `~>`, `^`, or no operator | yes |
| Space-separated or comma-separated constraints: AND | yes |
| `\|\|`: OR | yes |
| Hyphen range, `x`, `X`, `*` wildcards | yes |
| `latest` or empty: any version (`pkg/extensions/manager.go:297-300`) | yes |
| A string that is not a valid constraint | falls back to an exact, case-insensitive tag match (`manager.go:302-310`) |
| Prerelease candidates | rejected unless the constraint (in that `\|\|` group) itself has a prerelease (`constraints.go`) |

## Behaviour

- `azd` out of range: `pkg/project/project.go:63-77`, inside `project.Load`. Malformed range: `"<range> is not a valid semver range (for requiredVersions.azd): <err>"`. Out of range: `"this project requires a version of azd within the range '<range>', but you have '<version>'. ..."`. The project fails to load; the process exit code is 1 (`main.go`, inferred). The check is skipped for a local dev build (`internal.IsDevVersion()`).
- Extensions: project commands (`up`, `provision`, `deploy`, `package`, `restore`, `down`, `env refresh`, `pipeline config`) go through `missingProjectExtensions` (`cmd/project_extension_auto_install.go`). An installed extension whose version does not satisfy the constraint is a hard error; a missing extension is looked up in the configured sources and queued for auto-install with a prompt, or errors with `required extension <id> not found`.
- azd's own version is set at build time (`internal.Version`, `<semver> (commit <sha>)`). A tool that does not run azd cannot decide whether `requiredVersions.azd` is satisfied, and cannot decide the extension constraints (installed versions live in azd's user config and registry).

## Consequence for Foundry Doctor

A static check can validate that the `azd` range parses with blang rules and that each `extensions` value parses with Masterminds rules, or is `latest`, or falls back to an exact tag. It can decide that the `azd` range is unsatisfied only when the azd version is known (`--azd-version`, `FOUNDRY_DOCTOR_AZD_VERSION`, or a probe). The extension constraints are never decidable offline.

## Not verified

azd and the libraries were not executed (exit code, empty-string error and comma rejection are inferred from source). Microsoft Learn pages were not fetched. `main` as of 2026-10-16; older azd releases may differ. Non-interactive (`--no-prompt`) extension auto-install behaviour was not read.
