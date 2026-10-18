# Phase 5 review: Foundry lead, round 2

Reviewer: `foundry-lead`. I did not write the code and I reviewed it independently. Date: 2026-10-18. Round 2 (re-review after fix loop 1).

Scope:
- PRD section 13 (Phase 5).
- Fix log `phase-5-fixlog-loop1.md`.
- ADR-027.
- `internal/graph/**`, `internal/report/graph/**`, `internal/app/graph_cmd*.go`, `internal/app/graph_review_test.go`, `cmd/foundry-doctor/graph*.go`.
- The shared-adapter change in `internal/azure/{api.go,inventory.go,inventory_identity_test.go}`.
- `schemas/graph.schema.json`, `docs/graph.md`, `samples/graph/**`, `docs/development/hand-offs/phase-5.md`.

Environment constraints:
- There is no Go toolchain and no package registry in this sandbox, so `go build` and `go test` were not run by anyone.
- I reviewed by reading the code. I checked the Markdown, HTML, Mermaid, DOT and JSON goldens by hand-tracing (Markdown and HTML also through a Python port of the renderer).
- I could not regenerate the fixture samples, because that needs `view.Build`. Their correctness is therefore unverified until `TestSamples` runs.
- Following the gate policy for this loop, I do not block on the gates not being run. The hand-off states this honestly.

## 1. Round 1 findings: status

