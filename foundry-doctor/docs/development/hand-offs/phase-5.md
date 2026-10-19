# Phase 5 hand-off: dependency and deployment graph outputs

Status: **gate PASSED in loop 1 of the post-merge gate run (Go 1.26.9, 2026-10-19): foundry-lead APPROVE, azure-black-belt
APPROVE, no blocking findings.** Earlier rounds 1 and 2 were REJECT and are recorded below.
Fix log: `docs/development/reviews/phase-5-fixlog-loop1.md`. Decisions and deferrals: ADR-027.

## Delivered
- `foundry-doctor graph` with `--source`, `--deployed` and `--combined` modes, and the environment-difference graph
  `--diff-base <a.json> --diff-target <b.json>` (mode `diff`; ADR-027 decision 1).
- Deterministic projection in `internal/graph/view` with source, inventory, runtime and findings overlays.
  - Combined mode keeps the deployed ARM ID on correlated (`both`) nodes; template pointers are never emitted as `resource`.
  - The runtime account is deduplicated against a source-declared account.
  - Types the Resources - List inventory cannot observe are `unobserved`, not `local`.
  - An in-scope ARM ID that matches nothing is `missing` when its type is observable by the inventory and `unobserved`
    for child and extension types (subnets, role assignments, deployments, connections, capability hosts); out of scope it is
    `external` (loop 2, ADR-027 decision 8).
  - `--deployed` keeps `missing` and `unobserved` reference nodes (deleted identities and connection targets; loop 2).
- Deployed relationship edges: account/project -> connection, deployment and capability host; capability host -> connection
  (project connection first); connection -> target; resource -> user-assigned identity (`usesIdentity`). Account -> private
  endpoint, deployed role assignments and subnets are deferred (ADR-027 decision 4).
- Renderers: Mermaid (entity-coded labels, one `class` statement per class, accTitle/accDescr), JSON, DOT (classes in labels,
  origin fill and line style), Markdown and HTML (legend, Mermaid, node and edge text tables), and optional SVG/PNG through the
  Graphviz adapter, which reports unavailable when `dot` is absent.
- A runtime overlay call that is not permitted is skipped with a stderr warning and a `summary.notes` entry.
- Samples generated from recorded fixtures: `samples/graph/{combined,private-network,external-references,environment-diff}.md`
  from `samples/graph/fixtures/*.json`, and `samples/graph/azure-yaml-only.mmd` from the CLI. The old `bicep-backed.md` could not be
  reproduced from its fixture and was removed (ADR-027 decision 5).

## Changed contracts
- `view.Node.diff`, `view.Edge.diff`, `view.Summary.{addedNodes,removedNodes,changedNodes,notes}`, origin `unobserved`, mode
  `diff`; `schemas/graph.schema.json` updated (also documents `conditional`; drops the never-emitted origin `collapsed`).
- `view.Options.Scope`, `view.RuntimeInput.ObservedTypes`, `view.Diff`, `view.NodeClasses`, `view.SchemaVersion`.
- `app.GraphRequest.{DiffBase,DiffTarget}`; formats `svg` and `png`.
- Shared adapter: `azure.Resource.UserAssignedIdentities`, decoded from `identity.userAssignedIdentities` in
  `ListResources` (no new API call or permission). Since loop 2 the decode is tolerant: an unexpected `identity` shape yields no
  IDs and never fails the call.
- Health semantics: only `Failed`/`Canceled` provisioning is unhealthy; `deploymentState: Paused` and transient states are health
  text only.

## Rule catalogue changes
- None.

## Commands implemented
- `foundry-doctor graph --source | --deployed | --combined | --diff-base <a.json> --diff-target <b.json>`
- Formats: `mermaid`, `json`, `markdown`, `dot`, `html`, and optional `svg`, `png` (Graphviz).

## Tests executed and results
Run on Go 1.26.9 in the post-merge gate: `go build ./...`, `go vet`, `gofmt -l` and `go test ./...` pass; `-race` on
`internal/graph/...`, `internal/report/graph/...`, `internal/app/...` and `cmd/foundry-doctor` passes (reviewers, `-count=5`
for the graph packages); `TestLargeGraph` (3000 nodes) passes; the catalogue check passes. CI runs staticcheck, govulncheck and
the cross-platform jobs. The earlier "not run" and "Go 1.25.13" statements are withdrawn. Goldens and samples now match the
real renderers.
Added in the gate: `TestOpaqueTokenReferenceIsNotALabel`, `TestMarkdownHostileLabelIsNotALink` (both verified to fail without
the fix).
- Tests added or changed in loop 1:
  - golden: Mermaid, JSON, DOT, Markdown, HTML; sample drift (`TestSamples`, `TestGraphSampleAzureYAMLOnly`).
  - Mermaid validity: subset parser over hostile labels, multi-class nodes and class definitions.
  - JSON output of Build and Diff validated against `schemas/graph.schema.json`.
  - view: ARM-ID correlation, account dedupe, unobserved types, identity edges, connection scopes, in-scope missing references,
    finding fallback, disconnected nodes, cycles, collapse priority, a 3000-node large-graph test and benchmark, Paused deployment
    semantics, environment diff.
  - app: overlay permission failure degrades, account not in inventory warning, SVG/PNG with Graphviz absent, diff from files,
    diff validation, connection secrets absent from every format.
  - adapter: `identity.userAssignedIdentities` decoding.

