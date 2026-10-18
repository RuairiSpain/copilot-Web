<!-- Generated from samples/graph/fixtures/private-network.json by TestSamples in internal/report/graph; regenerate with `go test ./internal/report/graph -run TestSamples -update`. -->

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

- Nodes: 8
- Edges: 6

```mermaid
flowchart LR
  accTitle: Foundry Doctor source graph
  accDescr: 8 nodes and 6 edges, styled by origin and status as described in the legend
  n1["aif-sample"]
  n2["privatelink.cognitiveservices.azure.com/link-vnet"]
  n3["privatelink.cognitiveservices.azure.com"]
  n4["pe-aif-sample"]
  n5["vnet-sample/snet-pe"]
  n6["vnet-sample"]
  n7["ai-project"]
  n8["assistant"]
  n2 -.->|dependsOn| n3
  n2 -.->|dependsOn| n6
  n4 -.->|dependsOn| n1
  n4 -.->|dependsOn| n5
  n5 -.->|dependsOn| n6
  n8 -->|uses| n7
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
  class n1,n2,n3,n4,n5,n6,n7,n8 local
```

## Nodes

| ID | Label | Kind | Classes | Details |
|---|---|---|---|---|
| n1 | aif-sample | foundry-account | local |  |
| n2 | privatelink.cognitiveservices.azure.com/link-vnet | network | local | conditional |
| n3 | privatelink.cognitiveservices.azure.com | network | local |  |
| n4 | pe-aif-sample | network | local |  |
| n5 | vnet-sample/snet-pe | network | local |  |
| n6 | vnet-sample | network | local |  |
| n7 | ai-project | project | local |  |
| n8 | assistant | agent | local |  |

## Edges

| From | To | Kind | Details |
|---|---|---|---|
| n2 | n3 | dependsOn |  |
| n2 | n6 | dependsOn |  |
| n4 | n1 | dependsOn |  |
| n4 | n5 | dependsOn |  |
| n5 | n6 | dependsOn |  |
| n8 | n7 | uses |  |
