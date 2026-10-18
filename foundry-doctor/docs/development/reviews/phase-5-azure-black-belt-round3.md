# Phase 5 review — Azure black belt, round 3 (final loop)

Phase: 5 — Dependency and deployment graph outputs (PRD §13).
Reviewer: `azure-black-belt` (independent; did not author the code or the fixes).

What I read for this round:
- The "Loop 2" section of `phase-5-fixlog-loop1.md` and ADR-027 decisions 8–10.
- `internal/graph/view/view.go`: `Build`, `observedTypes`, `markUnobserved`,
  `typeObservable`, `refOrigin`, `addRefNode`, `addIdentityEdges`,
  `addConnectionTargets`, `addRuntimeNodes`, `mergeExistingNode` and `filterRaw`.
- The new tests in `view_review_test.go`.
- `internal/azure/inventory.go`: `ListResources` and `userAssignedIdentityIDs`.
- `inventory_identity_test.go`.
- Every non-test caller of `ListResources`.
- `samples/graph/*` and `docs/graph.md`.

## Environment constraints

No Go toolchain and no package-registry access exist in this sandbox, so nothing
was compiled or run. The hand-off states this. Under this loop's gate policy that
is not blocking on its own. I judged compile-safety by reading every changed
signature and all of its callers.

## Round-2 blocker

### B1 — `--deployed` dropped in-scope dangling references — **CLOSED**

- **Fix.** `filterRaw` in `ModeDeployed` now keeps `OriginMissing` and
  `OriginUnobserved`. A comment states why: source is never built in deployed
  mode, so these nodes come only from Azure-side references.
- **Test.** `TestDeployedKeepsMissingAndUnobservedReferences` runs in
  `ModeDeployed` with `Scope` set. It asserts:
  - a `missing` `usesIdentity` edge to a deleted in-scope user-assigned identity;
  - a `missing` `targets` edge to a deleted in-scope storage account, even though
    the connection also has an `https://` endpoint;
  - exactly two `missing` nodes and no `external` node;
  - an `unobserved` subnet target with a plain edge.

  This is the test I asked for.
- **Other modes.**
  - Source mode always has an empty `Scope`, because `app.Graph` sets `scope`
    only for deployed and combined. `isExternalRef` therefore still returns
    external for every ARM ID there, and `unobserved` cannot appear in source
    output, which source mode would otherwise filter out.
  - Combined mode is unfiltered.

### Round-2 non-blocking findings carried into loop 2

| ID | Status | Evidence |
|---|---|---|
| N1 strict identity decode | CLOSED | `identity` is `json.RawMessage`. `userAssignedIdentityIDs` returns nil on any decode error. `TestListResourcesToleratesUnexpectedIdentityShapes` covers six shapes: `null`, `userAssignedIdentities: null`, a string, an array map, a string map, and an array. |
| N2 endpoint wins over in-scope `ResourceId` | CLOSED | In `addRuntimeNodes` the `looksExternal(c.Target.ResourceID)` case now comes before the endpoint case. |
| N3 child/proxy refs drawn `missing` | CLOSED | `refOrigin` returns `unobserved` for an in-scope ARM ID whose type is not observable (more than one `/`, `notInResourceList`, or an unparseable type) and not listed. `TestCombinedInScopeChildAndExtensionRefsAreUnobserved`. |
| N4 cross-scope module deployments | DOCUMENTED (hand-off known limitations); acceptable | — |
| N5 diff of redacted inputs | DOCUMENTED (`docs/graph.md`); acceptable | — |
| N6 404 on overlay is a generic error | OPEN (not fixed); remains non-blocking | — |

## Compile check of the changed signatures

**`observedTypes(in Input) (listed, observed map[string]bool)`.** The single
caller is `view.go:227`, `listed, observed := observedTypes(in)`. Both values are
used:
- `listed` is passed to `addRefNode` (unresolved source refs), `addIdentityEdges`,
  `addConnectionTargets` and `addRuntimeNodes`;
- `observed` is passed to `markUnobserved` inside the `ModeCombined` branch.

A variable used only inside an `if` still counts as used, so this compiles.

**Changed function signatures and their callers:**

| Function | New signature | Caller(s) | Arity and types |
|---|---|---|---|
| `addIdentityEdges` | `(inv, inventoryKeys, scope, observed, addNode, addEdge, nodes)` | `view.go:321` | match |
| `addConnectionTargets` | `(doc, scope, observed, addNode, addEdge, nodes)` | `view.go:322` | match |
| `addRuntimeNodes` | `(rt, scope, observed, addNode, addEdge, nodes)` | `view.go:324` | match |
| `markUnobserved` | `(nodes, observed)` | `view.go:326` | match |
| `addRefNode` | `(addNode, ref, inTemplate, scope, observed) (key string, missing, external bool)` | `view.go:318`, `413`, `436`, `537` | all four destructure 3 values; every return path assigns `key` |

**No test callers.** None of these internal functions is called from any
`_test.go` file. The grep found no test callers, so no test needs updating.

**`Build` locals.**
- `var unresolved []coregraph.Unresolved` is declared outside the source branch
  and assigned `g.Unresolved`.
