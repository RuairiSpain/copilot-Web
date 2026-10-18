# Phase 5 review — Azure black belt, round 2

Phase: 5 — Dependency and deployment graph outputs (PRD §13).
Reviewer: `azure-black-belt` (independent; did not author the code or the fixes).

What I read for this round:
- `phase-5-fixlog-loop1.md`, ADR-027 and the updated `hand-offs/phase-5.md`.
- `internal/graph/view/{view,diff,classes}.go` and the new tests.
- `internal/app/graph_cmd.go`, `internal/graph/mermaid/render.go`.
- `internal/azure/{api,inventory}.go` and `inventory_identity_test.go`.
- `samples/graph/**`, `docs/graph.md` and `schemas/graph.schema.json`.

## Environment constraints

- This sandbox has no Go toolchain and no package-registry access. No one ran
  `go build` or `go test`.
- The hand-off now says so honestly ("NOT RUN in fix loop 1"). Under this loop's
  gate policy that is accepted.
- Compile risk was checked by reading the code:
  - a scripted unused-import scan over every new or changed Go file found
    nothing;
  - no duplicate package-level symbols were found in the touched packages;
  - the helpers the tests use (`doc`, `newFake().on`, `testRG`/`testOID`,
    `testScope`/`testProv`, `largeInput`) are each defined exactly once;
  - `b.Loop()` and range-over-int are valid for `go 1.26.9`.
- I saw no code that would fail to compile.

## Round-1 findings — verification

| ID | Status | Evidence |
|---|---|---|
| B1 template pointer kept as `resource` | **CLOSED** | `view.go` source branch no longer sets `resourceID` (comment at the former `r.resourceID = n.ID`). The inventory merge unconditionally sets `matched.resourceID = r.ID` and maps the inventory ID to the merged key. `matchFinding` falls back to type/name from `armResourceTypeName`. Covered by `TestCombinedCorrelatedNodeKeepsARMID` (live finding matched by ARM ID; in-scope connection `targets` edge not external), `TestSourceNodesHaveNoTemplatePointerResource` and `TestFindingARMIDFallsBackToTypeAndName`. |
| B2 account node duplicated | **CLOSED** | `addRuntimeNodes` resolves the account with `mergeExistingNode(nodes, "foundry-account", rt.Account.ID, …)`, which finds the merged source node by its ARM ID. `TestCombinedAccountDeclaredInSourceIsNotDuplicated` asserts one `both` account carrying `contains`/`deploys`/`uses` edges. |
| B3 no deployed relationship edges | **CLOSED** (partly implemented, remainder ADR-deferred and disclosed) | `addIdentityEdges` draws `usesIdentity` edges from `identity.userAssignedIdentities`. See the adapter check below. ADR-027 decision 4 defers account → private endpoint, role-assignment and subnet edges. The deferral is disclosed in the hand-off ("Known limitations", "Deferred issues") and in `docs/graph.md`. ADR-027 binds the Phase 8 hand-off to carry it forward. |
| B4 hand-off DoD and phantom sign-offs | **CLOSED** | The DoD table now lists exactly the four PRD §13 items. The sign-off claims and the Go 1.25.13 claim are withdrawn. Gates are stated as not run. |
| B5 non-reproducible sample | **CLOSED** | `bicep-backed.md` is removed (ADR-027 decision 5). Four samples are now generated from `samples/graph/fixtures/*.json` by `TestSamples`, plus a CLI drift test. I hand-traced `fixtures/combined.json` through `Build`; the result matches `combined.md` exactly (details below). |
| N1 connection scope / same name | CLOSED | Connections are keyed by ARM ID. `otherResource` stops name-merging across different IDs. Account-scoped connections hang off the account. Host resolution is project-first. Covered by `TestConnectionScopesAndSameName`. |
| N2 Paused/transient states unhealthy | CLOSED | `unhealthyState` marks only `failed`/`canceled`/`cancelled` as unhealthy. `deploymentState` is shown as health text only. |
| N3 deployments merged without ID | CLOSED | `mergeExistingNode(nodes, "model", d.ID, deploymentType, …)`. |
| N4 identifiers in default output | CLOSED (docs) | `docs/graph.md` warns that output contains ARM IDs and that the pseudonym is not a security control. |
| N5 permissions undocumented | CLOSED | `docs/graph.md` "Permissions" lists the actions and Reader `*/read` coverage, and states that `listSecrets` is not on the allow-list. |
| N6 inventory coverage undocumented | CLOSED | `docs/graph.md` "Inventory coverage"; `unobserved` origin. |
| N7 `--account` not in inventory | CLOSED | Stderr warning; `TestGraphWarnsWhenAccountNotInInventory`. |
| N8 collapse order | CLOSED | `collapsePriority` keeps Foundry kinds first; `TestCollapseKeepsFoundryNodesFirst`. |
| N9 Mermaid syntax | PARTIALLY | Entity codes and one `class` statement per class look correct against the Mermaid flowchart grammar. Validity is checked only by a subset validator, not a real Mermaid parser. Deferred to the Foundry lead; not blocking. |
| N10 missing tests | CLOSED | Tests added for large graph (3000 nodes), disconnected nodes, cycles, secrets-in-all-formats, partial permission and combined source+inventory+runtime. |

