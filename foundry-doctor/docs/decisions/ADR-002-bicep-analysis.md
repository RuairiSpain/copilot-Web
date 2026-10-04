# ADR-002: Bicep analysis via the Bicep CLI and ARM JSON

- Status: Accepted (Phase 0 spike)
- Date: 2026-10-04
- Owner: `internal/bicep`
- Tested CLI: **Bicep CLI version 0.47.16 (3f73e1a234)**, `linux-x64` standalone binary
  (`https://github.com/Azure/bicep/releases/download/v0.47.16/bicep-linux-x64`), generator version in ARM
  metadata `0.47.16.16243`. Tag `v0.48.1` was listed by `git ls-remote` but has no `bicep-linux-x64`
  asset (HTTP 404), so v0.47.16 is the newest installable release found.
- Licence: MIT, Copyright (c) Microsoft Corporation (`bicep --license`, repo `LICENSE`). The binary is
  not vendored in this repository; it is an external prerequisite.
- Host: Linux, no .NET SDK installed (standalone binary), run with `DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1`
  (no ICU in the sandbox).

## Context

Foundry Doctor must analyse Bicep without parsing Bicep itself (project constraint). Rules need
(a) the resources a template produces, including loops, conditions and modules, (b) compiler and linter
diagnostics, and (c) the best source location we can honestly give. The PRD risk register
(source locations incomplete) requires `confidence=likely` or ARM-level locations where a line cannot
be proven.

## Options

1. Hand-written Bicep parser. Rejected by constraint.
2. `bicep build` to ARM JSON, analyse JSON; take diagnostics from the compiler. Chosen.
3. `bicep jsonrpc --stdio` (documented in `docs/bicep-rpc-client.md`). Gives structured diagnostics with
   start and end ranges. Viable later, but it is a long-lived protocol client; deferred.
4. `az bicep build`. Same compiler, extra Azure CLI dependency; accepted only as a discovery fallback.

## Decision

- Use the Bicep CLI as an external executable (`BICEP_PATH`, else `bicep` on PATH, else `az bicep`
  is not auto-used in MVP). Invoke `bicep build <file> --stdout`, read ARM JSON from stdout, read
  diagnostics from `--diagnostics-format sarif` (or the default text) on stderr.
- Detect version with `bicep --version` (`Bicep CLI version X.Y.Z (hash)`), record it in the report.
  A missing or non-executable CLI when a Bicep scan is requested is **exit code 2** (tool/usage error),
  never a skip.
- Normalise compiler diagnostics to `Finding` records (adapter `bicep`); do not re-implement linter
  rules. Compiler diagnostic location is exact (file, line, column) and may be reported with
  `confidence=certain` for the diagnostic itself.
- **Source mapping from ARM resources back to Bicep lines is NOT proven for the MVP.** It is a documented
  deferred limitation. Rule findings derived from ARM carry an ARM-level location plus the owning
  `.bicep` file and, at most, `confidence=likely`. No line number is emitted unless the compiler
  supplied it.

## Evidence (all from fixtures in `test/spikes/bicep/`)

Fixtures: `main.bicep` (parameters, `@secure()` param, variable, `if` resource, `for` resource,
module, `for`+`if` module, child resource via `parent:`, outputs), `modules/storage.bicep`,
`main.bicepparam`, `error.bicep` (compile error), `lint-warning.bicep` (linter warnings),
`secure-output.bicep` (secret in outputs), `bicepconfig.json` (enables
`outputs-should-not-contain-secrets`, `no-unused-*` at warning).

Commands and results:

| Command | Result |
|---|---|
| `bicep build main.bicep --stdout` | exit 0, ARM JSON on stdout (6 KB), diagnostics on **stderr** |
| `bicep build error.bicep --stdout` | exit **1**, stdout **empty** (no ARM emitted on error) |
| `bicep build lint-warning.bicep` / `secure-output.bicep` | exit **0** (warnings do not fail) |
| `bicep lint error.bicep` | exit 1; same diagnostics as build, no ARM output |
| `bicep build secure-output.bicep --stdout --diagnostics-format sarif` | ARM JSON on stdout, SARIF 2.1.0 on **stderr** |
| `bicep lint error.bicep --diagnostics-format sarif` | SARIF 2.1.0 on **stdout** |
| `bicep build-params main.bicepparam --stdout` | exit 0, JSON object with keys `parametersJson`, `templateJson`, `templateSpecId` (both values are JSON strings) |
| `bicep build-params main.bicepparam --outfile p.json` | writes only the parameters file (`$schema` deploymentParameters, `parameters.<n>.value`) |