- `sourceNodes` is likewise declared outside the branch and used by the
  post-inventory unresolved loop.

**Scripted checks.** Re-running the scripted unused-import and duplicate-symbol
checks over `view.go`, `inventory.go` and the test files finds no issues. I saw
no code that would fail to compile.

## Shared adapter: regression check for Phase 2/3 callers of `ListResources`

**Callers.** The non-test callers are:
- `internal/preflight/preflight.go:433` (`Types: []string{typ}`, which reads names
  and types);
- `internal/app/graph_cmd.go:191`.

`internal/runtime` does not call it.

**Behaviour.** The adapter change only adds a tolerant, best-effort
`UserAssignedIdentities` slice:
- The outer decode struct still fails only on the item fields it failed on
  before (`id`, `name`, `type`, `sku`, `tags`, `provisioningState`). Because
  `identity` is now raw JSON, it can no longer cause a decode failure. This
  removes the round-2 risk and is strictly no worse than the code before Phase 5.
- No query key, API version, path or permission changed. `$expand` is still
  `provisioningState`, and the call stays on the allow-listed
  `resources.list.*` at 2025-04-01.

**Comparability.** Adding a slice field makes `azure.Resource` non-comparable. A
repo-wide search for `==`/`!=` on `azure.Resource{}` or `ResourceList{}`, and for
`map[azure.Resource]`, returns nothing. No existing comparison breaks.

**Existing tests.** The existing adapter tests (`TestInventory` and the gate
tests) check individual fields or use `DeepEqual` on unrelated values. Their
fixtures have no `userAssignedIdentities`, so their expectations are unchanged.

**Shape.** The documented `GenericResourceExpanded.identity.userAssignedIdentities`
is a dictionary keyed by user-assigned identity ARM IDs
(https://learn.microsoft.com/en-us/rest/api/resources/resources/list-by-resource-group,
page version 2021-04-01). Its 2025-04-01 shape remains **UNVERIFIED** and is
labelled so in code, ADR-027 and the hand-off. Unexpected shapes now yield no
edge instead of an error.

## Regression check of earlier findings

- **Round-1 B1 (ARM ID on correlated nodes).** Intact: inventory merge sets
  `matched.resourceID = r.ID`.
- **Round-1 B1, new path.** Unresolved source refs are now classified after the
  merge and first resolved through `inventoryKeys`, which maps the ARM ID to the
  merged key. A source ARM-ID reference to a deployed resource therefore resolves
  to the `both` node instead of a placeholder
  (`TestCombinedSourceRefToDeployedResourceResolves`).
- **Round-1 B2 (account dedupe).** Intact: `mergeExistingNode` by ARM ID.
- **Unobserved placeholder merge.** `mergeExistingNode` now also matches an
  `unobserved` placeholder **by ARM ID only**; the name-based passes still skip
  it.
  - `rankOrigin(unobserved)=0`, so the merged node becomes `azure`.
  - The placeholder `kind` comes from `classifyResource(typ)`, so the kind
    filters in the runtime calls (`connection`, `model`, `capability-host`,
    `project`) line up. `TestRuntimeResourceMergesIntoUnobservedRef`.
- **Round-1 N1 (same-name connections).** Intact: `otherResource` guard.
- **Round-1 N2 (health semantics).** Intact: `unhealthyState`.
- **Samples.**
  - `external-references.md`: in source mode, refs keep keys
    `external:<ref>` / `missing:<ref>` and the same labels, so the node and edge
    tables are unchanged.
  - `combined.md`: it has no unresolved refs except the `rg-shared` Search
    target, which is still `external`, so only the legend text changed. That is
    consistent with the fix log.
- **`--redact-ids`.** It still clears `resource`, so the ARM ID now carried by
  `unobserved` placeholders is removed under redaction.

## New findings

**Blocking:** none.

**Non-blocking:**
- **N1 — `unobserved` placeholders show the full in-scope ARM ID.**
  - An `unobserved` reference placeholder carries the full ARM ID in `resource`
    in default (unredacted) output. This is consistent with `azure` nodes and the
    documented ID exposure.
  - Note it in the `docs/graph.md` identifier warning. No code change is
    required.
- **N2 — Round-2 N6 is still open.** Map an overlay 404 (wrong `--project` or
  `--account`) to a usage error.

## PRD §13 DoD — final assessment (by inspection; gates to be run by the maintainer)

| DoD item | Verdict |
|---|---|
| A graph can be generated without Azure login from source | Met |
| Combined view correctly distinguishes local, Azure and external nodes | Met. Deployed and combined now also keep `missing`/`unobserved` Azure-side references. Private endpoint and role edges are deferred by ADR-027 and disclosed. |
| Graphs do not expose secrets or connection credentials | Met |
| Rendering order is deterministic for stable diffs | Met |

Approval is conditional on what this environment could not do: the maintainer
must run `scripts/claude/verify-phase.sh --strict` and the Go tests, and record
the results in the hand-off. If any hand-computed golden differs only in text,
regenerate it with `-update` and review the diff.

VERDICT: APPROVE
