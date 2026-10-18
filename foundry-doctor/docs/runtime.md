# Runtime diagnosis (`foundry-doctor runtime`)

`runtime` runs the `FND-RUN-001`..`FND-RUN-007` rules against a deployed
Foundry project. It combines:

- ARM management metadata reads
- metadata-only Foundry project endpoint reads
- metadata-only Azure AI Search reads
- aggregate Azure Monitor and Log Analytics counts
- optional DNS resolution from a declared network vantage point (operator asserted, not verified)

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
  Storage and Cosmos targets, scope-aware: only assignments at or above the resource count, and a grant on a child scope
  (for example one container) is uncertain. Roles are matched by built-in role GUID (verified in `built-in-roles.md`
  2026-10-18); role definitions are not fetched. Storage Account Contributor and Cosmos DB Operator are expected for the
  system-assigned identity only. Cosmos is matched by the SQL role GUID (unverified) and scope: the account or the
  `/dbs/enterprise_memory` database is a grant, any other database or a collection is uncertain. A missing role is
  reported with confidence capped at medium. Custom or unrecognised roles, conditional assignments, container-scoped Blob
  grants and Owner or Contributor for a management role are uncertain. Group-based and deny assignments are not
  evaluated. Key, SAS and service-principal connections are skipped because the command does not read credentials.
- **FND-RUN-002** validates private DNS resolution only when `--vantage vnet` is declared. The vantage point is
  asserted by the operator; the doctor cannot verify where it runs and says so. A fail needs a dependency proven
  private-endpoint-backed: for the Foundry account, `publicNetworkAccess` Disabled or an existing private endpoint
  connection; for Search, Storage and Cosmos DB, the connection `peStatus` Active (their own private endpoint connections
  are not read). A public answer for any other dependency is not probed and is uncertain. NXDOMAIN is distinct from a
  timeout or resolver error, which are skipped. The doctor does not read private DNS zones or virtual network links.
- **FND-RUN-003** checks project, capability host and connection metadata for failed or non-terminal provisioning
  state and surfaced connection errors, and applies the connection-target scope policy. The target resource ID is
  `metadata.ResourceId` when the connection carries it (the key name is unverified as a schema fact), otherwise
  `properties.target` when that is itself an ARM resource ID. A literal ARM ID outside the project's resource group and
  every `--approved-scope` is a finding. A resource-backed connection that yields no resource ID, or an unresolved
  template expression, is uncertain.
- **FND-RUN-004** checks deployment provisioning state and aggregate Azure Monitor signals over the last hour.
  `AzureOpenAIRequests` is read as Total split by `ModelDeploymentName` and `StatusCode` (at most 50 series; reaching
  the cap or an `errorCode` is noted as a truncation). A series with StatusCode 429 and a positive total is a finding for
  its deployment. If every StatusCode value is a three-digit code and none is 429, no throttling occurred; any other
  value that cannot be identified makes the result uncertain (the value domain is unverified). No time series is not a
  count of zero and is uncertain. Provisioned utilisation uses the Maximum aggregation; 100 percent or more is a finding.
- **FND-RUN-005** reads agent metadata from the listing and project connection listings from the Foundry v1 data plane
  (`data`/`has_more` for agents, `value`/`nextLink` for connections, `versions.latest`, limit at most 100, paging). An
  absent version status is treated as active; creating, an unrecognised value or a disabled agent is uncertain; failed,
  deleting or deleted is a finding. An agent or bound connection missing from a complete listing is a finding; missing from
  a capped listing it is uncertain.
- **FND-RUN-006** reads only Search index definitions, statistics and indexer status; no documents are queried. The index
  and indexer names come from connection metadata hints whose keys are unverified; without an index hint the connection
  is skipped. Vector fields must name a profile defined in `vectorSearch.profiles`. Indexer `status` is unknown, error or
  running; the last execution status is transientFailure, success, inProgress or reset. A zero document count after a
  success under 60 minutes ago, or while an indexer is running or was reset, is uncertain; a zero count with no
  successful run in the visible history says so explicitly. The probe stores only structured status fields
  (`itemsProcessed`, `itemsFailed`), never `errorMessage`, errors or warnings text.
- **FND-RUN-007** uses diagnostic-setting metadata plus an aggregate log count addressed with the workspace GUID
  (`customerId`, read from the workspace) and read from the `PrimaryResult` table. A resource-specific destination
  (`Dedicated`) is uncertain because the `AzureDiagnostics` count does not apply. A setting whose age is not reported is
  uncertain. Only the categories Audit and RequestResponse and the `allLogs` group are counted. It does not read raw log
  lines.

## Scheduling

The selected runtime rules run through `runtime.Scheduler` (`rules/run.Schedule`, wired in `internal/app/runtime.go`).
Parallelism is bounded at 4 rules in flight and each probe has its own `--timeout` (default 30s); a probe that exceeds
it is skipped as input-unavailable and names `--timeout`. Concurrency has been exercised only with fake probes.

## Flags

| Flag | Purpose |
|---|---|
| `--subscription`, `--resource-group` | Azure target. Default to `AZURE_SUBSCRIPTION_ID` and `AZURE_RESOURCE_GROUP`. |
| `--account`, `--project` | Foundry account and project names. |
| `--vantage` | `none`, `local`, or `vnet`. `vnet` is an operator assertion that the command runs inside the VNet; it is not verified. It enables the DNS probes. |
| `--approved-scope` | Extra scope (resource ID) that connection targets may lie in besides the project's resource group; repeatable. Used by FND-RUN-003. |
| `--timeout` | Per-probe timeout (default 30s). |
| `--strict` | Exit 3 when any check was skipped. |

## Least-privilege notes

The exact ARM actions are tracked in
[permissions-matrix.md](permissions-matrix.md). Runtime data-plane probes use
Microsoft Entra bearer tokens via `AZURE_ACCESS_TOKEN`, then `azd auth token`,
then `az account get-access-token`; they do not fall back to keys.

Probes are issued by the runtime command through the scheduler described above. Use a dedicated read-only identity where possible. Missing permissions degrade
to skipped checks that name the exact capability and action.
