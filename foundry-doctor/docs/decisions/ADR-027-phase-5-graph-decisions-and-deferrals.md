# ADR-027: Phase 5 graph decisions and deferrals

Status: Accepted
Date: 2026-10-18
Decided by: the Phase 5 go-implementer, fix loop 1, from the Phase 5 round 1 reviews
(`docs/development/reviews/phase-5-foundry-lead-round1.md`, `phase-5-azure-black-belt-round1.md`). Pending re-review.

## Context

Both round 1 reviews rejected Phase 5 (PRD section 13). Some findings are fixed in code in fix loop 1. This ADR records the
design choices those fixes make and the parts that are deferred, so that no finding is silently dropped.

## Decisions

1. **Environment-difference graph (Foundry lead B1): implemented, file-based.** `foundry-doctor graph --diff-base <a.json>
   --diff-target <b.json>` compares two graph JSON files rendered with `--format json` (for example `--deployed` for two resource
   groups, or `--source` before and after a change). Output mode `diff`. Nodes are matched by kind, type and name (the label when the
   name is empty), never by ARM ID, because IDs differ between environments by design. Nodes only in the target are `added`, only in
   the base are `removed`, and in both with a different origin, health, finding, baselined, missing, external or conditional state are
   `changed`; edges are `added` or `removed`. Limitations: resources whose names embed the environment name do not match and appear
   as one removed and one added node; a live two-environment mode (two `--subscription`/`--resource-group` pairs in one command) is
   not provided; collapse limits are not applied to diff output.
2. **Combined-mode correlation keeps the ARM ID (Azure black belt B1, B2).** A source node never carries its template pointer as
   `resource`; on an inventory match the node takes the deployed ARM ID. The runtime account is resolved by ARM ID, then by name,
   like the project, so a source-declared account is one `both` node.
3. **Unobserved origin (Foundry lead B4).** In combined mode a source node whose type the ARM Resources - List inventory cannot
   observe is `unobserved`, not `local`: proxy child types (two or more `/` in the type, for example subnets, account deployments,
   connections, capability hosts), the extension types `Microsoft.Authorization/*`, `Microsoft.Insights/diagnosticSettings` and
   `Microsoft.Resources/deployments`, and azure.yaml services. A type is observable when the inventory returned at least one
   resource of that type, or when a runtime overlay call for that type succeeded. The extension list is conservative
   (role assignments have their own list API: https://learn.microsoft.com/rest/api/authorization/role-assignments/list-for-scope,
   read 2026-10-18); a top-level type not on the list is assumed observable.
4. **Deployed relationship edges (Azure black belt B3): partly implemented, partly deferred.**
   - Implemented: resource -> user-assigned identity edges (`usesIdentity`) from the keys of `identity.userAssignedIdentities` in the
     Resources - List response (GenericResourceExpanded; https://learn.microsoft.com/rest/api/resources/resources/list, read
     2026-10-18, page version 2021-04-01). The adapter calls 2025-04-01; that the shape is unchanged in 2025-04-01 is UNVERIFIED, and
     a missing field only yields no edge. No new permission or API call.
   - Deferred: account -> private endpoint edges. `GetAccountNetwork` returns only counts of private endpoint connections; drawing
     edges needs the adapter to return `properties.privateEndpointConnections[].properties.privateEndpoint.id`, a shared-adapter change
     with its own fixture tests. Deferred to Phase 8, which already owns live private-endpoint evidence (ADR-023), or a later ADR.
   - Deferred: deployed role-assignment nodes and identity -> role edges (needs the Authorization list API on the ADR-006
     allow-list), and deployed subnet nodes (child resources). Until then these appear only from source and, in combined mode, as
     `unobserved`. `docs/graph.md` says so.
5. **Bicep-backed CLI sample (both reviews B5).** The old `samples/graph/bicep-backed.md` could not be produced from its fixture and
   is removed. Reproducing a Bicep sample through the CLI needs the Bicep CLI, which tests cannot assume. Instead the graph samples
   are generated from recorded fixtures under `samples/graph/fixtures/` (template resources recorded as the flattened ARM model
   produces them, a Resources - List inventory and a runtime overlay) by `TestSamples` in `internal/report/graph`, which fails on
   drift. `samples/graph/azure-yaml-only.mmd` is checked against the CLI output by `TestGraphSampleAzureYAMLOnly`.
6. **Health semantics (Azure black belt N2).** Only the ARM terminal failure states `Failed` and `Canceled` mark a node unhealthy.
   Transient states and `deploymentState: Paused` are shown as health text only (Paused is an operator choice:
   https://learn.microsoft.com/rest/api/microsoftfoundry/accountmanagement/deployments/pause, as cited by the reviewer; not re-read
   in this loop).
7. **Not changed in this loop.** Node IDs stay positional (`n1..nN` over sorted keys; Foundry lead N1): output is deterministic but
   one added node renumbers later IDs; hash-based IDs are deferred because every golden and sample would change again.
   `--max-nodes` stays a soft limit (collapse buckets are added on top) and is documented as such (N3). Agent `connections`
   (N6) are not projected. `--redact-ids` stays an unkeyed FNV-64 pseudonym (N12); the docs now state it is not a security control.

## Consequences

- The Phase 5 hand-off lists items 4 (deferred parts), 5 and 7 under known limitations and deferred issues.
- The Phase 8 hand-off must either draw account -> private endpoint edges or carry this deferral on with a new dated ADR.

## Amendment, fix loop 2 (2026-10-18)

From the round 2 reviews (`phase-5-foundry-lead-round2.md` B7, `phase-5-azure-black-belt-round2.md` B1):

8. **Unresolved references follow the same observability rule as declared nodes.** A reference that matches no node is
   `external` outside the inventory scope; inside the scope it is `missing` only when its type is observable by the inventory
   (top-level, not on the extension list, or returned by the inventory), and otherwise `unobserved` (a node carrying the ARM ID;
   a runtime overlay resource with that ID merges into it). Source references are classified after the inventory merge, so a
   reference to a deployed resource resolves to its node. A runtime connection's `ResourceId` is classified before its endpoint URL.
9. **`--deployed` keeps `missing` and `unobserved` nodes.** Deployed mode never builds source, so these nodes come only from Azure
   evidence (deleted identities or connection targets, or child resources the inventory cannot list) and are the dangling
   references a deployed graph exists to show.
10. **The shared inventory decode is tolerant.** `identity` is decoded as raw JSON and parsed separately; any unexpected shape
    yields no user-assigned identity IDs instead of failing `ListResources` for every caller.
