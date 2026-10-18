# Graph outputs

`foundry-doctor graph` renders the Phase 1-3 correlation graph without changing
validation semantics. Decisions and deferrals: ADR-027.

## Formats

- `mermaid`: primary portable text output
- `json`: machine-readable graph matching `schemas/graph.schema.json`
- `markdown`: legend, summary, embedded Mermaid, and text tables of every node
  and edge
- `dot`: Graphviz DOT text; each label also names the node's classes
- `html`: self-contained HTML page around the Markdown (no script)
- `svg`, `png`: optional, rendered by the Graphviz `dot` binary. When `dot` is
  not on `PATH` the command exits unavailable (exit 2) with "optional Graphviz
  renderer not installed"; Mermaid, Markdown and DOT text are always available.

Mermaid output is a restricted, deterministic subset of the flowchart syntax
(https://mermaid.js.org/syntax/flowchart.html):

- opaque node IDs (`n1`, `n2`, ...) assigned in sorted key order; IDs are
  positional, so adding a node renumbers later IDs
- quoted labels only; `"`, `#`, `<`, `>`, `&`, backtick and `|` are written as
  Mermaid entity codes (`#quot;`, `#35;`, `#lt;`, `#gt;`, `#amp;`, `#96;`,
  `#124;`), and line breaks become spaces. Backslash escapes are never used:
  Mermaid does not support them.
- one `class <ids> <name>` statement per class name, every name defined by a
  `classDef`
- `accTitle` and `accDescr` for screen readers
  (https://mermaid.js.org/config/accessibility.html)

## Modes

- `--source`: local `azure.yaml` plus optional compiled ARM/Bicep. Works
  without Azure login.
- `--deployed`: read-only Azure inventory projection.
- `--combined`: merged source and Azure view; node origins show drift.
- `--diff-base <a.json> --diff-target <b.json>`: environment-difference graph
  of two graph JSON files rendered with `--format json`, for example
  `--deployed` for two resource groups. Nodes are matched by kind, type and
  name (never by ARM ID, which differs per environment) and marked `added`,
  `removed` or `changed`; edges are marked `added` or `removed`. Resources
  whose names embed the environment name do not match. No Azure call is made.
  Render the input files with `--collapse-after 0` (collapse buckets would be
  compared as buckets, hiding changes inside them) and without `--redact-ids`
  (redacted labels include environment-specific IDs, so nothing would match);
  pass `--redact-ids` to the diff command instead.

Deployed and combined modes require `--subscription`; prefer also giving
`--resource-group`, because a subscription-wide inventory has few edges and
fails as unavailable above the collection limit (2000 resources, or four times
`--max-nodes`). When `--account` and `--project` are supplied, runtime overlays
use project-scoped Foundry hosts and connections plus account-scoped
deployments and connections. `--account` also requires `--resource-group`;
`--project` requires both `--account` and `--resource-group`. If `--account`
is not in the inventory, the command warns on stderr and draws the overlay
without an account node.

If the inventory listing would be truncated, the command exits unavailable
instead of emitting a partial graph. If a runtime overlay call is not
permitted or unavailable, that overlay is skipped, a warning is written to
stderr and `summary.notes` records it; the graph is still rendered. Any other
overlay failure stops the command.

### Permissions

All calls are read-only GETs on the ADR-006 allow-list. Deployed and combined
modes need `Microsoft.Resources/subscriptions/resources/read` (or
`.../resourceGroups/resources/read`); the runtime overlay needs read on
`Microsoft.CognitiveServices/accounts/deployments`, `accounts/connections`,
`accounts/capabilityHosts`, `accounts/projects`, `accounts/projects/connections`
and `accounts/projects/capabilityHosts`. The built-in Reader role covers these
through `*/read`. Connection credentials are never requested: `listSecrets` is
not on the allow-list.

### Inventory coverage

The deployed inventory is the ARM Resources - List API
(https://learn.microsoft.com/rest/api/resources/resources/list), not Azure
Resource Graph. It returns top-level resources. It does not return extension
resources such as role assignments, which have their own list API
(https://learn.microsoft.com/rest/api/authorization/role-assignments/list-for-scope),
and proxy child resources such as subnets, account deployments, connections and
capability hosts are read from their parent. Therefore, in combined mode:

- a source node whose type the inventory cannot observe is `unobserved`, not
  `local`. Unobservable types are child types (two or more `/`), the extension
  types `Microsoft.Authorization/*`, `Microsoft.Insights/diagnosticSettings`
  and `Microsoft.Resources/deployments`, and azure.yaml services (agents,
  toolboxes, ...). A type becomes observable when the inventory returns a
  resource of that type or a runtime overlay for it succeeded (for example
  account deployments with `--account`).
- without `--account`, Foundry projects, deployments, connections and
  capability hosts are not read; whether `accounts/projects` appears in
  Resources - List is UNVERIFIED.

Relationship edges in deployed and combined modes: account -> project
(`contains`), account -> deployment (`deploys`), project or account ->
connection (`uses`, by the connection's ARM scope), project or account ->
capability host (`hosts`), capability host -> connection (`uses`, project
connection first), connection -> backing resource (`targets`), and resource ->
user-assigned identity (`usesIdentity`, from `identity.userAssignedIdentities`
in the inventory list response; the field is verified on the 2021-04-01 page,
its presence in the 2025-04-01 response the adapter uses is UNVERIFIED).
Not yet drawn (ADR-027): account -> private endpoint edges, deployed role
assignment nodes and identity -> role edges, and deployed subnet nodes.

The connection `targets` edge uses the connection metadata `ResourceId` key,
which is best-effort and UNVERIFIED (see `internal/azure/api.go`
`ConnectionTarget`). When it is absent, an endpoint URL is drawn as an
external node.

An ARM ID reference (source `uses`/`dependsOn`, a connection target, a
user-assigned identity) that matches no node is classified as follows:

- outside the inventory scope (`/subscriptions/<id>` or
  `/subscriptions/<id>/resourceGroups/<rg>`): `external`;
- inside the scope, of a type the inventory observes (a top-level type not on
  the extension list, or any type the inventory returned): `missing`, because
  the inventory is complete for those types;
- inside the scope, of a type the inventory cannot observe (subnets, role
  assignments, account deployments, connections, capability hosts and other
  child or extension types): `unobserved`, a node that carries the ARM ID. If a
  runtime overlay later returns that resource, it merges into the node.

Source mode has no scope, so every ARM ID it cannot resolve is `external`.
For a runtime connection, the backing `ResourceId` is classified before the
endpoint URL.

`--deployed` never builds source, so its `missing` and `unobserved` nodes are
Azure-side references (for example a deleted user-assigned identity or a
deleted connection target) and are kept.

## Legend

| Class | Meaning | Non-colour cue |
|---|---|---|
| `local` | Declared in source; in combined mode, a type the inventory observes but did not return (not deployed in scope) | solid outline |
| `azure` | Present in deployed Azure state only | solid outline |
| `both` | Declared in source and correlated with deployed Azure state (the node carries the ARM ID) | solid outline |
| `unobserved` | Declared in source, or referenced in scope, with a type the inventory cannot observe, so its deployment is unknown | dotted outline |
| `external` | Referenced outside the graphed scope | solid outline |
| `missing` | Referenced but unresolved: a name that matches nothing, or an in-scope ARM ID of a type the inventory observes that it did not return | dashed outline |
| `collapsed` | Summary bucket for nodes over the size limit; keeps its underlying origin | dashed outline |
| `unhealthy` | `Failed` or `Canceled` provisioning, a connection error, or an error finding | thick outline |
| `baselined` | Has baselined findings | fine-dashed outline |
| `added`, `removed`, `changed` | Diff mode only | thick or long-dashed outline |

Transient provisioning states (Creating, Updating, ...) and
`deploymentState: Paused` are shown as health text, not as unhealthy.

Node kinds: `project`, `agent`, `model`, `connection`, `toolbox`, `skill`,
`routine`, `evaluation`, `foundry-account`, `capability-host`, `identity`,
`role`, `network`, `search`, `storage`, `apim`, `monitoring`, generic
`resource` and `service`, plus `external` and `missing` placeholders.
Edge kinds: `uses`, `dependsOn`, `hosts`, `deploys`, `contains`, `targets`,
`usesIdentity`.

The JSON `resource` field is always an ARM resource ID, never a template
pointer, and is absent for nodes not correlated with deployed state.

## Size controls

`--collapse-after` (default 80) and `--max-nodes` keep Foundry nodes
(account, project, agent, model, connection, capability host, toolbox) first,
then other resources, then external and missing placeholders; the rest are
collapsed into one bucket per origin and kind. `--max-nodes` is a soft limit:
the buckets are added on top. `--max-edges` drops edges beyond the limit and
reports the count. Diff mode applies no limits.

## Accessibility

- `markdown` and `html` include the legend, the exact Mermaid source, and text
  tables listing every node (with its classes and health) and every edge, so
  the graph can be read without rendering it and without relying on colour.
- Mermaid output carries `accTitle` and `accDescr`; status classes change the
  outline width or dash pattern as well as the colour.
- Labels are plain text after escaping.

## Overlays

`--findings-report <report.json>` overlays existing Foundry Doctor JSON report
findings on matching nodes: by ARM ID first, then by type and name (also
derived from the ARM ID). Active error findings mark nodes unhealthy; baselined
findings are counted separately. Findings that match no node are counted in
`summary.unmatchedFindings`.

## Privacy

- Connection credentials, keys and URL query strings are never emitted:
  connection targets that are URLs are drawn as `external-url`, placeholder
  nodes carry no name, and runtime error text is reduced to `error reported`.
- Without `--redact-ids`, JSON, Markdown and HTML output contains full ARM IDs
  (subscription ID, resource group) and resource names. They are not secrets,
  but review them before committing generated artefacts, or use `--redact-ids`.
- `--redact-ids` replaces labels with an unkeyed FNV-64 hash of name, resource
  ID and label, and removes names, resource IDs, health text and source
  locations. The hash is a deterministic pseudonym, not a security control:
  guessable names can be recovered by trying candidates.

## Samples

`samples/graph/` holds generated samples. `azure-yaml-only.mmd` is the CLI
output for `samples/azure-yaml-only` (checked by `TestGraphSampleAzureYAMLOnly`).
`combined.md`, `private-network.md`, `external-references.md` and
`environment-diff.md` are generated from the recorded inputs in
`samples/graph/fixtures/` by `TestSamples` in `internal/report/graph`
(regenerate with `go test ./internal/report/graph -run TestSamples -update`).

## Examples

```text
foundry-doctor graph --source --format mermaid
foundry-doctor graph --combined --subscription <sub> --resource-group <rg> --account <acct> --project <proj> --format markdown --out graph.md
foundry-doctor graph --source --findings-report foundry.json --redact-ids --format json
foundry-doctor graph --deployed --subscription <sub> --resource-group rg-dev --format json --out dev.json
foundry-doctor graph --deployed --subscription <sub> --resource-group rg-prod --format json --out prod.json
foundry-doctor graph --diff-base dev.json --diff-target prod.json --format markdown
```
