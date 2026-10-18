<!-- Generated from samples/graph/fixtures/combined.json by TestSamples in internal/report/graph; regenerate with `go test ./internal/report/graph -run TestSamples -update`. -->

# Foundry Doctor graph (combined)

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

- Nodes: 13
- Edges: 13

```mermaid
flowchart LR
  accTitle: Foundry Doctor combined graph
  accDescr: 13 nodes and 13 edges, styled by origin and status as described in the legend
  n1["ra-storage-blob"]
  n2["aif-sample"]
  n3["id-sample"]
  n4["srch-sample"]
  n5["stsample"]
  n6["appi-sample"]
  n7["search-conn"]
  n8["storage-conn"]
  n9["external:srch-shared"]
  n10["default"]
  n11["gpt-4o-mini (OpenAI / gpt-4o-mini / 2024-07-18)"]
  n12["ai-project"]
  n13["assistant"]
  n1 -.->|dependsOn| n3
  n1 -.->|dependsOn| n5
  n2 -->|usesIdentity| n3
  n2 -->|uses| n7
  n2 ==>|deploys| n11
  n2 -->|contains| n12
  n7 -->|targets| n9
  n8 -->|targets| n5
  n10 -->|uses| n8
  n12 -->|uses| n8
  n12 -->|hosts| n10
  n12 ==>|deploys| n11
  n13 -->|uses| n12
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
  class n6,n7,n8,n10 azure
  class n1 baselined
  class n2,n3,n5,n11,n12 both
  class n9 external
  class n4 local
  class n6 unhealthy
  class n1,n13 unobserved
```

## Nodes

| ID | Label | Kind | Classes | Details |
|---|---|---|---|---|
| n1 | ra-storage-blob | role | unobserved, baselined | baselined 1 |
| n2 | aif-sample | foundry-account | both | health succeeded |
| n3 | id-sample | identity | both | health succeeded |
| n4 | srch-sample | search | local |  |
| n5 | stsample | storage | both | health succeeded, findings 1 (max warning) |
| n6 | appi-sample | monitoring | azure, unhealthy | health failed |
| n7 | search-conn | connection | azure |  |
| n8 | storage-conn | connection | azure |  |
| n9 | external:srch-shared | external | external |  |
| n10 | default | capability-host | azure | health succeeded |
| n11 | gpt-4o-mini (OpenAI / gpt-4o-mini / 2024-07-18) | model | both | health running |
| n12 | ai-project | project | both | health succeeded |
| n13 | assistant | agent | unobserved |  |

## Edges

| From | To | Kind | Details |
|---|---|---|---|
| n1 | n3 | dependsOn |  |
| n1 | n5 | dependsOn |  |
| n2 | n3 | usesIdentity |  |
| n2 | n7 | uses |  |
| n2 | n11 | deploys |  |
| n2 | n12 | contains |  |
| n7 | n9 | targets | external |
| n8 | n5 | targets |  |
| n10 | n8 | uses |  |
| n12 | n8 | uses |  |
| n12 | n10 | hosts |  |
| n12 | n11 | deploys |  |
| n13 | n12 | uses |  |
