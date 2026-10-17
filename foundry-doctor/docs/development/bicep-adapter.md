# Bicep adapter (`internal/bicep`, `internal/armmodel`, `internal/app/wire.go`)

Decision records: ADR-002 (analyse the compiled ARM JSON), ADR-009 (compiler diagnostics become `bicep/<code>`
findings), ADR-012 (synthetic infrastructure), ADR-015 (version contract), ADR-018 (parameter files).
No Bicep parser exists in this repository: the adapter runs the Bicep CLI and reads its output.

**Contract version: Bicep CLI 0.48.1** (`bicep.ContractVersion`, ADR-015). A CLI older than 0.48.1 is refused
(`KindVersionTooOld`); a newer one is accepted and marked `Tool.Newer`.

## Pipeline

1. `bicep.Discover(ctx, DiscoverOptions)` locates the CLI: explicit `Path` option, then `BICEP_PATH`, then `bicep`
   on `PATH` (`az bicep` is never used). It runs `bicep --version`, parses the version and compares it with the
   contract. Failures are `*ToolError` values (`missing`, `not-executable`, `version-too-old`, `version-unknown`,
   `timeout`, `adapter`); `IsDependencyError` is true for them. The app layer maps them to a warning, or to exit 2
   when `validation.bicep` is `required` for the profile (ADR-010 decision 11).
2. `armLoader.Load` (`internal/app/wire.go`) picks the entry file `<infra.path>/main.bicep` (or the path
   resolved from azure.yaml), compiles it, loads parameters, locates module files and builds the model.
   With no `main.bicep` it falls back to `main.json`; with neither the project is synthetic (ADR-004, ADR-012).
3. `armmodel.FromARM(raw, options)` turns the compiled ARM JSON into the `sdk.ARMModel` that rules read.
   The raw template is kept next to the model (`RawTemplate()`) for the preflight what-if.
4. Compiler diagnostics become findings (below).

## Compilation and offline guarantees

`Compiler.Compile` runs, with the file's directory as working directory:

```
bicep build <file name> --stdout --no-restore --diagnostics-format sarif
```

`--no-restore` keeps every run offline: a registry (`br:`) module that is not in the local cache fails as a compile
diagnostic and never triggers a download. The doctor makes no network call and needs no Azure credential.
`ExecRunner` passes an allow-listed environment only (`PATH`, `HOME`, `USERPROFILE`, `TMP`, `TEMP`, `TMPDIR`,
`SystemRoot` on Windows, `DOTNET_BUNDLE_EXTRACT_BASE_DIR`, telemetry opt-outs); no `AZURE_*`, token or user
variable is forwarded. Bounds: 60 s timeout, 32 MiB per stream (overflow sets `Truncated` and is an adapter error).

Exit semantics (`Compiler.interpret`):

- exit 0 with valid ARM JSON on stdout: `Result.OK`; warnings are diagnostics.
- non-zero exit with at least one error diagnostic: `OK=false`, a normal result and not a Go error.
- non-zero exit without a parsable diagnostic, or exit 0 without ARM JSON: adapter error.

## Diagnostics as SARIF

With `--diagnostics-format sarif` the CLI writes a SARIF 2.1.0 log to **stderr** (stdout carries the ARM JSON).
Recorded with 0.48.1: each result has `ruleId` (`BCP057`, `no-unused-params`, `no-hardcoded-env-urls`,
`secure-secrets-in-params`, ...), `message.text` ending in ` [<docs url>]`, a `physicalLocation` with a `file://`
URI and a region with `startLine` and `charOffset` (the 1-based column). `level` is `error` for errors and **absent
for warnings**, which `ParseSARIF` treats as warnings.

`ParseSARIF` is bounded and strict: at most 8 runs, 4096 results, 8 locations per result; a malformed document, an
invalid rule id or a negative position is an adapter error. It strips the trailing docs URL into `Diagnostic.DocsURL`,
sanitises the message (control characters, secret-looking assignments, 1024 bytes), converts the `file://` URI to a
path and makes it relative to the project root with forward slashes. A path outside the project keeps only its base
name, so an absolute path never reaches a report. If no SARIF document is present, text lines of the form
`file(line,col) : Level code: message` are parsed instead (`ParseTextDiagnostics`).

### Diagnostics become findings (ADR-009)

`internal/app/wire_bicep.go` converts the diagnostics of the **entry compile** (modules referenced by the entry are
reported in the same SARIF) into `sdk.Finding` values:

| Field | Value |
|---|---|
| `RuleID` | `bicep/<code>` (`bicep/BCP057`, `bicep/no-hardcoded-env-urls`) |
| `Severity` | compiler error -> error, warning -> warning, info -> info |
| `Adapter` | `bicep`; `Category`, `Basis`, `Pillar` and `Profile` are empty (no catalogue entry) |
| `Location` | repo-relative file, line, column from the SARIF region |
| `Evidence` | the message with the docs URL removed and secrets redacted |
| `Key` | `<code>:<first 12 hex of sha256(message)>`, plus `:<n>` for the n-th identical diagnostic in a file |
| `Confidence` | high (the compiler reports the position exactly) |

The key excludes the line, so moving code does not change a fingerprint or break a baseline entry; two diagnostics
with the same code but different messages, or the same message in two files, differ.

Hand-off to the engine without changing the loader interface: `armLoader` stores the findings in an `adapterStore`
keyed by project directory, whether the compile succeeded or not; `engineAdapter.Run` takes them once and appends
them to the rule findings before baseline and suppression. They therefore honour `--baseline` and `--suppressions`
(`ruleId: bicep/<code>` is a valid suppression selector) and `--fail-on`. A compile warning never fails a run at the
default `--fail-on error`; an error diagnostic is an error finding and makes the run exit 1 like any error finding,
except that `validation.bicep: required` still maps a failed compile to exit 2. Explicit rule selection (`--rules`
naming a rule, group or pillar) leaves compiler findings out; `rules.exclude` entries matching `bicep/<code>` (exact
or glob) remove them. Compiler findings are not counted as catalogue rules and have no `explain` page.

## Parameter files (`build-params`, ADR-018)

For `<entry>.bicepparam`, `Compiler.BuildParams` runs `bicep build-params <file> --stdout --no-restore`, reads
`parametersJson` from the output (bounded: 1 MiB, 2000 parameters) and parses literal values; Key Vault references and
secure values are never substituted. `readEnvironmentVariable()` reads the process environment of the runner, so the
loader passes only the non-secret-looking values of the selected azd environment (`.azure/<env>/.env`, at most 256
variables). When it cannot compute a parameter file the reason is one of the named `Param*` constants and the
loader writes a `warning:` note to stderr; parameters stay `unresolved`, never guessed. A plain `parameters.json`
is read directly. Default values are fallbacks: rules treat a default-derived value as lower confidence.

## Module file locations (template hashing)

ARM JSON has no source map, so the loader maps a module deployment to its file by hash: the template nested in the
caller carries `metadata._generator.templateHash`, and a module file compiled alone has the same hash. For the
`.bicep` files under the infra directory (at most `MaxModuleFiles` = 64, each at most 512 KiB, 3 minutes in total)
the loader compiles each with `bicep build --no-restore` and matches `RootTemplateHash` against
`NestedTemplateHashes` of the entry template, stopping when all are matched. Two files with the same hash are
ambiguous and not mapped; unmatched modules keep the entry file as location. A skipped file or an exceeded bound is
named in a stderr note (`module file locations ...`), never silent.

## `armmodel`

`armmodel.FromARM` handles both ARM resource forms (array and `languageVersion` 2.0 symbolic maps), module
deployments (inline `properties.template`, inner/outer expression scope), conditions and `copy` loops. Only
literals are resolved: a value that is exactly `[parameters('x')]` is replaced by a supplied or literal default
value; anything else stays an ARM expression and reads as unresolved, so a rule skips rather than passes. Bounds
(`armmodel.Max*`): 20000 resources, 64 KiB per expression, 8 MiB substituted text, 32 MiB expression text overall.
A model that hit a bound reports `Truncated()`; the loader writes `model-truncated` to stderr and rules treat an
absent resource as unknown.

## Source locations

A resource location is the entry file or the matched module file with confidence `likely`, and the ARM pointer;
no line number is derived for an ARM resource. Only compiler diagnostics carry exact lines and columns.

## Tests

- `internal/bicep`: discovery with a fake runner, `ParseSARIF` and text diagnostics (golden files under
  `testdata/golden`), URI handling, `interpret` exit semantics, parameter parsing, template hashing.
- `internal/app/wire_bicep_test.go`: finding mapping, key stability, redaction, selection filters, and the whole
  `Doctor` pipeline with a scripted runner serving a recorded 0.48.1 SARIF (`testdata/bicepdiag`), covering
  warnings (exit 0), error (exit 1), suppression and rule selection. These run in CI without a Bicep CLI.
- Real CLI, skipped without `BICEP_PATH` (the `spike` build tag; `REQUIRE_SPIKE_DEPS=1` turns the skip into a
  failure): `go test -tags spike ./internal/bicep/... ./internal/app/...` with `BICEP_PATH=<absolute bicep>`.
