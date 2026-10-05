# azure.yaml reader (`internal/azureyaml`)

Phase 1 reader for `azure.yaml` (ADR-004, ADR-011, rules FND-CFG-001..004, 007, 011, FND-ENV-001). It reads bytes and
returns data. It does no file I/O, no network and no evaluation of `${...}` references.

## API

```go
res, err := azureyaml.Parse(src, azureyaml.Options{File: "azure.yaml"})
```

`err` is set only when there is no usable document. Match with `errors.Is`:

| Error | Meaning |
|---|---|
| `ErrEmpty` | no content, or only comments or a BOM |
| `ErrSyntax` (`*SyntaxError`, has `Line`) | not valid YAML, including tab indentation |
| `ErrMultiDocument` | more than one `---` document (azure.yaml is strict single-document) |
| `ErrNotMapping` | the root is a scalar or a list |
| `ErrLimit` (`*LimitError`, has `Limit`) | over `bytes`, `depth`, `nodes` or `scalar` size |

Engine mapping (ADR-004 decision 5): any of these becomes the single `invalid-azure-yaml` diagnostic, and dependent
rules are skipped.

Defaults (`DefaultLimits`): 1 MiB, depth 64, 100 000 nodes, 64 KiB per scalar. A zero field in `Options.Limits` means
the default.

`Result` fields, all in document order:

- `YAML *model.AzureYAML`: `Name`, `Infra`, `Services` (host, project, `Uses` with positions, hooks, raw `Node`),
  project `Hooks`, and `Root`, the lossless tree. Every `model.Node` has `KeyPos` and `Pos` (1-based line, column in
  characters; a BOM is skipped, CRLF is fine). Duplicate keys stay in `Root`; typed fields use the first occurrence,
  as `Node.Lookup` does.
- `Duplicates`: path, key and every occurrence position. Reported as data (azd itself rejects duplicates: the
  `braydonk/yaml` loader sets unique keys, so this is not only a Foundry Doctor addition, see fact-check fact 1).
- `Interpolations`: path, scalar position, byte offset, `Form` and variable name. Forms: `env` (`${VAR}`),
  `env-default` (`:-`, `-`), `env-operator` (other shell operators), `foundry` (`${{...}}`), `escaped-env`
  (`$${VAR}`), `escaped-foundry` (`$${{...}}`), `malformed`. Operand text is never stored. `Nested` marks a `${`
  inside an operator part (the extension refuses those, ADR-004). A run of `n` dollars before `{` is live when `n` is odd.
- `Resources`, `Extensions` (raw `azure.ai.*` and legacy `microsoft.foundry` service blocks, plus top-level `azure.ai.*`
  keys, with position and node), `UnknownTopLevel`.
- `Issues`: structural problems and notes, sorted by position; `Level` is `error` (the vendored schema or azd rejects
  it) or `info` (allowed, worth recording).
- Helpers: `UndefinedUses()` (a `uses` entry that is neither a service nor a `resources:` key; entries containing `$`
  are skipped) and `UnresolvedRefs(has)` (references that need a value, ignoring defaults, escapes and Foundry
  expressions). Precedence of env sources is the caller's (ADR-004 decision 4).

## What counts as an issue

YAML level (always `info`): `yaml-anchor`, `yaml-alias`, `yaml-merge-key`, `yaml-non-scalar-key`. Aliases are never
expanded, so an alias bomb costs nothing, and a mapping with a merge key is not judged for absent keys. At most 100
issues per code are listed, followed by one summary.

Schema level, from the vendored schemas (`schemas/vendor/azd`): `missing-required`, `invalid-type`, `invalid-enum`,
`pattern-mismatch`, `invalid-length`, `min-properties`, `unknown-property` (only where the schema says
`additionalProperties: false`, for example `hooks`, a hook, `requiredVersions`), `forbidden-property` (a property the
schema sets to `false` or `not.required` for a host, such as `project` on `azure.ai.project`), `unsupported-shape`
(`modelType: hosted_agent`, `targetAgent`). Each carries the dotted path, a JSON pointer, the keyword, the schema file
and the position. Messages name keys and schema values, never document values.

Info only: `unknown-top-level-key` (the schema allows extra keys; they are also in `UnknownTopLevel`) and
`unknown-host` (the schema lists hosts as `examples`, so unknown hosts are valid; ADR-004 asks rules to skip them).

## Checker scope (not a JSON-schema engine)

Applied keywords: `type`, `enum`, `const`, `pattern`, `minLength`, `maxLength`, `minProperties`, `required`,
`properties`, `additionalProperties` (false or a schema), `items`, local and file `$ref`, `allOf`, and `if/then/else`
over `properties`, `required`, `const`, `enum` and `not`. Not applied: `oneOf`, `anyOf` (except the hooks "one or a
list" form), `format`, `uniqueItems` and anything else. A condition that uses an unmodelled keyword is skipped, so the
reader never claims a violation the schema text does not state.

Where it follows azd's decoder rather than strict JSON Schema: any scalar is a string; `null` fits every type; `yes`,
`no`, `on`, `off` are accepted as booleans; a scalar holding `${...}` is not type-, enum- or pattern-checked because it
is known only after expansion. A property that is absent does not satisfy an `if` (otherwise every host's rules would
apply to a service that has no `host`). Go's RE2 compiles the schema patterns used today; a pattern it cannot compile is
skipped.

## Vendored schemas

`schemas/vendor/azd/` holds the root schema from azd tag `azure-dev-cli_1.35.0` (`170ebb8`) and the `azure.ai.*` schemas
from commit `a64ca8f`, with `$id` and `$ref` rewritten to relative paths. See its `README.md` for provenance and the MIT
licence. Refresh with `scripts/ci/vendor-azd-schemas.sh <azure-dev clone>`, then update `recordedSHA256` in
`internal/azureyaml/schema_test.go`, `SHA256SUMS` and the README in the same change. The test fails on any drift.

Go cannot import a package below a directory named `vendor`, so the embed lives in `schemas/embed_azd.go`
(`schemas.AzdFS()`).

The `spike`-tagged JSON-schema test in `test/spikes/azd` is unchanged; it validates against a live clone with Python's
`jsonschema` and is not part of this package.

## Tests

`go test -race -cover ./internal/azureyaml/...`. Fixtures are in `test/fixtures/azureyaml/`; the two Phase 0 spike
fixtures are read in place and checked against `test/spikes/azd/expected.json`. `FuzzAzureYAMLParse` runs its seeds as a
normal test; run `go test ./internal/azureyaml -fuzz FuzzAzureYAMLParse -fuzztime 30s` to fuzz.

## Known limits

- Positions of a reference inside a scalar are the scalar's position plus a byte offset: columns inside quoted or
  multi-line scalars are not reliable.
- The checker cannot see values supplied by anchors, aliases or merge keys. Those are reported, not expanded.
- `$ref:` file includes inside services (FileRef) are not followed here; resolving them is the caller's job.
- Extension schema coverage is the keyword subset above; `oneOf` shapes (for example agent `tools`, `model` objects)
  are not validated.
