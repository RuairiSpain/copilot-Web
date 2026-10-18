<!-- Generated from samples/graph/fixtures/external-references.json by TestSamples in internal/report/graph; regenerate with `go test ./internal/report/graph -run TestSamples -update`. -->

# Foundry Doctor graph (source)

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

- Nodes: 6
- Edges: 4

```mermaid
flowchart LR
  accTitle: Foundry Doctor source graph
  accDescr: 6 nodes and 4 edges, styled by origin and status as described in the legend
  n1["external:stshared"]
  n2["external-url"]
  n3["shared-search"]
  n4["ai-project"]
  n5["assistant"]
  n6["docs-conn"]
  n5 -->|uses| n1
  n5 -->|uses| n3
  n5 -->|uses| n4
  n6 -->|targets| n2
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
  class n1,n2 external
  class n4,n5,n6 local
  class n3 missing
```

## Nodes

| ID | Label | Kind | Classes | Details |
|---|---|---|---|---|
| n1 | external:stshared | external | external |  |
| n2 | external-url | external | external |  |
| n3 | shared-search | missing | missing |  |
| n4 | ai-project | project | local |  |
| n5 | assistant | agent | local |  |
| n6 | docs-conn | connection | local |  |

## Edges

| From | To | Kind | Details |
|---|---|---|---|
| n5 | n1 | uses | external |
| n5 | n3 | uses | missing |
| n5 | n4 | uses |  |
| n6 | n2 | targets | external |
