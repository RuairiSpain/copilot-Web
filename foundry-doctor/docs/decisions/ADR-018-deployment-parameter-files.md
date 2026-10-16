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
   `concat`, `format`, `toLower`, `toUpper`, `empty`; `equals` and `contains` on strings are case-sensitive, as the ARM template functions and the Bicep `==` operator are documented: https://learn.microsoft.com/azure/azure-resource-manager/templates/template-functions-comparison#equals, https://learn.microsoft.com/azure/azure-resource-manager/bicep/operators-comparison, https://learn.microsoft.com/azure/azure-resource-manager/templates/template-functions-array#contains). With the value `deploy` in a parameter file, `if (mode == 'Deploy')` therefore means not deployed. Values are type-checked and coerced (`"true"` for `bool`); a mismatch, or a string that
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

- With a parameter file, parameter-driven rules evaluate instead of skipping. Correction (amendment 2): this holds for conditions, names and properties that depend on parameters directly or through root variables built from the supported literal functions; conditions that depend on other functions, or on variables of child modules, still stay unresolved. Facts that rest on a default are marked, never silently equal to facts from the file.
- Parameters supplied only by `azd` prompts or `azd env set` values that are not in the `.env` stay unresolved.
- Module location costs up to 64 extra offline Bicep compiles per run; it stops as soon as every nested template is matched.
- The 1 MiB and 2000-parameter bounds are engineering limits, not Azure limits.

## Not decided here

- Multi-layer infrastructure (`infra.layers`) still has no parameter file handling.
- Parameter files of nested modules are not read (Bicep modules take no parameter files).

## Amendment 1 (2026-10-16): confidence of default-derived findings

The engine lowers the confidence of a finding to `likely` (medium) when the ARM model reports through `FromDefault` that the finding's resource took a fact from a parameter's Bicep default. The cap is applied in `internal/rules/rules.go` (`capDefaultDerived`) before the catalogue clamp, so it can only lower confidence. The evidence text and the fingerprint are not changed, so adding a parameter file does not re-key existing baseline entries. A finding whose rule already set a lower confidence keeps it.

## Amendment 2 (2026-10-16): file-load guard, variable folding, default paths, ignored values

Review P1-P8 of the Phase 1 Azure review changed four things.

1. **File-load guard (point 7 enforced).** Point 7 promised reads only under the project root, but the Bicep compiler itself reads files through `loadTextContent`, `loadFileAsBase64`, `loadJsonContent`, `loadYamlContent` and `loadDirectoryFileInfo`, and a hostile `.bicepparam` or module could put an outside file into a resource name in a report. Before the doctor compiles a `.bicepparam`, the entry `.bicep` or a module `.bicep`, `bicep.GuardFileLoads` scans the file and, transitively and bounded (256 files, 1 MiB each), every local file it references. The guard is a **lexical scan, not a Bicep lexer or parser**, and it refuses when in doubt. What it does:
   - Comments and string literals are skipped; `${...}` interpolation inside strings is scanned; triple-quote multi-line strings are skipped. An unterminated comment, string or interpolation is refused.
   - A load-function name (matched case-insensitively) must be followed by `(` after whitespace and block comments. Bicep 0.48.1 accepts `loadTextContent/* x */('../../outside.txt')`, so a name followed by anything else is refused. A line comment between the name and `(` does not compile in Bicep 0.48.1 and is refused as well.
   - The first argument must be one plain single-quoted literal (no escape, no interpolation) followed by `,` or `)`. A comment between `(` and the literal, or between the literal and `,` or `)`, is refused although Bicep accepts it.
   - The literal must name a relative path that, cleaned against the file's directory, stays inside the project root and resolves through `os.Root` (so symlinks out of the root fail) to a regular file (a directory for `loadDirectoryFileInfo`). Absolute, drive, UNC and backslash paths are refused.
   - A path with a `.azure` segment (any case) is refused, because `.azure/<env>/.env` holds azd environment values. The check is on the path text; a symlink inside the project that points into `.azure` is not detected.
   - Files that are followed: the string after `module <name>` (a `.bicep`, `.bicepparam` or local `.json` path), and the string after `using`, `extends` and `from` (`import ... from`). Other strings are ignored, so a URL in a variable such as `'https://github.com/org/repo/blob/main/infra/main.bicep'` does not stop compilation. A local `.json` module is checked for its location only and is not scanned, because an ARM JSON template cannot call a Bicep load function. A reference that leaves the root, including an outside `.json` module that Bicep would embed, is refused.
   - Known differences from the Bicep lexer: reference detection looks at the one or two previous tokens, so a reference written in a form the scan does not recognise (a registry or template spec alias, an interpolated path) is not followed; function names are matched case-insensitively even where Bicep may be case-sensitive; a load-function name used as a variable or property name is refused because it is not followed by `(`; comments inside the argument list are refused; files named by `bicepconfig.json` and environment variables are not examined; the 256-file and 1 MiB bounds are engineering limits.

   A refusal uses the named reason `bicep-file-load-outside-project`. The compile is then not run: for the entry file the model is unavailable (rules skip `input-unavailable: compiled ARM model unavailable`, and `validation.bicep: required` exits 2 as before); for a `.bicepparam` the parameter file is not used and a note names the reason; a module file is not compiled alone and its location stays the entry file. Content of a loaded file can only reach a finding as user content from inside the project, which is the control.
