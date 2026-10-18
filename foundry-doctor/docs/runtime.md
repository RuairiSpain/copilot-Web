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

The catalogue (`rules/catalog/run/`, `docs/rule-catalog.md`) is authoritative; these notes summarise it. Each rule's
behaviour changed in Phase 3 gate fix loop 1 (all rules at least one version higher); see
`docs/development/hand-offs/phase-3.md` for what is delivered and what is still in remediation.

- **FND-RUN-001** checks the project identity (system-assigned and each user-assigned principal) on bound Search,
  Storage and Cosmos targets, scope-aware and role-definition-aware. Cosmos is matched by `roleDefinitionId` and scope.
  Custom roles and container-scoped Blob grants are uncertain. Group-based assignments and deny assignments are not
  evaluated, so a fail is reported as 'likely'. Key, SAS and service-principal connections are skipped because the
  command does not read credentials.
- **FND-RUN-002** validates private DNS resolution only when `--vantage vnet` is declared. The vantage point is
  asserted by the operator; the doctor cannot verify where it runs and says so. A fail needs a dependency proven
  private-endpoint-backed; a public answer for any other dependency is uncertain. NXDOMAIN is distinct from a timeout.
- **FND-RUN-003** checks project, capability host and connection metadata for failed or non-terminal provisioning
  state and surfaced connection errors, and applies the connection-target scope policy (a literal ARM ID outside the
  project's resource group and `--approved-scope` is a finding; a target whose resource group cannot be determined is
  uncertain).
- **FND-RUN-004** checks deployment provisioning state and aggregate Azure Monitor signals (HTTP 429 and provisioned
  utilization). The `StatusCode` value domain of the metric is unverified; when 429 cannot be identified the result is
  uncertain.
- **FND-RUN-005** reads agent/version metadata and project connection listings from the Foundry v1 data plane
  (`data`/`has_more` for agents, `value`/`nextLink` for connections, `versions.latest`, limit at most 100, paging).
  An absent status is uncertain.
- **FND-RUN-006** reads only Search index definitions, statistics and indexer status; no documents are queried. Indexer
  `status` is unknown, error or running; the last execution status is transientFailure, success, inProgress or reset.
  The service result has no error code, so none is used. A recent success with zero documents is uncertain. The probe
  stores only structured status fields (`itemsProcessed`, `itemsFailed`), never `errorMessage`, errors or warnings text.
- **FND-RUN-007** uses diagnostic-setting metadata plus an aggregate log count addressed with the workspace GUID
  (`customerId`, read from the workspace). A resource-specific destination (`Dedicated`) is uncertain. It does not read
  raw log lines.

## Flags

| Flag | Purpose |
|---|---|
| `--subscription`, `--resource-group` | Azure target. Default to `AZURE_SUBSCRIPTION_ID` and `AZURE_RESOURCE_GROUP`. |
| `--account`, `--project` | Foundry account and project names. |
| `--vantage` | `none`, `local`, or `vnet`. `vnet` is an operator assertion that the command runs inside the VNet; it is not verified. It enables the DNS probes. |
| `--timeout` | Per-probe timeout. |
| `--strict` | Exit 3 when any check was skipped. |

## Least-privilege notes

The exact ARM actions are tracked in
[permissions-matrix.md](permissions-matrix.md). Runtime data-plane probes use
Microsoft Entra bearer tokens via `AZURE_ACCESS_TOKEN`, then `azd auth token`,
then `az account get-access-token`; they do not fall back to keys.

Probes are issued by the runtime command; whether independent probes run in parallel is not claimed here (see the hand-off). Use a dedicated read-only identity where possible. Missing permissions degrade
to skipped checks that name the exact capability and action.