### Shared-adapter change: `UserAssignedIdentities` in `ListResources`

**Shape check.** The `Resources - List` / `List By Resource Group` response item
(`GenericResourceExpanded`) has `identity.userAssignedIdentities`. Microsoft Learn
documents it as a dictionary whose keys are user-assigned identity ARM IDs:
https://learn.microsoft.com/en-us/rest/api/resources/resources/list-by-resource-group
(the page renders version 2021-04-01). The decode,
`map[string]json.RawMessage` under `identity`, collects the keys only, sorted.
That matches the documented shape.

**API version.** The adapter calls 2025-04-01 (`transport.go`
`resources.list.*`). I could not open the 2025-04-01 swagger: the GitHub raw
file is not fetchable from here, and the GitHub page is truncated before
`definitions`. The 2025-04-01 shape is therefore **UNVERIFIED**. The code comment
(`api.go` `Resource.UserAssignedIdentities`), ADR-027 decision 4 and the
hand-off all label it UNVERIFIED and state that a missing field yields no edge.
That is honest labelling, not an unverified fact presented as verified.

**Read-only and permissions.** The change adds no new call, query key or
permission.

**Tests.** `TestListResourcesUserAssignedIdentities` uses the documented shape:
a map with `{principalId}` and `{}` values, and a resource without `identity`.

### Hand-trace of `samples/graph/combined.md`

- **Node order.** The keys sort as:
  - `arm:Microsoft.Authorization…`, `arm:…CognitiveServices…`,
    `arm:…ManagedIdentity…`, `arm:…Search…`, `arm:…Storage…`;
  - `az:…appi-sample`;
  - `conn:…/connections/search-conn`, then `conn:…/projects/ai-project/connections/storage-conn`
    (`c` < `p`);
  - `external:…srch-shared`, `host:default`, `model:ai-project:gpt-4o-mini`,
    `svc:ai-project`, `svc:assistant`.

  This gives n1–n13 as emitted.
- **Origins.**
  - The role assignment (`Microsoft.Authorization/*`) and the `assistant` agent
    service are `unobserved`.
  - `srch-sample` is a top-level type, so it stays `local`.
  - The runtime project merges into `svc:ai-project` and becomes `both`.
  - The deployment merges into `model:…`, `both`, with health `running`.
  - `search-conn` is account-scoped (it hangs off the account). Its target is in
    `rg-shared`, outside the scope, so it is drawn `external`.
  - `storage-conn` targets the correlated `stsample` by ARM ID.
- **Edge order.** The 13 edges sort by from-key, to-key and kind exactly as
  listed.
- **Classes.** The class lines follow the `classNames` order.
- **Findings.** The ARM-ID finding lands on `stsample`. The baselined type/name
  finding lands on the role node.

The environment-diff sample is also consistent: identity keys sort
`foundry-account < identity < monitoring < search < storage`, Search is
`added`, `appi` is `changed`, and the storage `usesIdentity` edge is `removed`.

## New findings

### Blocking

#### B1 — `--deployed` silently drops in-scope dangling Azure references, a regression from the loop-1 `Options.Scope` change
- Where: `internal/graph/view/view.go` `filterRaw`. The `ModeDeployed` keep-set is
  `azure | both | external | collapsed` and does not include `missing`.
- Before loop 1, every unresolved ARM-ID reference was `external`, so it survived
  deployed mode.
- Loop 1 (FL-N7) changed this. With `Options.Scope` set (always set for
  deployed and combined), an **in-scope** ARM ID that matches no node is now
  `missing`. Deployed mode then deletes the missing node and its edge.
