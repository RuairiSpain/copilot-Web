# shared-export-checkpoint

A callable child workflow, not a standalone ingestion pipeline — nothing
triggers it on a schedule. It exists to remove duplication: `llm-usage-ingestion`,
`mcp-usage-ingestion`, and `agent-usage-ingestion` each independently
re-implemented the identical three-action pattern for reading (and, on
first run, creating) their own "last export checkpoint" document —
`Read_Export_Config` → `Create_Export_Config` (only if the read failed) →
resolving `LastExportDate`/`CurrentTime` into workflow variables. That
pattern now lives here once.

## Contract

**Input** (the `Request` trigger's body):

```json
{
  "databaseId": "the Cosmos database name",
  "containerId": "the config container name (holds the checkpoint doc)",
  "configItemId": "the checkpoint document's own id — e.g. \"002\" for llm-usage-ingestion, \"003\" for mcp, \"004\" for agent"
}
```

**Output** (the HTTP response body):

```json
{
  "lastExportDate": "yyyy-MM-ddTHH:mm:00 — the checkpoint's own value, or utcNow() if no checkpoint document existed yet (and one was just created)",
  "currentTime": "yyyy-MM-ddTHH:mm:00 — utcNow() at the moment this ran"
}
```

Same behavior as before this refactor, byte-for-byte: a missing
checkpoint document is created with `lastExportDate` = now and
`totalExportedRecords: 0`; an existing one's `lastExportDate` is read
back unchanged.

## How a parent workflow calls this

A native Logic Apps Standard nested-workflow `Workflow`-type action,
calling this workflow by name (both live in the same host, so no
separate connection/auth is needed — this is Azure's own
same-host-workflow-invocation mechanism, not an HTTP call over the
network):

```json
"Call_Shared_Export_Checkpoint": {
  "type": "Workflow",
  "inputs": {
    "host": { "workflow": { "id": "shared-export-checkpoint" } },
    "body": {
      "databaseId": "@appsetting('CosmosDBDatabase')",
      "containerId": "@appsetting('CosmosDBContainerConfig')",
      "configItemId": "002"
    }
  },
  "runAfter": {}
}
```

Then a slim `Initialize_Logs_Time_Range` copies the two response fields
into the same `LastExportDate`/`CurrentTime` variable names every
existing downstream action (`Run_query_and_list_results`,
`Update_Export_Config`) already expects — see any of the three parent
workflows for the exact shape. Each parent's own `Update_Export_Config`
(the *write-back*, at the end of the run) is unchanged and stays in the
parent workflow — it's specific to that pipeline's own container and
record count, not something this shared workflow could sensibly own.

## What's NOT shared, and why

`pii-usage-ingestion` and `quota-approval-notification` don't use this
pattern at all today (confirmed — neither has a
`Read_Export_Config`/`Create_Export_Config` action pair), so they weren't
touched by this refactor. If either later needs the same
"resume-from-last-checkpoint" behavior, point it at this workflow rather
than writing a fourth copy of the pattern.

## Verification status

Structurally valid JSON, and the `Workflow`-type nested-invocation
mechanism and `Request`/`Response` trigger/action shapes follow Azure's
documented Logic Apps Standard schema — but this has **not** been
deployed or run against a live Logic Apps Standard host in this session
(no Azure subscription available). Verify the nested-workflow call
resolves correctly and the response shape matches what
`Initialize_Logs_Time_Range` expects before relying on this in
production, same disclosure as every other Logic App change in this
fork.
