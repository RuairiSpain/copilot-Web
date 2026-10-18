<!-- Generated from samples/graph/fixtures/environment-diff.json by TestSamples in internal/report/graph; regenerate with `go test ./internal/report/graph -run TestSamples -update`. -->

# Foundry Doctor graph (diff)

| Class | Meaning | Non-colour cue |
|---|---|---|
| local | Declared in source; not in the deployed inventory (combined mode) or source-only (source mode) | solid outline |
| azure | Present in deployed Azure state only | solid outline |
| both | Declared in source and correlated with deployed Azure state | solid outline |
| unobserved | Declared in source, or referenced in scope, with a type the deployed inventory cannot observe, so its deployment is unknown | dotted outline |
| external | Referenced outside the graphed scope | solid outline |
| missing | Referenced but unresolved: a name that matches nothing, or an in-scope ARM ID of an observable type the inventory did not return | dashed outline |
| collapsed | Summary bucket for nodes over the size limit | dashed outline |
| unhealthy | Failed or canceled provisioning, connection error, or an error finding | thick outline |
| baselined | Has baselined findings | fine-dashed outline |
| added, removed, changed | Diff graph only: node state of target compared with base | thick or long-dashed outline |

- Nodes: 5
- Edges: 2
- Added nodes: 1
- Removed nodes: 0
- Changed nodes: 1

```mermaid
flowchart LR
  accTitle: Foundry Doctor diff graph
  accDescr: 5 nodes and 2 edges, styled by origin and status as described in the legend
  n1["aif-sample"]
  n2["id-sample"]
  n3["appi-sample"]
  n4["srch-sample"]
  n5["stsample"]
  n1 -->|usesIdentity| n2
  n5 -->|usesIdentity removed| n2
  classDef added fill:#dcfce7,stroke:#166534,color:#111827,stroke-width:3px
  classDef azure fill:#dcfce7,stroke:#166534,color:#111827
  classDef baselined fill:#f3e8ff,stroke:#7e22ce,color:#111827,stroke-dasharray: 2 2
  classDef both fill:#ede9fe,stroke:#6d28d9,color:#111827
  classDef changed fill:#fef9c3,stroke:#854d0e,color:#111827,stroke-width:3px
  classDef collapsed fill:#e5e7eb,stroke:#374151,color:#111827,stroke-dasharray: 3 3
  classDef external fill:#fef3c7,stroke:#92400e,color:#111827
  classDef local fill:#dbeafe,stroke:#1d4ed8,color:#111827
  classDef missing fill:#fee2e2,stroke:#991b1b,color:#111827,stroke-dasharray: 5 5
  classDef removed fill:#f3f4f6,stroke:#6b7280,color:#111827,stroke-dasharray: 8 4
  classDef unhealthy fill:#fee2e2,stroke:#991b1b,color:#111827,stroke-width:4px
  classDef unobserved fill:#f1f5f9,stroke:#475569,color:#111827,stroke-dasharray: 1 3
  class n4 added
  class n1,n2,n3,n4,n5 azure
  class n3 changed
```

## Nodes

| ID | Label | Kind | Classes | Details |
|---|---|---|---|---|
| n1 | aif-sample | foundry-account | azure | health succeeded |
| n2 | id-sample | identity | azure | health succeeded |
| n3 | appi-sample | monitoring | azure, changed | health succeeded |
| n4 | srch-sample | search | azure, added | health succeeded |
| n5 | stsample | storage | azure | health succeeded |

## Edges

| From | To | Kind | Details |
|---|---|---|---|
| n1 | n2 | usesIdentity |  |
| n5 | n2 | usesIdentity | removed |
