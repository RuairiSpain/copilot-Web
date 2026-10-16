# ADR-018: Deployment parameter files and module file locations

Status: Accepted
Date: 2026-10-16

## Context

Round-2 and verification reviews found that parameter-driven templates make rules inert (finding M4): an `if (deploySearch)` resource, a `location: location` region
and a `publicNetworkAccess: publicNetworkAccess` property are all `parameters('x')` expressions in the compiled ARM, so SEC/IDN/NET/OPS/REL rules skip as
`conditional-resource` or uncertain. azd supplies those values from `infra/main.parameters.json` (an ARM parameter file whose string values may hold azd
substitutions) or `infra/main.bicepparam`, and from the selected azd environment's `.env`. Review item R5 found that `Finding.Location.File` is always the entry file
because `armmodel.FromARM` was given only the entry. ADR-006 (read-only), ADR-007 (default-derived facts carry less certainty) and ADR-012 (no pass without input) apply.

## Decision

1. **Sources and precedence.** For the entry template `<dir>/<name>.bicep` the doctor looks for exactly two sibling files, `<name>.bicepparam` and `<name>.parameters.json`
   (azd: `infra/main.bicepparam`, `infra/main.parameters.json`). `.bicepparam` takes precedence over `.parameters.json`, as in azd. If the preferred file exists but cannot be
   used, the other file is **not** used: azd would not use it, and the doctor must not model a different deployment. Both are found with `os.Root`; symlinks and directories are ignored.
2. **`.parameters.json`.** Read with a 1 MiB bound and at most 2000 parameters. String values get azd substitution from the selected environment's
   `.azure/<env>/.env` (parsed by `project.ParseDotEnv`): `${VAR}`, `${VAR=default}`, `${VAR:=default}`, `${VAR-default}`, `${VAR:-default}`. A variable that is missing,
   and a plain `${VAR}` that is empty, leaves the parameter **unresolved**. When no environment is selected or its `.env` is unreadable, every substitution is unresolved,
   defaults included, because azd could supply a value the doctor cannot see. Keys that look secret (`secretishKey`) are never read as substitution sources. Objects, arrays,
   `null` and Key Vault `reference` entries are set but unresolved.
3. **`.bicepparam`.** Compiled by the Bicep CLI in use with `bicep build-params <file> --stdout --no-restore` (offline); the `parametersJson` it prints is read like a parameter
   file with no substitution. `readEnvironmentVariable()` is fed from the selected environment's non-secret values through the CLI process environment. If the file mentions
   `readEnvironmentVariable` and no environment is readable, the file is skipped (`bicepparam-needs-environment`) instead of accepting the default the CLI would silently use.
   There is no Bicep parser: the check is a substring test, the compile is the CLI's.
4. **Injection.** `armmodel.Options.Parameters` carries the values of the ROOT template. Before module bindings are substituted, `parameters('x')` of a non-secure `string`, `int`
   or `bool` parameter is replaced by the value, and expressions that become fully literal are evaluated by `armexpr.Eval` (pure functions only: `not`, `and`, `or`, `equals`, `if`,
   `concat`, `format`, `toLower`, `toUpper`, `empty`; `equals` on strings is case-insensitive). Values are type-checked and coerced (`"true"` for `bool`); a mismatch, or a string that
   begins with `[`, is not injected. **Secure parameters are never substituted**, from a file or a default.
5. **Defaults.** Without a parameter file nothing changes: defaults are not facts. With a parameter file that does not set a parameter, a **literal** Bicep default (string, int, bool)
   is applied and marked default-derived; a default that is an expression (`resourceGroup().location`) stays unresolved. The model records the parameter names that shaped each resource:
   `Placement.DefaultParams` and `Model.FromDefault(resource)`. A set-but-unresolved file value is not replaced by the default. Rules and findings read the provenance through the optional
   `FromDefault` method; lowering a finding's confidence and saying "from the default of parameter X" in evidence is the rule layer's work (ADR-007).
6. **Failure is named, never a panic or a silent drop.** A malformed, oversize or unreadable file, a failed `build-params`, or a `.bicepparam` beside an ARM JSON entry (no Bicep CLI) produces a
   warning on stderr naming the file and one reason: `parameter-file-malformed`, `parameter-file-too-large`, `parameter-file-too-many-parameters`, `parameter-file-unreadable`,
   `bicepparam-compile-failed`, `bicepparam-needs-environment`. Parameter-driven checks then stay unresolved exactly as before. Parameters that reference azd variables that could not be
   resolved are named in a warning.
7. **Read-only guarantee.** The loader reads files under the project root and runs `bicep build` / `bicep build-params` offline (`--no-restore`). It makes no Azure call and no azd call, and
   hands the Bicep process only non-secret `.env` values.
8. **Variables and tags.** `Model.Variable(name)` exposes the root template's compiled variables (literal, or an expression with the parameter values and other variables substituted;
   not resolvable means `ok == false`). `Model.TagsAreExpression(resource)` tells an expression-valued `tags` property (`tags: tags`) from absent tags; `Resource.Tags` is unchanged.
   Variable retention is bounded (512 variables, 8 KiB each).
9. **Module file locations.** The compiled ARM carries no source path, but a module compiled alone has the `templateHash` of the template nested in its caller. The loader lists the `.bicep`
   files under the entry's directory (bounded), compiles each alone with `bicep build --no-restore`, and maps `templateHash` to the repo-relative file (`Options.ModuleHashes`), so
   `Finding.Location.File` is the module file. Bounds: 64 files compiled, 512 KiB per file, 1000 files listed, 3 minutes. Two files with the same hash are ambiguous and not mapped. Modules outside the
   entry's directory, registry modules and files that do not compile alone keep the entry file with the existing `uncertain` description. Each limit produces a named warning.
10. **Truncation.** `Model.Truncated()` (resource or expression-text bound reached) is reported as the warning `model-truncated`; absent resources are then unknown.

## Consequences

- With a parameter file, parameter-driven rules evaluate instead of skipping. Facts that rest on a default are marked, never silently equal to facts from the file.
- Parameters supplied only by `azd` prompts or `azd env set` values that are not in the `.env` stay unresolved.
- Module location costs up to 64 extra offline Bicep compiles per run; it stops as soon as every nested template is matched.
- The 1 MiB and 2000-parameter bounds are engineering limits, not Azure limits.

## Not decided here

- Multi-layer infrastructure (`infra.layers`) still has no parameter file handling.
- Parameter files of nested modules are not read (Bicep modules take no parameter files).

## Amendment 1 (2026-10-16): confidence of default-derived findings

The engine lowers the confidence of a finding to `likely` (medium) when the ARM model reports through `FromDefault` that the finding's resource took a fact from a parameter's Bicep default. The cap is applied in `internal/rules/rules.go` (`capDefaultDerived`) before the catalogue clamp, so it can only lower confidence. The evidence text and the fingerprint are not changed, so adding a parameter file does not re-key existing baseline entries. A finding whose rule already set a lower confidence keeps it.