Flags verified with `--help`: `build` has `--stdout`, `--no-restore`, `--outdir`, `--outfile`,
`--pattern`, `--diagnostics-format <Default|Sarif>`; `lint` has `--pattern`, `--no-restore`,
`--diagnostics-format`; `build-params` has `--stdout`, `--bicep-file`, and the same format flag.
`--no-restore` should be passed to avoid registry access (Azure checks and scans must be offline-safe).

### Q1. Can ARM resources be traced to Bicep source lines?

**No, not from the compiler output.** Observed ARM shapes (default settings):

- Resources are a JSON **array**; no symbolic names. A resource is identified only by `type`, `apiVersion`
  and a `name` expression (e.g. `"[format('{0}extra', variables('prefix'))]"`).
- `if` -> `"condition": "[parameters('deployExtra')]"` on the resource.
- `for` resource -> `"copy": {"name": "accts", "count": "[length(parameters('accountNames'))]"}`; the
  `copy.name` equals the Bicep symbolic name, which is the only name leakage in default mode. Loop
  variable usage becomes `copyIndex()`.
- `for` + `if` module -> both `copy` and `condition` on a `Microsoft.Resources/deployments` resource.
- Module -> a `Microsoft.Resources/deployments` resource with `properties.expressionEvaluationOptions.scope=inner`
  and the module body **inlined** as `properties.template`, with its own `metadata._generator`. The
  deployment `name` is the module's `name:` property value (`"single"`), not the module symbolic name and
  not the module file path.
- Child resource with `parent:` -> `name` becomes `"[format('{0}/{1}', <parent name expr>, 'default')]"` and
  a generated `dependsOn` with a `resourceId(...)` of the parent.
- `@secure()` param -> `"type": "securestring"`. `@description` -> `metadata.description`.
- Generated infrastructure is visible only as `dependsOn` and `resourceId`/`reference` expressions;
  nothing marks it as compiler-generated.
- A resource that is looped, conditional or a module can only be matched to source by heuristics
  (type + name expression + copy name). Two resources of identical type and name expression cannot be
  told apart, hence `likely` at best.

### Q2. Does ARM JSON carry any source map or metadata?

**No.** Only `metadata._generator {name, version, templateHash}` at the root and in each inlined module
template. No line, range, file or symbolic-name keys in default mode (asserted by the spike test).
With `bicepconfig.json` `experimentalFeaturesEnabled: {"sourceMapping": true, "symbolicNameCodegen": true}`:
`symbolicNameCodegen` works (`languageVersion: "2.0"`, `resources` becomes an object keyed by symbolic
name: `extra, accts, blobSvc, single, many`; modules still inlined, loops still `copy`), but
`sourceMapping` produced **no extra output** from `bicep build` (no `.map` file, no keys, `metadata` unchanged
apart from hash). The flag is documented as mapping deployment-layer errors back to Bicep and is consumed by
other tooling; it provides nothing we can read from `build`. Both are experimental and may change; we do
not depend on them. `symbolicNameCodegen` is a candidate to match resources to symbolic names later
(still no line numbers).

### Q3. Does `bicep build --stdout` work?

Yes: ARM JSON on stdout, diagnostics on stderr, so the two streams must be captured separately. On a
compile error stdout is empty and exit is 1. With `--diagnostics-format sarif` and `--stdout` the SARIF goes to
stderr; for `lint` it goes to stdout.

### Q4. What do diagnostics give?

Default text format, one per line: `<abs path>(<line>,<col>) : <Error|Warning|Info> <code>: <message> [<docs url>]`.
Line and column are 1-based. Codes seen: `BCP037` (warning, unknown property), `BCP057` (error, undefined
name), `no-unused-params`, `no-unused-vars`, `outputs-should-not-contain-secrets` (linter ids). Observed:

- `error.bicep(8,5) Warning BCP037`, `error.bicep(11,21) Error BCP057`
- `lint-warning.bicep(1,7) no-unused-params`, `(2,5) no-unused-vars`
- `secure-output.bicep(8,21)` for `listKeys`, `(11,26)` for secure value `pw`
- diagnostics in a module are reported against the module file path.

SARIF 2.1.0: `runs[0].tool.driver.name = "bicep"`, `results[].ruleId`, `message.text`,
`locations[0].physicalLocation.artifactLocation.uri` (`file://` URI) and `region {startLine, charOffset}`
(start only, no end line, `columnKind: utf16CodeUnits`). `level` is present for errors (`"error"`) but
**absent for warnings** in this version, so severity must default from the code/linter configuration, not
from a missing level. Text and SARIF carry the same file/line/column; SARIF is easier to parse robustly. Neither gives an end position.
`bicep jsonrpc --stdio` (`bicep/compile`) was also probed: it returns `success`, `diagnostics[]` with
`source`, 0-based `range.start/end {line,char}`, `level`, `code`, `message`, and `contents` (ARM JSON).
This is the richest option and is the preferred future transport, but it adds protocol code and is not required for MVP.

