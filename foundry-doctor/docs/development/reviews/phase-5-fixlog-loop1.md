# Phase 5 fix log, loop 1

Date: 2026-10-18. Author: go-implementer (fix loop 1). Reviews addressed: `phase-5-foundry-lead-round1.md` (FL) and
`phase-5-azure-black-belt-round1.md` (ABB). No Go toolchain was available: nothing was compiled or tested in this loop; the
maintainer must run `scripts/claude/verify-phase.sh --strict`.

## Blocking findings

| Finding | Status | Files changed |
|---|---|---|
| FL-B1 environment-difference graph missing | FIXED (file-based `--diff-base/--diff-target`; design recorded in ADR-027 decision 1) | `internal/graph/view/diff.go`, `diff_test.go`, `view.go` (ModeDiff, Diff fields), `internal/app/graph_cmd.go`, `graph_review_test.go`, `cmd/foundry-doctor/graph.go`, `graph_test.go`, `schemas/graph.schema.json`, `docs/graph.md`, `samples/graph/environment-diff.md`, `samples/graph/fixtures/environment-diff.json` |
| FL-B2 Mermaid multi-class `class a b,c` | FIXED (one `class <ids> <name>` per class; every name has a classDef; subset validator test) | `internal/graph/mermaid/render.go`, `render_test.go`, `testdata/graph.mmd.golden`, `internal/graph/view/classes.go` |
| FL-B3 Mermaid quote escaping (`%q` + backslash) | FIXED (entity codes `#quot;`, `#35;`, `#lt;`, `#gt;`, `#amp;`, `#96;`, `#124;`; hostile-label validity test) | `internal/graph/mermaid/render.go`, `render_test.go` |
| FL-B4 child/extension types shown as local-only | FIXED (origin `unobserved` in combined mode; runtime `ObservedTypes`; coverage documented) | `internal/graph/view/view.go`, `view_review_test.go`, `internal/app/graph_cmd.go`, `internal/graph/mermaid/render.go`, `internal/report/graph/render.go`, `schemas/graph.schema.json`, `docs/graph.md`, ADR-027 decision 3 |
| FL-B5 / ABB-B5 sample not reproducible, too few samples | FIXED (bicep-backed.md removed, ADR-027 decision 5; four fixture-generated samples with drift test; CLI drift test for azure-yaml-only.mmd) | `samples/graph/*`, `samples/graph/fixtures/*`, `internal/report/graph/samples_test.go`, `cmd/foundry-doctor/graph_test.go` |
| FL-B6 / ABB-B4 hand-off evidence unreliable | FIXED (DoD table follows the four PRD items; sign-off and Go 1.25.13 claims withdrawn; gates stated as not run) | `docs/development/hand-offs/phase-5.md` |
| ABB-B1 correlated node keeps template pointer | FIXED (`resource` is ARM-ID only; inventory match sets the ARM ID; finding match falls back to type/name derived from the ID) | `internal/graph/view/view.go`, `view_review_test.go`, `schemas/graph.schema.json` |
| ABB-B2 account node duplicated | FIXED (runtime account resolved with `mergeExistingNode` by ARM ID then name) | `internal/graph/view/view.go`, `view_review_test.go` |
| ABB-B3 no deployed relationship edges | PARTLY FIXED + ADR-027: `usesIdentity` edges from `identity.userAssignedIdentities` implemented; account -> private endpoint, deployed role assignments and subnets deferred (ADR-027 decision 4) and disclosed | `internal/azure/api.go`, `internal/azure/inventory.go`, `internal/azure/inventory_identity_test.go`, `internal/graph/view/view.go`, `docs/graph.md`, ADR-027 |

## Non-blocking findings

| Finding | Status | Files changed |
|---|---|---|
| FL-N1 positional IDs | NOT-FIXED (deferred, ADR-027 decision 7: every golden would change again) | - |
| FL-N2 / ABB-N8 collapse order | FIXED (Foundry kinds kept first, placeholders last) | `view.go`, `view_review_test.go` |
| FL-N3 `--max-nodes` not a hard cap | FIXED as documentation (soft limit in flag help and docs) | `cmd/foundry-doctor/graph.go`, `docs/graph.md` |
| FL-N4 overlay failure aborts graph | FIXED for permission/unavailable errors (skip + stderr warning + `summary.notes`); other errors still fail | `internal/app/graph_cmd.go`, `graph_review_test.go` |
| FL-N5 / ABB-N1 same-name project/account connections | FIXED (connections keyed by ARM ID; no name merge across different ARM IDs; account connections hang off the account; hosts resolve project connection first) | `view.go`, `view_review_test.go` |
| FL-N6 agent `connections` ignored | NOT-FIXED (out of scope for this loop; ADR-027 decision 7) | - |
| FL-N7 in-scope IDs drawn external | FIXED (`Options.Scope`; in-scope unmatched IDs are missing) | `view.go`, `graph_cmd.go`, `view_review_test.go` |
| FL-N8 DOT loses information; SVG/PNG unreachable; CombinedOutput | FIXED (classes in DOT labels, fill and line style; `svg`/`png` formats; stderr kept separate) | `internal/graph/graphviz/render.go`, `render_test.go`, `testdata/graph.dot.golden`, `internal/app/graph_cmd.go` |
| FL-N9 HTML/accessibility | FIXED (accTitle/accDescr; node and edge text tables in Markdown/HTML) | `mermaid/render.go`, `internal/report/graph/render.go`, `render_test.go`, `testdata/*` |
| FL-N10 colour-only cues; legend incomplete | FIXED (stroke width/dash per status; legend lists every class with its non-colour cue) | `mermaid/render.go`, `internal/report/graph/render.go`, `docs/graph.md` |
| FL-N11 schema gaps; no schema test | FIXED (`conditional`, `diff`, `notes`, `unobserved`; `collapsed` origin removed; output validated against the schema) | `schemas/graph.schema.json`, `internal/graph/json/render_test.go`, `testdata/graph.json.golden` |
| FL-N12 / ABB-N4 pseudonym strength; IDs in default output | FIXED as documentation | `docs/graph.md` |
| FL-N13 subscription-wide inventory | FIXED as documentation (recommend `--resource-group`) | `docs/graph.md` |
| ABB-N2 Paused/transient states unhealthy | FIXED (only Failed/Canceled unhealthy; test inverted with rationale) | `view.go`, `view_test.go` |
| ABB-N3 deployments merged without ID | FIXED (`d.ID` and the deployment type passed) | `view.go` |
| ABB-N5 permissions undocumented | FIXED | `docs/graph.md` |
| ABB-N6 inventory coverage undocumented | FIXED | `docs/graph.md` |
| ABB-N7 `--account` not in inventory | FIXED (stderr warning) | `graph_cmd.go`, `graph_review_test.go` |
| ABB-N9 Mermaid syntax unverified | FIXED with FL-B2/B3 (still not rendered by a real Mermaid parser; validator is a subset check) | see FL-B2/B3 |
| FL section 4 / ABB-N10 missing tests | FIXED (large-graph, disconnected, cycle, goldens for all formats, schema, secret redaction, partial permission, Graphviz absence, combined source+inventory+runtime) | test files listed above |

