# Runtime diagnosis (`foundry-doctor runtime`)

`runtime` runs the `FND-RUN-001`..`FND-RUN-007` rules against a deployed
Foundry project. It combines:

- ARM management metadata reads
- metadata-only Foundry project endpoint reads
- metadata-only Azure AI Search reads
- aggregate Azure Monitor and Log Analytics counts
- optional DNS resolution from a declared network vantage point

It **never** retrieves document bodies, prompts, completions, tool outputs, or
connection secrets.

Readiness is **advisory**. A `ready` result means no runtime rule found a
problem; it never proves the deployment is healthy end-to-end.

## Probe safety contract

| Class | Meaning |
|---|---|
| `safe-local` | Local-only evaluation with no Azure call. |
| `control-plane` | ARM metadata read. |
| `data-plane` | Metadata-only service endpoint read. Content retrieval is forbidden by interface. |
| `vnet-only` | A probe that only makes sense from inside the target VNet. When no valid vantage point exists it is skipped, never passed. |

## Outcomes

| Outcome | Meaning |
|---|---|
| pass | The check ran and found no problem. |
| fail (blocked) | A finding naming the resource or configuration that failed. |
| skipped | The check could not run because input, permission, or vantage point was unavailable. Never counted as pass. |
| uncertain | The probe ran but could not prove the condition either way. Never counted as pass. |

## Rule evidence notes

- **FND-RUN-001** inspects project identity RBAC on bound Search, Storage, and
  Cosmos targets. Key, SAS, and service-principal connections are skipped
  because the command does not read credentials.
- **FND-RUN-002** validates private DNS resolution only when `--vantage vnet`
  is declared and the command runs from that network vantage point.
- **FND-RUN-003** checks project, capability host, and connection metadata for
  failed or non-terminal provisioning state and surfaced connection errors.
- **FND-RUN-004** checks deployment provisioning state and aggregate Azure
  Monitor signals such as HTTP 429s and provisioned utilization.
- **FND-RUN-005** reads only agent/version metadata and project connection
  listings from the Foundry project endpoint.
- **FND-RUN-006** reads only Search index definitions, statistics, and indexer
  status; no documents are queried.
- **FND-RUN-007** uses diagnostic-setting metadata plus aggregate log counts;
  it does not read raw log lines.

## Flags

| Flag | Purpose |
|---|---|
| `--subscription`, `--resource-group` | Azure target. Default to `AZURE_SUBSCRIPTION_ID` and `AZURE_RESOURCE_GROUP`. |
| `--account`, `--project` | Foundry account and project names. |
| `--vantage` | `none`, `local`, or `vnet`. `vnet` enables DNS probes that require private resolution from inside the VNet. |
| `--timeout` | Per-probe timeout. |
| `--strict` | Exit 3 when any check was skipped. |

## Least-privilege notes

The exact ARM actions are tracked in
[permissions-matrix.md](permissions-matrix.md). Runtime data-plane probes use
Microsoft Entra bearer tokens via `azd auth token` / `az account
get-access-token`; they do not fall back to keys.

Use a dedicated read-only identity where possible. Missing permissions degrade
to skipped checks that name the exact capability and action.
