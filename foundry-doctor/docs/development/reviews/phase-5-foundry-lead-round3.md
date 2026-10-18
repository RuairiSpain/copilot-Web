# Phase 5 review: Foundry lead, round 3

Reviewer: `foundry-lead` (independent; I did not write the code). Date: 2026-10-18. Round 3, the final allowed loop.

Scope:
- Fix-loop-2 changes recorded in the "Loop 2" section of `phase-5-fixlog-loop1.md` and in ADR-027 decisions 8-10.
- `internal/graph/view/view.go` and its tests.
- `internal/azure/inventory.go` and `inventory_identity_test.go`.
- `internal/report/graph/render.go`, the goldens and the samples.
- `docs/graph.md` and the hand-off.

Environment constraints:
- There is no Go toolchain or package registry in this sandbox. I compiled nothing and ran no tests.
- I reviewed by reading every caller of the changed functions.
- The hand-off states honestly that the gates were not run in loops 1 and 2. Under this loop's gate policy, I do not block on that.

## 1. Round 2 findings: status

| Finding | Status | Evidence |
|---|---|---|
| **B7** An in-scope reference of a type the inventory cannot observe was drawn as `missing` | **CLOSED** | See the evidence below the table. |
| N1 Strict identity decode in the shared adapter | **CLOSED** | `ListResources` now decodes `identity` as `json.RawMessage`. `userAssignedIdentityIDs` (`inventory.go:165-183`) unmarshals it separately and returns nil on any error. `"identity":null` gives a nil map, also with no error. `TestListResourcesToleratesUnexpectedIdentityShapes` covers six shapes: `null`, a string, an array, an array map, a string map and a null map. |
| N2 Diffing collapsed inputs | Documented, not enforced (`docs/graph.md`, hand-off known limitations) | Accepted as non-blocking. |
| N3 Diffing redacted inputs | Documented, not enforced | Accepted as non-blocking. |
| N4 Overlay error classification | Not fixed | Non-blocking; carried forward. |
| N5 "missing" wording | **CLOSED** | The `docs/graph.md:143` row and the in-output legend (`render.go:21,23`) now describe the observable/unobserved split. Both the Markdown and HTML goldens and all four samples carry the new legend text, which I checked with grep. |
| N6 Real Mermaid parse in CI | Not fixed | Non-blocking; carried forward. |

**Evidence for B7:**
- `refOrigin` and `addRefNode` (`view.go:645-685`) now classify every unresolved reference in one place:
  - no `/subscriptions/` or URL prefix, or an in-template reference: `missing`;
  - outside the scope, or no scope: `external`;
  - inside the scope, with a type that cannot be parsed or is not observable (child type, extension type or a type not listed): `unobserved`, a node that carries the ARM ID;
  - otherwise: `missing`.
- Every reference site routes through `addRefNode`:
  - source unresolved references (`view.go:313-320`), which now run after the inventory merge, so a reference to a deployed resource resolves to its node;
  - `addConnectionTargets` (413);
  - `addIdentityEdges` (436);
  - the runtime connection `ResourceId` (537), which is now classified before the endpoint URL.
- Tests: `TestCombinedInScopeChildAndExtensionRefsAreUnobserved`, `TestCombinedSourceRefToDeployedResourceResolves`, `TestRuntimeResourceMergesIntoUnobservedRef` and `TestDeployedKeepsMissingAndUnobservedReferences`.
- The merge path the last of these tests depends on exists: `mergeExistingNode` (`view.go:1112-1124`) accepts `OriginUnobserved` in the ARM-ID loop only. The placeholder's kind comes from `classifyResource(typ)`, which matches the kinds the runtime code passes (`model`, `connection`, `capability-host`, `project`).

## 2. Compile check of the changed signatures

I read every call site.

**Functions:**
- `observedTypes(in Input) (listed, observed map[string]bool)`, defined at `view.go:594`. Its only caller is `view.go:227` (`listed, observed := observedTypes(in)`), and both values are used: `listed` at lines 318, 321, 322 and 324, and `observed` at line 326.
- `addIdentityEdges(inv, inventoryKeys, scope, observed, addNode, addEdge, nodes)`, defined at line 421, with one caller at line 321 that passes seven arguments in matching order and types.
- `addConnectionTargets(doc, scope, observed, addNode, addEdge, nodes)`, defined at line 382, with one caller at line 322 that matches.
- `addRuntimeNodes(rt, scope, observed, addNode, addEdge, nodes)`, defined at line 442, with one caller at line 324 that matches.
- `markUnobserved(nodes, observed)`, defined at line 613, with one caller at line 326 that matches. Its only helper is `inventoryObservable(n, observed)` (line 622), which calls `typeObservable` (line 637).
- `addRefNode` returns `(key string, missing, external bool)`. All four callers use three-value assignment. Every branch returns three values, and `display` is used in every branch.
- `refOrigin` has one caller. `armResourceTypeName` returns `(string, string, bool)`, and both of its uses take three values.
- `unresolvedKeyFor(ref, external bool)` is unchanged and has two callers, both with a `bool` second argument.