## Security and privacy review
- Read-only: every call is a GET on the ADR-006 allow-list; no new endpoint was added.
- Connection credentials and URL query strings are never emitted; covered by `TestGraphNeverEmitsConnectionSecrets`.
- Mermaid labels use entity codes and DOT labels escape quotes, so a label cannot end its statement.
- `--redact-ids` is an unkeyed FNV-64 pseudonym; `docs/graph.md` states it is not a security control. Default output contains ARM
  IDs and names; the docs warn before committing artefacts.

## Foundry lead review
- Round 1 (`docs/development/reviews/phase-5-foundry-lead-round1.md`): REJECT. Fixes applied in fix loop 1.
- Round 2 (`docs/development/reviews/phase-5-foundry-lead-round2.md`): REJECT (B7). Fixes applied in fix loop 2; pending re-review.
- The earlier "final signoff" statement had no review file behind it and is withdrawn.

## Azure black belt review
- Round 1 (`docs/development/reviews/phase-5-azure-black-belt-round1.md`): REJECT. Fixes applied in fix loop 1.
- Round 2 (`docs/development/reviews/phase-5-azure-black-belt-round2.md`): REJECT (B1). Fixes applied in fix loop 2; pending re-review.
- The earlier "final signoff" statement had no review file behind it and is withdrawn.

## Known limitations
- Deployed account -> private endpoint edges, deployed role assignments and subnets are not drawn (ADR-027 decision 4).
- The `identity.userAssignedIdentities` field is verified on the Resources - List 2021-04-01 page; its presence in the 2025-04-01
  response the adapter uses is UNVERIFIED (a missing field only yields no edge).
- The connection metadata `ResourceId` used for `targets` edges is UNVERIFIED (best-effort).
- A source `accounts/projects` resource and an azure.yaml `azure.ai.project` service can both exist; the runtime project merges
  into the one whose name matches exactly, so the other may remain a separate node.
- Diff matches nodes by kind, type and name; environment-specific names do not match. Diff inputs must be rendered with
  `--collapse-after 0` and without `--redact-ids` (documented, not enforced).
- A template resource deployed by a module into another resource group stays `local` in combined mode when the inventory scope
  is a single resource group.
- Node IDs are positional; `--max-nodes` is a soft limit; agent `connections` are not projected (ADR-027 decision 7).
- `go test -race` cannot run on windows/arm64.

## Deferred issues with rationale
- Account -> private endpoint edges: need the adapter to return private endpoint IDs; deferred to Phase 8 or a later ADR (ADR-027).
- Deployed role assignments: need the Authorization list API on the allow-list (ADR-027).
- Hash-based stable node IDs: deferred (ADR-027 decision 7).

Gate non-blocking items, open (foundry-lead NB2-NB9, azure-black-belt N3-N8): Foundry source edges agent to model, toolbox
and connection and toolbox to connection are not projected; capability-host connections match by name only (an ARM-ID form
is unverified); Mermaid classDef order may mask diff colours (unverified); model deployments show under both project and
account; schema `$id` is a placeholder; Mermaid has no separate schema; all external URLs share one label; Mermaid nodes do
not show kind; `--subscription` is not format-checked; deployed graphs have no private endpoint, role or subnet nodes
(ADR-027 decision 4). Fixed in the gate: opaque-token reference labels, Markdown table link injection, and the missing
template-nodes caveat in `summary.notes`.

## Compatibility matrix
- Not validated in fix loop 1 (no toolchain). The maintainer must record the `go version` used for the re-run.
- Source mode needs no Azure credentials. Deployed and combined modes need Reader-level read on the inventory scope and,
  for overlays, the Foundry account (see `docs/graph.md`, Permissions).

## Artefacts for next phase
- `docs/graph.md`, `schemas/graph.schema.json`, `samples/graph/` (with `fixtures/`)
- `internal/graph/{view,mermaid,json,graphviz}/`, `internal/report/graph/`
- ADR-027

## Exact next-phase prerequisites
- Run the gates listed under "Tests executed and results" and record the results here before re-review.
- Phase 8 decides account -> private endpoint edges (ADR-027).

## Definition of Done audit
| DoD item (PRD section 13) | Status (met / unmet / requirement changed by ADR-NNN) | Current evidence |
|---|---|---|
| A graph can be generated without Azure login from source | met (re-reviewed: APPROVE x2, Go 1.26.9) | `TestGraphSourceOffline`, `TestGraphSampleAzureYAMLOnly`; source mode never calls `graphClient` |
| Combined view correctly distinguishes local, Azure and external nodes | met (re-reviewed: APPROVE x2); deployed PE/role edges deferred by ADR-027 | ARM-ID correlation, account dedupe, `unobserved` origin for declared nodes and in-scope references, `missing` only for observable types, deployed mode keeps dangling references; `TestCombinedInScopeChildAndExtensionRefsAreUnobserved`, `TestDeployedKeepsMissingAndUnobservedReferences`, `TestRuntimeResourceMergesIntoUnobservedRef`, `TestCombinedCorrelatedNodeKeepsARMID`, `TestCombinedAccountDeclaredInSourceIsNotDuplicated`, `TestCombinedUnobservedTypes`, `TestInScopeUnresolvedReferenceIsMissingNotExternal`, `samples/graph/combined.md` |
| Graphs do not expose secrets or connection credentials | met (re-reviewed: APPROVE x2, Go 1.26.9) | `TestGraphNeverEmitsConnectionSecrets` (all formats), `TestRuntimeConnectionHealthSanitized`, `TestRedactIDs` |
| Rendering order is deterministic for stable diffs | met (positional IDs, ADR-027 decision 7; byte-identical runs, `-count=5`) | `TestBuildDeterministicAcrossOrderings`, `TestLargeGraph`, `TestDiffIsDeterministicAndOrderIndependent`, goldens |