2. **Variable folding.** With a parameter file, root `variables('x')` references in conditions, names, regions, properties, tags, SKU, identity and scope are replaced by the literal the variable evaluates to, using the existing `Model.Variable` machinery and `armexpr.Eval` (now `true`, `false`, `not`, `and`, `or`, `equals`, `if`, `concat`, `format`, `toLower`, `toUpper`, `empty`, `length` and `contains` on literals). A variable that is unknown, depends on a parameter without a value, is an object or array, uses another function, is part of a cycle, or exceeds the existing text bounds (`MaxBindingBytes`, `MaxExprBytes`, `MaxSubstitutedBytes`) is left as `variables('x')` and compares as unknown. Resolution is memoised and cycle-checked. Without a parameter file nothing is folded. Variables of child modules are not folded (a module's own `variables()` stay module-scoped references); that remains a limitation. Consequence 1 is corrected accordingly: the common `var useExisting = !empty(existingAccountResourceId)` / `if (!useExisting)` shape now resolves with a parameter file; variables using unsupported functions (for example `uniqueString`, `resourceGroup()`, `bool`) or child-module variables do not.
3. **Default paths and the confidence cap.** `Model.FromDefaultPaths(r)` records where a parameter default landed: `condition` (the resource's own, or an ancestor module's condition or loop), `name`, `location`, `scope`, `tags`, `sku`, `identity` or `properties.<top-level key>`. `capDefaultDerived` now lowers confidence to `likely` only when the paths include `condition`, or a path whose leaf name appears in the finding's Key or Evidence. A default that only shaped the name or region, or an unrelated property, does not cap. A model that cannot report paths keeps the earlier behaviour (cap on any default). Evidence and fingerprints are unchanged.
4. **Ignored parameter values.** Values a parameter file sets that the model cannot use (type mismatch, a string starting with `[`, an oversize value, an object, array, null or reference) now produce one stderr warning per file naming the parameters, never the values, capped at 20 names with a count of the rest. Parameters already reported as unresolved `${VAR}` references are not repeated.

## Amendment 3 (2026-10-16): string comparison, default cap limitation

1. **Case-sensitive string comparison.** `armexpr.Eval` compared strings case-insensitively for `equals` and `contains`, on the belief that ARM does. Microsoft documents `equals`, the Bicep `==` operator and `contains` as case-sensitive for strings (links in point 4). `Eval` now compares exactly. Resource names and types elsewhere in `armexpr` (`Same`, `ParseName`) still compare case-insensitively; that is a separate rule for resource identity.
2. **Limitation of `capDefaultDerived`.** `defaultFeedsFinding` decides whether a default fed a finding by looking for the leaf of the default path (for example `publicNetworkAccess` from `properties.publicNetworkAccess`) as a lower-cased substring of the finding's Key or Evidence. It does not know which properties a rule read. A rule that reads a defaulted property and does not name it in its Key or Evidence is not capped to `likely`, although its result depends on the default. This is accepted and not redesigned here. The control is a test per rule: `TestNET001FindingNamesTheDefaultedProperties` fails when NET-001 stops naming `publicNetworkAccess` or `networkAcls`. A rule that reads further defaultable properties needs the same kind of test.