- The references affected are exactly the Azure-side broken-dependency signals a
  deployed graph exists to show:
  - a resource whose `identity.userAssignedIdentities` names a user-assigned
    identity that no longer exists in the resource group (`addIdentityEdges`);
  - a Foundry connection whose `ResourceId` names a deleted in-scope storage,
    Search or Cosmos account (`addRuntimeNodes` `targets`).
- The `Resources - List` inventory for the scope is complete (truncation already
  fails the command). An unmatched in-scope top-level ID is therefore real
  evidence of a dangling reference.
- In deployed mode, source is never built (`opt.Mode != ModeDeployed`, and
  `app.Graph` does not load source). So every `missing` node in deployed mode
  comes from Azure-side evidence and none is a source-only artefact.
- The PRD §13 purpose is to highlight "missing, external, baselined and unhealthy
  nodes". Every new test exercises `ModeCombined` only, so this regression is
  untested.
- Fix:
  - Add `n.origin == OriginMissing` to the `ModeDeployed` keep-set in `filterRaw`.
  - Add a deployed-mode test: an inventory resource whose user-assigned identity
    is in scope but absent, and a runtime connection whose `ResourceId` is in
    scope but absent. Assert that both `missing` nodes and their edges
    (`Missing: true`) are present.

### Non-blocking

- **N1 — The shared-adapter decode is strict.**
  - The new `Identity listIdentity` field sits in the per-item struct of
    `ListResources`. Any resource whose `identity` or `userAssignedIdentities`
    is not a JSON object makes the whole call fail with `azure: decode resource`.
    That breaks every `ListResources` caller, not only `graph`.
  - The documented shape is an object, so the risk is low. But this data is a
    best-effort hint that is not verified for 2025-04-01. Decode `identity` as
    `json.RawMessage` and ignore its decode errors.
- **N2 — Runtime connection target precedence.**
  - In `addRuntimeNodes`, `case looksExternal(c.Target.Endpoint)` runs before the
    in-scope `ResourceID` check. A connection whose `ResourceId` is in scope but
    unmatched, and which also has an `https://` endpoint, is drawn `external-url`
    instead of `missing`.
  - This contradicts the loop-1 FL-N7 semantics. Evaluate the `ResourceID` branch
    (with `isExternalRef`) before the endpoint branch.
- **N3 — In-scope references whose types the inventory cannot list.**
  - `isExternalRef` makes every in-scope unmatched ARM ID `missing`, including
    child or proxy IDs that `Resources - List` never returns. An example is a
    connection `ResourceId` naming a sub-resource. Such resources may well exist.
  - Apply the `inventoryObservable` rule (type with more than one `/`, or
    `notInResourceList`) and draw these as `unobserved` or `external`, not
    `missing`.
- **N4 — Cross-scope module deployments read as "not deployed".**
  - In combined mode, a template resource deployed by a module into another
    resource group or subscription, outside `--resource-group`, stays `local`.
    That reads as "not deployed".
  - The ARM placements know the module scope. Either mark out-of-scope
    placements `unobserved`, or document the limitation in `docs/graph.md`.
- **N5 — Diff of redacted inputs.**
  - When the `--diff-base/--diff-target` files were rendered with
    `--redact-ids`, `identityKeys` falls back to the hashed label. That hash
    includes the ARM ID, so every node becomes `added` or `removed`.
  - Reject inputs whose names are all empty with a usage error, or document
    "diff un-redacted JSON, then pass `--redact-ids` to the diff".
- **N6 — Wrong `--project` or `--account` gives a generic error.**
  - A 404 from an overlay call is an `APIError` (not `ErrUnavailable`), so it
    aborts the command with a generic error.
  - Map 404 on `GetProject` or account lists to a usage error naming the bad
    `--project`/`--account`.

## PRD §13 DoD — round-2 assessment

| DoD item | Verdict |
|---|---|
| Graph without Azure login from source | Met (by inspection) |
| Combined view distinguishes local, Azure and external | Met for combined mode by inspection. Deployed mode hides in-scope dangling references (B1). |
| No secrets or connection credentials | Met by inspection (metadata-only connections, `TestGraphNeverEmitsConnectionSecrets`) |
| Deterministic rendering order | Met by inspection (sorted keys; hand-traced sample matches) |

## Summary

Round-1 findings: B1–B5 closed (B3 partly implemented, remainder ADR-027
deferred and disclosed). N1–N8 and N10 closed; N9 partially closed.

New: 1 blocking (B1, a one-line keep-set fix plus a test), 6 non-blocking
(N1–N6).

VERDICT: REJECT
