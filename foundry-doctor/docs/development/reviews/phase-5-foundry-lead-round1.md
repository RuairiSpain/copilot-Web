# Phase 5 review: Foundry lead, round 1

Reviewer: `foundry-lead` (independent; did not write the code). Date: 2026-10-18. Round 1. Verdict: **REJECT**.

Scope: PRD section 13 (Phase 5, dependency and deployment graph outputs). Files: `internal/graph/**`, `internal/report/graph/**`, `internal/app/graph_cmd*.go`, `cmd/foundry-doctor/graph*.go`, `schemas/graph.schema.json`, `docs/graph.md`, `samples/graph/`, `docs/development/hand-offs/phase-5.md`.

Review conditions:
- This is the first recorded review for Phase 5. `docs/development/reviews/` contains no Phase 5 file, but the hand-off says a "final signoff review" found no blocking issues (see B6).
- I could not open `/tmp/fd/REVIEW_BRIEF.md`. This environment does not allow file operations under `/tmp`. I reviewed the source tree in `copilot-Web-main (5).zip`. The Phase 5 files I compared byte for byte match the working copy. I used the output order defined by the persona file.
- I did not run any tests or gates. The Go toolchain (go.mod needs `go 1.26.9`) could not be installed here because there is no network or package access. Every finding comes from reading the code statically against the PRD and primary documentation.

## 1. Blocking findings

**B1. The environment-difference graph is not implemented, and the hand-off does not say so.**
- Where: `internal/graph/view` (there is no environment-diff mode), `cmd/foundry-doctor/graph.go` (flags are only `--source`, `--deployed` and `--combined`), and the hand-off.
- Why it is wrong: PRD section 13, Functional scope, requires "Implement environment-difference graph." The hand-off marks every DoD row as met and lists nothing as deferred. No ADR changes the requirement.
- Fix: either implement it (for example `graph --diff <envA> <envB>`, which projects two graphs and marks added, removed and changed nodes, with tests), or record an ADR that defers it and add the gap to the hand-off "Deferred issues" section.

**B2. Mermaid node styling for unhealthy and baselined nodes uses an undocumented form, and no test renders the output.**
- Where: `internal/graph/mermaid/render.go`, `classifyNode`, which emits `class n2 both,unhealthy`. The same pattern is in the golden file `testdata/graph.mmd.golden`.
- Why it is wrong: Mermaid documents the comma in a `class` statement as a separator between node IDs (`class A,B,C myStyle`), not between class names. Source: https://mermaid.ai/docs/style-and-customize/create-custom-styles and https://mermaid.js.org/syntax/flowchart.html#classes. The emitted line is therefore likely read as one class named `both,unhealthy`, which matches no `classDef`. The node would then lose both its origin colour and its unhealthy or baselined highlight. Highlighting "missing, external, baselined and unhealthy nodes" is the stated purpose of the phase (PRD section 13, Purpose and context).
- I could not render the output in this environment, so the visual result is unverified. However, the syntax is not the documented form, and the golden test only compares strings.
- Fix: emit one `class <id> <name>` statement per class, or one statement per class listing many node IDs. Add a test that parses the output with a Mermaid parser (for example mermaid-cli in CI behind a build tag), or at least asserts that every class token is a defined `classDef`.

