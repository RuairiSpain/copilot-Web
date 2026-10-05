# Bicep adapter (`internal/bicep`)

Owner: Bicep package. Decision record: ADR-002. Tested CLI: **Bicep CLI 0.47.16 (3f73e1a234)**, linux-x64; floor
check also run on **0.24.24** (fixture compile, diagnostics, `.bicepparam` env handling). No Bicep parsing exists in
this repository: the adapter runs the compiler and reads ARM JSON.

## Pipeline

1. `Discover(ctx, opts)` returns a `Discovery` holding an `sdk.ToolStatus`. It never fails. Order: explicit path
   (must be absolute), else `bicep` on PATH (a relative PATH entry is refused). `az bicep` is not used. Version
   from `bicep --version`; floor `MinVersion()` = 0.24.24. States: `available`, `missing`, `unsupported-version`,
   `failed` (unparseable or crashing). A required tool that is not `available` becomes exit 2 in the app layer
   (`NewCompiler` returns `ErrCLIUnavailable` to make that mapping trivial).
2. `NewCompiler(d, CompilerOptions{ProjectDir})` then `Compile(ctx, file)` / `CompileParams(ctx, file)`.
   Commands: `bicep build <abs file> --stdout --no-restore` and `bicep build-params <abs file> --stdout --no-restore`.
   Inputs must have the right extension and stay inside the project after symlink resolution (`ErrBadInput`).
3. `Normalise(arm, Options)` turns the ARM JSON into `model.ARMTemplate` plus a parallel `[]ResourceInfo`.
4. `ToFinding` / `Adapter.Run` turn diagnostics into `sdk.Finding` (`bicep/<code>`, ADR-009).

### Execution safety

Environment is an allow-list (`MinimalEnv`): `HOME`, `DOTNET_CLI_HOME`, `TMPDIR` (a fresh temp dir per run unless
`CompilerOptions.Home` is set), `DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1`, `DOTNET_CLI_TELEMETRY_OPTOUT=1`,
`DOTNET_NOLOGO=1`, `PATH=/usr/bin:/bin` (Windows: `USERPROFILE`, `TEMP`, `TMP`, `SystemRoot`). No `AZURE_*`, token
or user variable is forwarded (tested with a fake CLI that dumps its environment). Working directory is the
project. Limits: 60 s timeout, 32 MiB stdout, 4 MiB stderr (overflow kills the process, `ErrOutputTooLarge`).
stdout and stderr are captured separately. `--no-restore` keeps the run offline; a `br:` module that is not in the
cache then fails as a compile error.

### Exit semantics

- exit 0 and JSON on stdout: `Result.OK`, diagnostics (warnings) attached.
- exit non-zero with at least one parsed error: `OK=false`, `Skipped=[bicep-compile-failed]`, not a Go error.
- non-zero without a parsable diagnostic, or exit 0 with non-JSON stdout: `ErrNoDiagnostics` (adapter error, exit 4).

## Diagnostics

The text format on stderr is used (verified, carries line, column and level):
`<path>(<line>,<col>) : <Error|Warning|Info> <code>: <message> [<docs url>]`. The compiler's SARIF is not used: it
has no start column and no level for warnings (facts doc). Unmatched stderr lines are ignored. Paths are made relative
to the project with forward slashes (a path outside the project keeps only its base name); messages are stripped of
control characters, absolute project prefixes and capped at 1000 bytes. Position is exact (`certain`) for the
diagnostic itself. Linter default levels need no `bicepconfig.json` rewrite. No end position is available
(deferred to a `bicep jsonrpc` transport).

`#disable-next-line`: the compiler drops the suppressed diagnostics, so `Compile` adds one informational diagnostic
`disable-next-line` (ID `bicep/disable-next-line`) per directive in the **entry file**, with the codes listed.
`ScanDisableDirectives(root, file)` is exported for module files. This is a one-directive line scan, not a parser.

### `.bicepparam` and environment variables