The linter severity is user-configurable through `bicepconfig.json` (we ship defaults in fixtures
only; the product must not rewrite a user's config). Offline: all built-in type diagnostics above
worked with no network (`--no-restore` not even needed for local modules).

### Q5. `.bicepparam`

`bicep build-params x.bicepparam --stdout` compiles both the parameters file and the `using` template and
returns `{parametersJson, templateJson, templateSpecId}` where the first two are JSON documents encoded as
strings. Values are resolved literals (`location: "westeurope"`, `deployExtra: false`,
`accountNames: ["x","y","z"]`). Diagnostics of the template (e.g. unused param warning at
`main.bicep(11,7)`) are included on stderr. Defaults not overridden are not copied into `parametersJson`; the
effective value needs the template default. Parameter expressions that call `readEnvironmentVariable` or
`getSecret` will resolve or need Azure, which we do not exercise here (not tested).

### Q6. Secure-output violation

`outputs-should-not-contain-secrets` fired with the fixture `bicepconfig.json` (rule set to warning; default level not tested). Secure params used in outputs and `listKeys()` are both caught with
exact line/column. This is the compiler's rule: we normalise it and do not duplicate it.

## Go interface (consumer-defined, in the package that consumes it)

```go
// package scan (consumer); implemented by internal/bicep.
type BicepCompiler interface {
    // Version reports the CLI version; returns ErrCLIMissing if unavailable (exit code 2).
    Version(ctx context.Context) (Version, error)
    // Compile builds one .bicep file offline (--no-restore). A compile error is NOT a Go error:
    // it is returned in Result.Diagnostics with Result.OK=false and ARM nil.
    Compile(ctx context.Context, bicepFile string) (Result, error)
    // CompileParams builds a .bicepparam file with its template.
    CompileParams(ctx context.Context, paramFile string) (ParamsResult, error)
}

type Result struct {
    OK          bool
    ARM         json.RawMessage // empty when !OK
    Diagnostics []Diagnostic
    Version     Version
}

type Diagnostic struct {
    File     string // absolute path of the .bicep file
    Line     int    // 1-based; 0 means unknown
    Column   int    // 1-based; 0 means unknown
    Severity string // error|warning|info; empty level in SARIF => derive from text format
    Code     string // BCP### or linter rule id
    Message  string
    DocsURL  string
}
```

Implementation: `exec.CommandContext`, separate stdout/stderr buffers, timeout, `--no-restore`,
`--diagnostics-format sarif` for parsing, `DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1` only if the host
lacks ICU. `exec.ErrNotFound` or a failed `--version` maps to `ErrCLIMissing` -> CLI exit 2. Exit 1
from bicep with parsed diagnostics is a normal result; exit 1 with no parsable diagnostics is an adapter error.
A secondary `JSONRPC` implementation can satisfy the same interface later.

## Fallback when source mapping is unavailable

1. Compiler diagnostics: exact `file:line:col` (confidence `certain`).
2. ARM-derived rule findings: `Location` = the entry `.bicep` file (or the module file when the module
   resource can be matched by name to the `modules` declared in ARM `deployments` names), plus an
   ARM pointer such as `/resources/2` or, with `symbolicNameCodegen`, `/resources/accts`, and
   `Resource` = type + name expression. Line omitted (0). Confidence `likely` when the file is inferred,
   `possible` when only the ARM pointer is known. Never fabricate lines.
3. Loops/conditions: report that the resource is `copy`/`condition` guarded in evidence; rules must
   treat `condition` resources as "may not deploy" and not assert about runtime counts.
4. Users can still jump from the finding to the compiler diagnostic location when one exists.

## Consequences

- No Bicep parsing code; behaviour follows the pinned compiler (record version in each report).
- The Bicep CLI is a runtime prerequisite for Bicep checks (exit 2 when absent). CI must pin a version
  (0.47.16 here) and run `go test -tags spike` against it to detect output changes.
- ARM-level findings have no source line in MVP; this is a documented limitation. Revisit when the
  `sourceMapping` feature stabilises or through the jsonrpc API; `symbolicNameCodegen` could improve
  identity matching.
- Linter severity depends on the user's `bicepconfig.json`; defaults differ from our fixtures.
- Module bodies are inlined in ARM, so rules over modules must recurse into `properties.template`
  and treat the owning module as the nearest attributable source.

## Not verified

Registry (`br:`) and template-spec modules (network), `az bicep build` equivalence, Windows/macOS output
differences, `bicep build --pattern` behaviour, `.bicepparam` with `readEnvironmentVariable`/`getSecret`,
UTF-16 vs rune column differences for non-ASCII source, and the `bicep jsonrpc` protocol beyond one probe.

## Reproduce

```
BICEP_PATH=/path/to/bicep go test -tags spike ./test/spikes/bicep/...
```