**B3. Escaped Mermaid labels are invalid when a label contains `"`, and the injection test cannot catch this.**
- Where: `mermaid.Render` (`%s[%q]`) and `escapeLabel`.
- Why it is wrong: `escapeLabel` turns `"` into `\"`, then `%q` escapes it again, producing `\\\"`. Mermaid quoted labels do not support backslash escapes; quotes must be written as entity codes such as `#quot;`. Source: https://mermaid.js.org/syntax/flowchart.html#entity-codes-to-escape-characters. The input `x"]-->evil` is emitted as `n1["x\\\")→evil"]`, and the string ends at the first unescaped `"`. That is a syntax error, not a safe label.
- `TestRenderEscapesInjection` only checks that some substrings are absent, so it passes on invalid output. The DoD row "Untrusted labels ... safely escaped" is claimed as met but is not demonstrated.
- Fix: do not use `%q`. Write `"` + label + `"`, encode `"`, `#`, `<`, `>`, `` ` `` and `|` as Mermaid entity codes, and add a parse-validity test for hostile labels.

**B4. The combined view reports child and extension resources as local-only, which is false drift.**
- Where: `app.Graph` inventory collection (`Inventory.ListResources`, which is the ARM `.../resources` list, source `arm-list`), `view.Build` correlation, and `filterRaw`.
- Why it is wrong: the ARM Resources list returns the resources in the group. Source: https://learn.microsoft.com/rest/api/resources/resources/list-by-resource-group.
  - Extension resources such as `Microsoft.Authorization/roleAssignments` are listed through their own API (https://learn.microsoft.com/rest/api/authorization/role-assignments/list-for-scope), not through this one.
  - Proxy child types such as account deployments, connections, capability hosts and subnets are read from their parent resource. Foundry deployments, connections and capability hosts are only re-added when `--account` or `--project` is given.
  - So every Bicep role assignment, subnet or private DNS zone link, and, without the runtime flags, every deployment, connection and capability host appears as `local` ("present only in local source inputs", `docs/graph.md`) even when it is deployed.
- Effect: this breaks the PRD DoD item "Combined view correctly distinguishes local, Azure and external nodes".
- Fix: for resource types the inventory source cannot observe, mark the node as "not observed" (a distinct status or `unknownDeployed: true`), not `local`. Alternatively, fetch those types. Document the inventory coverage in `docs/graph.md` and add a test.

**B5. The sample diagram is not reproducible from its fixture and carries a wrong relationship.**
- Where: `samples/graph/bicep-backed.md`.
- Why it is wrong: the sample shows a model deployment node labelled `storageAccountName (OpenAI / gpt-4o-mini / 2024-07-18)`. `samples/bicep-backed/azure.yaml` declares no `deployments`. Source model nodes come only from service-level `deployments` (`addDeploymentNodes`), which is consistent with the vendored `azure.ai.project.json` and `azure.ai.agent.json` schemas (`properties.deployments`). The node therefore cannot be produced from this fixture, and a deployment named after a storage parameter is semantically wrong.
- The "bicep-backed" sample also shows no Bicep resources at all.
- Only two source-mode samples exist. PRD section 13, Required deliverables, asks for "Sample diagrams for supported architectures". There is no deployed, combined, private-network or external-reference sample.
- Fix: generate the samples with the CLI from their fixtures, add a test (golden, or a `go generate` check) that fails if a sample drifts, and add combined, private and external examples using recorded inventory fixtures.

**B6. The hand-off evidence is not reliable.**
- The "Foundry lead review" and "Azure black belt review" sections claim final sign-off, but no review file exists in `docs/development/reviews/` for Phase 5. Remove the claims and point to this round.
- "Compatibility matrix: validated ... with Go `1.25.13` and `GOTOOLCHAIN=local`" cannot be true. `go.mod` and `go.work` declare `go 1.26.9`, and a 1.25 toolchain with `GOTOOLCHAIN=local` refuses to build that module. Source: https://go.dev/doc/toolchain. The recorded gate results therefore cannot have come from the stated toolchain. Re-run the gates and record the exact `go version` output.
- The PRD testing strategy items are listed as done but are missing (see section 4): the large-graph performance test, disconnected-node graph tests, golden tests for JSON, DOT and Markdown, and the optional adapter absence test at command level.

## 2. Non-blocking findings

- **N1. Stable diffs.** Node IDs `n1..nN` are positional over sorted keys. Adding one resource renumbers every later node and rewrites every edge line. The output is deterministic but not diff-stable, which is the PRD goal ("deterministic for stable diffs"). Use stable IDs derived from a short hash of the key, which stay opaque.
- **N2. Collapse order.** Collapse is applied by sorted key prefix (`arm:` < `az:` < … < `svc:`), and `--collapse-after` defaults to 80. On larger graphs, the azure.yaml agents, projects and connections (`svc:`) are collapsed first while raw ARM nodes are kept. Prioritise Foundry nodes (project, agent, model, connection, capability host) and collapse leaf infrastructure first.
- **N3. `--max-nodes` is not a hard cap.** Collapse buckets are added after the limit, so output can exceed `--max-nodes`. Either make it a hard cap or document that it is a soft limit.
- **N4. Overlay failures abort the whole graph.** A 403 on capability hosts or connections aborts the graph (`graphOverlayError`), and generic errors map to an internal exit code. The PRD says rendering must not block core execution. Degrade instead: render without that overlay and add a summary note that the overlay was skipped because of permissions.
- **N5. Project and account connections with the same name.** After `mergeFoundryConnections`, `connByName` is overwritten by the account connection. A project capability host's `*Connections` names can then link to the account connection instead of the project connection. Prefer the project-scoped connection. Capability host and connection relationships: https://learn.microsoft.com/azure/ai-foundry/agents/concepts/capability-hosts.
- **N6. Agent `connections` property ignored.** `azure.ai.agent` declares `connections` (prompt-only references to sibling `azure.ai.connection` services; vendored schema `azure.ai.agent.json`), but the projection ignores it. Agent-to-connection edges therefore appear only when they are repeated in `uses`.
- **N7. Over-broad `external` label.** `looksExternal` treats any `/subscriptions/...` ID as external, including IDs inside the graphed subscription or resource group that are simply absent, which should be `missing`. Compare with the graphed scope before labelling a reference external.
- **N8. DOT output loses information.** DOT carries no origin, health or baselined styling and no legend. `Binary.Render` (SVG/PNG) exists but is not reachable from the CLI. `CombinedOutput` would also mix `dot` stderr warnings into the image bytes; use `Output` and keep stderr separate.
- **N9. HTML output.** The HTML wrapper is a `<pre>` of the Markdown. It shows no rendered graph and no node or edge list. That is acceptable as "optional", but the accessibility guidance should add Mermaid `accTitle`/`accDescr` (https://mermaid.js.org/config/accessibility.html) and a text table of nodes and edges.
- **N10. Colour-only distinctions.** `unhealthy` and `missing` share the same fill, and `unhealthy` has no non-colour cue, contrary to WCAG 1.4.1 (https://www.w3.org/WAI/WCAG22/Understanding/use-of-color.html). The Markdown legend omits the unhealthy, baselined and collapsed states.
- **N11. Schema gaps.** `schemas/graph.schema.json` omits `conditional`, and the `origin` enum includes `collapsed`, although the docs say buckets keep their underlying origin. No test validates real output against the schema; `TestRenderMatchesSchemaEnvelope` only checks that the file exists.
- **N12. Pseudonym strength.** `--redact-ids` uses unkeyed FNV-64 over name, ID and label. Guessable Azure names can be reversed by dictionary attack. The hand-off acknowledges this; the docs should state it plainly.
- **N13. Inventory scope.** Deployed mode with only `--subscription` lists the whole subscription. It fails as unavailable above 2000 resources, and with no relationship edges it produces a mostly disconnected node list. Recommend requiring `--resource-group`, or filtering to Foundry-related types.

## 3. Unsupported assumptions

- The connection metadata key `ResourceId` as the backing resource. `internal/azure/api.go` itself marks this UNVERIFIED. `targets` edges rest on it.
- The `deploymentState` values (`running`/`ready` treated as healthy, anything else, such as "paused", treated as unhealthy). I found no source for the value domain in the ARM deployment schema used here. Cite the REST spec version or treat unknown values as "unknown", not "unhealthy".
- Merging a runtime project with an azure.yaml `azure.ai.project` service by service name (`mergeExistingNode` uses name and last segment). The azd service key is not necessarily the Foundry project name.
- That findings carry `Resource.ID` values that match graph node `resourceID`s. Source nodes use template pointers or `/resources/N`, so most findings may be unmatched. Only `unmatchedFindings` reveals this.
- The Mermaid rendering behaviour in B2 and B3 has not been verified by rendering (see the review conditions).

## 4. Missing tests

- Large-graph performance test (PRD section 13). There is no benchmark or size test.
- Disconnected-node test at the view or render level. Only `internal/graph` covers cycles.
- Golden tests for JSON, DOT, Markdown and HTML. Only Mermaid has a golden.
- Validation of JSON output against `schemas/graph.schema.json`.
- A Mermaid parse-validity test for hostile labels and multi-class nodes (B2, B3).
- A combined-mode test with Bicep role assignments, subnets and deployments, without runtime flags (B4).
- Sample reproducibility test (B5).
- A secret-redaction test for connection targets: a URL with a SAS or `api-key` query string, and azure.yaml connection `credentials`, asserting that nothing reaches any format.
- A partial-permission overlay test (403 on hosts or connections) (N4).
- An optional-adapter absence test at command level (`--format svg` with no `dot`). This is currently unreachable.

## 5. Verdict: **REJECT**

Unmet PRD section 13 and phase DoD items:

| Item | Status |
|---|---|
| Implement environment-difference graph (Functional scope) | Not met (B1) |
| Combined view correctly distinguishes local, Azure and external nodes | Not met (B4, and N7 contributes) |
| Highlights missing, external, baselined and unhealthy nodes; untrusted labels safely escaped (hand-off DoD) | Not demonstrated (B2, B3) |
| Sample diagrams for supported architectures | Not met (B5) |
| Testing strategy: large-graph performance, disconnected-node and golden tests | Not met (section 4) |
| Command wiring, docs, samples and gates complete (hand-off DoD) | Not demonstrated (B6) |
| A graph can be generated without Azure login from source | Met by inspection (`TestGraphSourceOffline`); not executed by me |
| Graphs do not expose secrets or connection credentials | Largely met by inspection; the test gap is listed in section 4 |