**Other symbols:**
- `coregraph.Unresolved` is an exported type (`internal/graph/graph.go:87`), so `var unresolved []coregraph.Unresolved` is valid.
- The loop over `unresolved` uses `u.From`, `u.Ref`, `u.InTemplate` and `u.Kind`; these are the same fields as before.
- `OriginUnobserved`, `deploymentType`, `firstNonEmpty`, `classifyResource` and `findings.Redact` are all in scope.
- No grep hit outside `view.go` and its tests calls any changed function (the `internal/app`, `internal/report` and `cmd` directories were searched).
- Test helpers `nodeByLabel`, `hasEdge`, `testScope`, `testProv` and `doc` are defined in the same package.

**Result:** I see nothing that would fail to compile. Whether gofmt alignment of the new `ListResources` decode struct is correct can only be checked with the toolchain.

## 3. Shared adapter: no regression for phase 2 and 3 callers

- The only non-graph caller of `ListResources` in production code is `internal/preflight/preflight.go:433`, which calls it with a `Types` filter. I found no Phase 3 production caller.
- The request is unchanged: same path, `$expand`, `$filter`, page limit and API version 2025-04-01.
- The decode is now strictly more tolerant than in round 2, because `identity` is decoded as raw JSON.
- No existing field changed. The added `[]string` field does not change comparability: `Resource` already held maps (`Tags`, `Properties`). The search for unkeyed `Resource{…}` literals found none.
- The `Inventory` interface signature is unchanged, so fakes and test doubles still satisfy it.

Preflight behaviour is therefore unchanged. The only difference is that a resource with an unusual `identity` value no longer causes a failure that the round-2 code could have caused.

## 4. Regression check of earlier findings

**Round 1 B1-B6:**
- B1, the diff mode: `diff.go` and the CLI wiring are untouched.
- B2 and B3, Mermaid: `labelEncoder` and the one-class-per-statement emission (`render.go:53,79-97`) are untouched.
- B5, the samples: the four samples were regenerated for the legend text only.
  - `external-references.md` is a source-mode sample with no scope. Under `refOrigin` its in-scope classification does not apply, and its references stay `external`, `missing` and `external-url` as before.
  - The `combined.md` external target lies outside the fixture scope, so it remains `external`.
- B6, the hand-off: it states that loop 2 gates were not run, and the review history is recorded (hand-off lines 4, 48 and 75).

**Round 1 non-blocking findings:** none regressed. `collapsePriority`, connection ARM-ID keying, the overlay degrade path, DOT styling and the schema are all untouched.

**ABB B1 (deployed mode):**
- `filterRaw` now keeps `missing` and `unobserved` in deployed mode (ADR-027 decision 9).
- That is consistent with B7: in deployed mode these nodes can only come from Azure evidence, because source is not built.

## 5. Non-blocking observations (no action needed for this phase)

- **NB1. Conservative references to overlay-observed types.** References are classified with `listed` (inventory types) only. So an in-scope reference to an account deployment or connection that a successful overlay did *not* return stays `unobserved`, where `missing` would be more exact. This errs toward "unknown", not toward a false failure, and ADR-027 decision 8 documents it.
- **NB2. Merged references.** An unobserved reference merged with a runtime resource gets origin `azure`, not `both`. `rankOrigin` has no rank for `unobserved`. This is cosmetic.
- **Carried forward:** N2 and N3 (diffing collapsed or redacted inputs), which are documented; N4 (an `ErrDecode` from an overlay aborts the graph); and N6 (a real Mermaid parse in CI).
- **Unverified facts that remain disclosed:**
  - the 2025-04-01 shape of `identity.userAssignedIdentities` (Learn documents it for 2021-04-01: https://learn.microsoft.com/rest/api/resources/resources/list);
  - the connection metadata `ResourceId` key;
  - whether `accounts/projects` appears in Resources - List.

  None is presented as verified, and the code is fail-safe for each.

## 6. Verdict

Status of the PRD section 13 Definition of Done, checked by reading the code (the tests have not been run):

| DoD item | Status |
|---|---|
| A graph can be generated without Azure login from source | Met |
| Combined view correctly distinguishes local, Azure and external nodes, with `unobserved` for what the inventory cannot see | Met: B4 and B7 are closed |
| Graphs do not expose secrets or connection credentials | Met |
| Rendering order is deterministic for stable diffs | Met: IDs stay positional, as ADR-027 decision 7 records |
| Functional scope: environment-difference graph | Met (B1) |

The remaining items are non-blocking or are deferred by ADR-027. The maintainer must still run `scripts/claude/verify-phase.sh --strict` and the golden and sample tests before release, as the hand-off states.

VERDICT: APPROVE