| Finding | Status | Evidence |
|---|---|---|
| B1 Environment-difference graph missing | **CLOSED** | `view.Diff` (`internal/graph/view/diff.go`) matches nodes by kind, type and name and marks them added, removed or changed; edges are added or removed. The CLI wires `--diff-base/--diff-target` (`cmd/foundry-doctor/graph.go:52-80`, `app.graphDiff`). Tests: `diff_test.go` (4 tests) and `TestGraphEnvironmentDiff*`. Sample: `samples/graph/environment-diff.md`. The file-based design and its limits are recorded in ADR-027 decision 1. That design meets the PRD's "Implement environment-difference graph". See N2 and N3 for usability gaps. |
| B2 Mermaid multi-class `class a b,c` | **CLOSED** | `mermaid.Render` now emits one `class <ids> <name>` per class, listing several node IDs where needed, which is the documented form (`class A,B,C myStyle`, https://mermaid.ai/docs/style-and-customize/create-custom-styles). Every name in `classNames` has a matching `classDef`. The golden (`testdata/graph.mmd.golden`) matches the code by hand-trace. Rendering in a real Mermaid parser is still unverified; the validator in `render_test.go` checks only a subset of the syntax (see N6). |
| B3 Mermaid quote escaping | **CLOSED** | `labelEncoder` uses a single-pass `strings.NewReplacer` to turn `#`, `"`, `<`, `>`, `&`, `` ` `` and `\|` into Mermaid entity codes (https://mermaid.js.org/syntax/flowchart.html#entity-codes-to-escape-characters). Labels are written as `["…"]` without `%q`. The golden line `n4["x#quot;]--#gt;evil #35;1 #lt;b#gt;"]` is exactly what the encoder produces. `TestRenderEscapesHostileLabels` asserts there is no `\"` anywhere in the output. |
| B4 Child and extension types shown as local-only | **PARTIALLY** | For **declared source nodes**, the fix works: `markUnobserved` and `inventoryObservable` (`view.go:604-650`) mark them `unobserved`, the behaviour is documented (`docs/graph.md` "Inventory coverage") and tested (`TestCombinedUnobservedTypes`). However, the same false-status problem has moved to **references**: an in-scope ARM ID of an unobservable type is now drawn as `missing`. See new blocking finding B7. |
| B5 Sample not reproducible; too few samples | **CLOSED** (pending test run) | `bicep-backed.md` is removed (ADR-027 decision 5). Four samples are generated from fixtures (combined, private-network, external-references, environment-diff), and `TestSamples` fails on drift. `TestGraphSampleAzureYAMLOnly` checks `azure-yaml-only.mmd` against the CLI output. I could not confirm that the hand-written sample text equals the `Build` output; a mismatch would fail a test, not compilation. |
| B6 Hand-off evidence unreliable | **CLOSED** | The sign-off and Go 1.25.13 claims are withdrawn. The gates are stated as not run. The DoD table now follows the four PRD section 13 items, with honest "by inspection" statuses. |
| N1 Positional IDs | Deferred (ADR-027 decision 7). Accepted. | |
| N2 Collapse order | CLOSED | `collapsePriority` and the stable keep-order sort; `TestCollapseKeepsFoundryNodesFirst`. |
| N3 `--max-nodes` soft limit | CLOSED (documented) | Flag help and `docs/graph.md`. |
| N4 Overlay failure aborts the graph | CLOSED | The `overlay` helper in `graph_cmd.go` skips a step on `azure.ErrUnavailable`. HTTP 403 and 401 are returned as `*UnavailableError` (`transport.go:512-515`) whose `Is(ErrUnavailable)` is true (`api.go:62`), and the skip adds `summary.notes`. Tested by `TestGraphOverlayPermissionFailureDegrades`. |
| N5 Same-name project and account connections | CLOSED | Connections are keyed by ARM ID, and capability hosts resolve the project connection first (`view.go:515-525`); `TestConnectionScopesAndSameName`. |
| N6 Agent `connections` | Deferred (ADR-027 decision 7). Accepted. | |
| N7 In-scope IDs drawn as external | CLOSED as stated, but it introduced B7 | `isExternalRef` (`view.go:929`). |
| N8 DOT, SVG/PNG, CombinedOutput | CLOSED | DOT labels now carry the classes, with fill and line styles. `svg` and `png` are reachable through `graphvizRenderer`. An absent `dot` binary reports unavailable (`TestGraphSVGWithoutGraphvizIsUnavailable`). Stderr is kept separate from the image bytes. The DOT golden matches by hand-trace. |
| N9 HTML and accessibility | CLOSED | `accTitle` and `accDescr` are emitted; Markdown and HTML have node and edge tables. The Markdown and HTML goldens match by hand-trace: Go's `html.EscapeString` writes `&#34;`, as the golden expects. |
| N10 Colour-only cues | CLOSED | Each status has a stroke width or dash, and the legend lists the non-colour cue for each. |
| N11 Schema gaps | CLOSED | `conditional`, `diff`, `notes` and `unobserved` are in the schema, and `TestRenderMatchesSchema` validates real `Build` and `Diff` output. |
| N12 Pseudonym strength | CLOSED (documented) | `docs/graph.md:173-180`. |
| N13 Subscription-wide inventory | CLOSED (documented) | |
| Section 4 missing tests | CLOSED by inspection | Tests now cover large graphs, disconnected nodes, cycles, goldens for every format, the schema, connection-secret leakage (`TestGraphNeverEmitsConnectionSecrets`), partial permission, Graphviz absence and drift. |
| Assumption: connection `ResourceId` key | Still UNVERIFIED | Disclosed in `docs/graph.md` and `api.go`. Acceptable as best-effort. |
| Assumption: `deploymentState` domain | Resolved | Only `Failed`/`Canceled` provisioning is unhealthy; `Paused` is health text only (ADR-027 decision 6). |

## 2. Focus items from the brief

**The shared-adapter change (`azure.Resource.UserAssignedIdentities`).**
- Shape: Microsoft Learn documents `GenericResourceExpanded.identity.userAssignedIdentities` for Resources - List as a dictionary. Its keys are user-assigned identity ARM IDs of the form `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ManagedIdentity/userAssignedIdentities/{name}`, and its values are objects (https://learn.microsoft.com/rest/api/resources/resources/list, page version **2021-04-01**).
- The adapter calls **2025-04-01** (`transport.go:126-127`). I confirmed that the 2025-04-01 `resources.json` exists. It is now generated from TypeSpec (https://github.com/Azure/azure-rest-api-specs/blob/main/specification/resources/resource-manager/Microsoft.Resources/resources/stable/2025-04-01/resources.json), but my fetch was truncated before the `Identity` definition. **The 2025-04-01 shape therefore remains UNVERIFIED.** The code and ADR-027 say so honestly and do not present it as verified.
- The decode (`map[string]json.RawMessage`, keys only) matches the documented shape and tolerates any value shape. The key-only read is also correct: the documentation says the dictionary keys are the identity IDs.
- One robustness concern, non-blocking: the identity is decoded inside the same `json.Unmarshal` as the rest of the resource. If any resource in the scope returned a non-object `identity` or `userAssignedIdentities`, the whole `ListResources` call would fail. That call is shared with Phase 2 preflight and Phase 3. See N1.
- No new API call or permission is involved, and `TestListResourcesUserAssignedIdentities` covers the documented shape.

**`--diff-base/--diff-target`.**
- Validation is correct: both files are required; the flags cannot be mixed with `--source`, `--deployed` or `--combined`; Azure and findings flags are rejected; a diff graph cannot be diffed again; an unknown `schemaVersion` is refused.
- Output is deterministic: identity keys are sorted with ordinal tie-breaks, and edges are sorted by ordinal.
- The input files are untrusted user JSON, but every renderer re-escapes labels, so there is no injection path.
- Usability gaps are in N2 and N3.

**The `unobserved` origin.**
- The semantics are sound and conservative. A type counts as observable only if the inventory or a successful overlay call returned it, or if it is a top-level type outside the extension list.
- Over-conservatism: a tracked child type such as `privateDnsZones/virtualNetworkLinks` probably does appear in Resources - List (unverified). When none exists in the scope, such a node is `unobserved` rather than `local`. That errs toward "unknown", which is acceptable.

**Hand-computed goldens.**
- These match the code by hand-trace or Python port: Mermaid, DOT, JSON (including the `"source": {}` that `omitempty` cannot suppress on a struct, and no trailing newline from `MarshalIndent`), Markdown and HTML.
- The fixture-generated samples could not be traced, because that needs `Build` (see B5).

**Compile check.**
- In the files I read, I found nothing that would not compile:
  - `DiffAdded`, `DiffRemoved` and `DiffChanged` are untyped string constants, so they are fine both in `[]string` and in comparisons with `string` fields.
  - `unresolvedKeyFor(ref, external)` has the same signature at all five call sites.
  - `graphvizRenderer`, `emitGraph`, `notes` and `deploymentType` are all declared.
  - `UnavailableError.Is` exists.
  - `for i := range n` and `b.Loop()` need Go 1.22 and 1.24, and the module declares 1.26.9.
- gofmt alignment cannot be checked without the toolchain.

## 3. New blocking finding

**B7. In combined and deployed modes, an in-scope reference to a type the inventory cannot observe is drawn as `missing`.**
- Where: `internal/graph/view/view.go`:
  - `isExternalRef` (line 929), whose comment reads "the inventory is complete for that scope".
  - Its callers: source unresolved references (line 268), `addConnectionTargets` (lines 411-419), `addIdentityEdges` (lines 441-447) and runtime connection targets (lines 550-556).
- Why it is wrong: `Options.Scope` is set whenever an inventory is listed (`graph_cmd.go:186-192`), and any ARM ID under it that matches no node is labelled `missing`, which the legend defines as "Referenced but unresolved" (red, dashed). But Resources - List does not return:
  - extension resources, such as role assignments (https://learn.microsoft.com/rest/api/authorization/role-assignments/list-for-scope);
  - proxy child resources, such as subnets, account deployments, connections and capability hosts.

  ADR-027 decision 3 and `docs/graph.md` already say this. So the inventory is not complete for the scope. Some examples:
  - A Bicep `existing` subnet ID, or a subnet passed as a parameter. This is the private-network architecture: `.../resourceGroups/rg/providers/Microsoft.Network/virtualNetworks/v/subnets/s`.
  - A role-assignment or deployment ID used as a `dependsOn` or connection target inside the graphed resource group.

  In each case a deployed resource is shown as a broken reference. This is the same false-status class as round-1 B4, now on reference nodes. It violates the PRD section 13 DoD item "Combined view correctly distinguishes local, Azure and external nodes". `TestInScopeUnresolvedReferenceIsMissingNotExternal` only covers a top-level type, so it does not catch this.
- Fix:
  - In the "in scope, unmatched" branch, derive the type with `armResourceTypeName`. If `inventoryObservable` for that type is false (a child type, an extension type, or a type that was not observed), emit an `unobserved` reference node, not `missing`, and do not set `missing` on the edge. Keep `missing` only for observable in-scope types.
  - Add a test with an in-scope subnet ID and an in-scope role-assignment ID in combined mode, plus a test for the observable top-level case that already exists.
  - Update the `isExternalRef` comment and the "missing" row in `docs/graph.md`.

## 4. Non-blocking findings

- **N1. Make the identity decode fail-safe in the shared adapter.** Decode `identity` as `json.RawMessage` and parse it in a second, error-tolerant step, so that a resource whose identity has an unexpected shape yields no edges instead of failing `ListResources` for Phase 2, Phase 3 and Phase 5 together. Add a test with `"identity": null` and with `"userAssignedIdentities": null`. Record the 2025-04-01 `Identity` definition once it has been read from the spec, and drop the UNVERIFIED note then.
- **N2. Diff inputs rendered with the default `--collapse-after 80` contain collapse buckets.** Bucket names are cleared and their labels encode origin and kind, so `identityKeys` pairs buckets, not resources, and real changes inside them are hidden. Either `Diff` should refuse inputs with `summary.collapsedNodes > 0` (or warn and add a note), or the docs and help should tell users to render diff inputs with `--collapse-after 0`.
- **N3. Diff inputs rendered with `--redact-ids` never match across environments.** `name` is cleared and the label is a hash that includes the environment-specific ARM ID, so every node comes out as both added and removed. Refuse redacted inputs, which are detectable (empty names and `kind-` hash labels), or document that redaction must be applied only on the diff output (`graph --diff-base … --redact-ids`), which the code already supports.
- **N4. Overlay error classification.** `ErrNotAllowed` (allow-list) and `ErrDecode` errors still abort the whole graph, with an internal exit code. That is acceptable, but `ErrDecode` on a preview Foundry API could arguably degrade like a permission failure. Consider treating it the same way, with a note.
- **N5. `docs/graph.md` "missing" wording** should be updated together with B7: "Referenced, in scope, of a type the inventory observes, and absent".
- **N6. The Mermaid validator is a subset check.** CI should, once, render the five goldens and the samples with `@mermaid-js/mermaid-cli` (behind a build tag or in a CI job) to close the residual risk from B2 and B3. Without a real parse the risk is low but not zero.

## 5. Unsupported assumptions (still open)

- The 2025-04-01 Resources - List `identity.userAssignedIdentities` shape is unverified (it is documented for 2021-04-01). This is disclosed.
- The connection metadata key `ResourceId` is unverified. This is disclosed.
- Whether `Microsoft.CognitiveServices/accounts/projects` and `privateDnsZones/virtualNetworkLinks` appear in Resources - List is unverified. This is disclosed for projects; the code handles both conservatively.
- Runtime projects are still merged with an azure.yaml service by name. This is disclosed as a known limitation.

## 6. Missing tests

- B7: in-scope references of child and extension types in combined and deployed modes.
- N1: `ListResources` with `identity: null` and with a malformed `userAssignedIdentities`.
- N2 and N3: diffing inputs that contain collapse buckets or redaction.

## 7. Verdict

Five of my six round-1 blocking findings are closed. B4 is partially closed: it is fixed for declared nodes but has moved to reference nodes as B7. The remaining unmet PRD section 13 DoD item is "Combined view correctly distinguishes local, Azure and external nodes" (B7). The fix is small and local to `view.go`.

VERDICT: REJECT