## Unsupported assumptions raised by reviewers

- Connection `ResourceId` metadata: still UNVERIFIED; docs now say best-effort.
- `deploymentState` domain: Paused no longer treated as failure; only provisioning Failed/Canceled are unhealthy.
- Runtime project merged with an azure.yaml service by name: unchanged; disclosed as a known limitation in the hand-off.

## Compile risks to check first

- gofmt alignment of the new struct fields and const blocks in `view.go` and of the decode struct in `internal/azure/inventory.go`.
- `internal/graph/view/view_review_test.go` uses `for i := range n` and `b.Loop()` (Go 1.22/1.24 features; module is Go 1.26.9).
- Hand-computed goldens and samples (`internal/graph/{mermaid,json,graphviz}/testdata`, `internal/report/graph/testdata`,
  `samples/graph/*.md`): if a test fails only on golden text, inspect and regenerate with `-update`.

## Loop 2 (2026-10-18)

Reviews addressed: `phase-5-foundry-lead-round2.md` (FL2) and `phase-5-azure-black-belt-round2.md` (ABB2). Still no Go toolchain:
nothing compiled or tested.

| Finding | Status | Files changed |
|---|---|---|
| FL2-B7 in-scope refs of unobservable types drawn `missing` | FIXED: `refOrigin`/`addRefNode` classify every unresolved reference (source refs, connection targets, identities, runtime targets) as external / missing (observable type) / unobserved (child or extension type, via `armResourceTypeName` + `typeObservable`); source refs are classified after the inventory merge; runtime nodes merge into an unobserved reference by ARM ID; ADR-027 decision 8 | `internal/graph/view/view.go`, `view_review_test.go` (`TestCombinedInScopeChildAndExtensionRefsAreUnobserved`, `TestCombinedSourceRefToDeployedResourceResolves`, `TestRuntimeResourceMergesIntoUnobservedRef`), `docs/graph.md`, ADR-027 |
| ABB2-B1 `--deployed` drops `missing` nodes | FIXED: `filterRaw` keeps `missing` and `unobserved` in deployed mode (ADR-027 decision 9) | `view.go`, `view_review_test.go` (`TestDeployedKeepsMissingAndUnobservedReferences`), `docs/graph.md` |
| FL2-N1 / ABB2-N1 strict identity decode | FIXED: `identity` decoded as `json.RawMessage`, parsed leniently by `userAssignedIdentityIDs` (ADR-027 decision 10) | `internal/azure/inventory.go`, `internal/azure/inventory_identity_test.go` (`TestListResourcesToleratesUnexpectedIdentityShapes`) |
| ABB2-N2 endpoint wins over in-scope `ResourceId` | FIXED (ResourceId classified first) | `view.go`, covered by `TestDeployedKeepsMissingAndUnobservedReferences` |
| ABB2-N3 | FIXED with FL2-B7 | - |
| FL2-N5 "missing" wording | FIXED (docs and the in-output legend; Markdown/HTML goldens and the four samples regenerated for the legend text only) | `internal/report/graph/render.go`, `testdata/graph.{md,html}.golden`, `samples/graph/*.md`, `docs/graph.md` |
| FL2-N2 / FL2-N3 / ABB2-N5 diff of collapsed or redacted inputs | DOCUMENTED, not enforced (`docs/graph.md`, hand-off known limitations) | `docs/graph.md` |
| ABB2-N4 cross-scope module deployments read as local | DOCUMENTED (hand-off known limitations) | hand-off |
| FL2-N4, FL2-N6, ABB2-N6 | NOT-FIXED (non-blocking; error classification and a real Mermaid parse in CI left for re-review) | - |

Compile risks to check first: `observedTypes` now returns two maps; `addIdentityEdges`, `addConnectionTargets`,
`addRuntimeNodes` and `markUnobserved` changed signatures (all callers are in `view.go` and were updated); gofmt alignment of the
`ListResources` decode struct.