The environment is never forwarded, so `readEnvironmentVariable('X')` fails offline. Observed:
0.47.16 reports `BCP427`; 0.24.24 reports `BCP338` ("Failed to evaluate parameter ... Environment variable does not
exist"). Both map to the diagnostic plus `Skip{Reason: "bicepparam-env-var-unavailable"}`, so checks that need the
parameters or template are reported as skipped. `readEnvironmentVariable('X','default')` compiles and yields the
default. `getSecret` is not touched. Secret values reaching `parametersJson` is impossible by construction.
`ParamsResult.Values` holds literal values (Key Vault references are dropped) and may be sensitive: do not print.

## ARM forms proven by fixtures

| Construct | Array form (no `languageVersion`) | `languageVersion` 2.0 |
|---|---|---|
| Resources | `resources: [...]`, pointer `/resources/3` | `resources: {symbolic: {...}}`, pointer `/resources/acct` |
| Child resource | separate entry, name `format('{0}/{1}', ...)`, `dependsOn` has `resourceId(...)` | key `parent::child`, `dependsOn` has the symbolic name |
| `for` | `copy: {name, count}`; `copy.name` is the symbolic name (`acct::child` for children) | same `copy` |
| `if` | `condition: "[parameters('p')]"` | same |
| module | `Microsoft.Resources/deployments`, name = module `name:`, inline `properties.template`, `expressionEvaluationOptions.scope: inner` | same; nested template may be in either form independently |
| `existing` | not emitted | `existing: true` entries |

What triggers 2.0: the azd template uses user-defined types; the foundry-v2 fixture uses the experimental
`symbolicNameCodegen` flag in its `bicepconfig.json`. Both nested forms occur in one compile (synthetic main.arm.json).
The normaliser decides per template by looking at the type of `resources`, never at `languageVersion`.

## Normaliser rules

- Flattens resources depth-first, parent before module children, children of 2.0 maps sorted by key. Module
  `Microsoft.Resources/deployments` entries stay in the list (`Module=true`) and their inline template is expanded;
  the template is removed from the deployment's `Body` to avoid duplication. `templateLink` modules are not
  followed (warning). Legacy nested `resources` arrays are expanded. `existing` resources are listed in
  `Normalised.Existing` and are not part of `Template.Resources`.
- `Body` is the resource object, including `condition` and `copy`, with parameter references substituted.
  `Resource.Name` is the name as written unless it is exactly a parameter reference with a known value.
- Flags in `ResourceInfo`: `Looped`/`CopyName`, `Conditional`/`Condition`/`ConditionValue`, `Module`, `Parent`,
  `ModuleChain` (each step carries its own `Looped`/`Conditional`), helpers `MayNotDeploy()` and `MultiInstance()`.
- **Only literals are resolved.** A string that is exactly `[parameters('x')]` is replaced when `x` has a value
  supplied by the caller (`Options.ParameterValues`, from a `.bicepparam`) or by the calling module, or a literal
  `defaultValue`. Secure parameters (including `$ref` to a secure type) are never substituted. Every exact reference
  is recorded in `ResourceInfo.ParamRefs` with source `supplied`, `default` or `unresolved` and a reason
  (`no-default`, `default-is-expression`, `secure-parameter`, `key-vault-reference`, `arm-expression`,
  `unknown-parameter`). Anything else (`concat`, ternaries, `variables()`) stays an ARM expression and
  `Resource.Get` reports `Unresolved`. A default is only a fallback: rules must treat `source=default` as "likely",
  because a parameters file or azd may override it.
- Module scope: `expressionEvaluationOptions.scope: inner` builds the child scope from the deployment's
  `properties.parameters` (evaluated in the parent scope); `outer` or absent evaluates in the caller's scope.
- Limits: depth 16, 20000 resources, with warnings and `Truncated`.

## Source locations

Not provable from ARM (no source map, ADR-002 Q2). Resources declared in the entry file get `File` = entry file,
`Pos` zero, `Confidence` `likely`; resources inside a module get `uncertain` because the module file cannot be
proven (the deployment name is the module's `name:` property, not its path). Use `Pointer` for ARM-level location.
Compiler diagnostics keep their exact line and column. No line is ever derived for an ARM resource.

## Fixtures (`test/fixtures/bicep`)

`spike/` (array form, loops, conditions, modules, error and lint files), `foundry/` (array) and `foundry-v2/`
(symbolic) with four accounts (`disableLocalAuth` literal, parameter with default, parameter without default,
absent), a looped deployment, project child, conditional private endpoint and DNS module, `synthetic/` (copy of
Azure/azure-dev synthesis templates at afe4b2b4d262, MIT, `LICENSE-azure-dev`), `params/` (BCP427 case and default
case), `diagnostics/` (captured stderr, `{{DIR}}` placeholder). Regenerate with
`BICEP_PATH=/abs/bicep scripts/ci/regen-bicep-fixtures.sh`.

## Tests

Always run: `TestNormaliseARM_*`, discovery, compile (fake runner and fake CLI script), diagnostics from captured
output, fuzz seeds. `go test -tags bicep` needs an absolute `BICEP_PATH`: `TestCompileFixtures`,
`TestCompileDiagnosticFixtures`, `TestCompileParamsEnvVars`, `TestFixtureARMFresh` (committed ARM equals compiler
output after normalisation; byte equality is not required because the generator version and hash change).

## Needed elsewhere

- `model.Resource` has no Pointer, Looped or Conditional; rules needing them read `Normalised.Info[i]` (parallel
  slice). Proposal: add `Info` or those fields to `model.ARMTemplate`.
- ADR-002 should record that 0.24.24 reports `BCP338` instead of `BCP427`, and that text diagnostics replaced SARIF.
