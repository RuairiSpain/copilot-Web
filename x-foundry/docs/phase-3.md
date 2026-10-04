# Phase 3: data-plane provisioning and state

`xfoundry deploy azure.yaml` creates and updates the agents and toolboxes in the Foundry projects
that the Phase 2 infrastructure created. It talks to each project's data plane
(`https://<account>.services.ai.azure.com/api/projects/<project>`, API version `v1`). The paths
and payload shapes follow the `azure-ai-projects` SDK (2.8.0); the token scope is
`https://ai.azure.com/.default`.

```bash
xfoundry generate azure.yaml --out infra        # Phase 2: the Azure resources
azd provision                                    # (or az deployment sub create)
xfoundry deploy azure.yaml --dry-run             # what would change, no Azure calls
xfoundry deploy azure.yaml                       # --account defaults to $AZURE_AI_ACCOUNT_NAME
```

## What is deployed

| Configuration | Foundry object |
| --- | --- |
| `toolboxes[]` (effective, so inherited hub toolboxes are created in every project that inherits them) | toolbox version: `POST /toolboxes/{name}/versions` |
| `agents[]` with `kind: prompt` | agent version: `POST /agents/{name}/versions` with a `prompt` definition (model, instructions, tools) |
| `mcps[]` | an `mcp` tool inside a toolbox or an agent |
| a toolbox attached to an agent (`agents[].toolboxes`) | an `mcp` tool on the agent that points at the toolbox's versioned MCP URL |

* Agent and toolbox versions are immutable. A change creates a new version; nothing is edited in place.
* An MCP server with `allowedTools` is called without asking for approval; one without is called
  only after approval (`require_approval: always`), the safe default.
* The model is the agent's `model`, else the project's default model.
* Every agent gets metadata (`managed-by`, `x-foundry-env`, `x-foundry-id`) so it can be recognised.

## State

`deploy` records what it deployed in `.xfoundry/<environment>.state.json` next to `azure.yaml`
(or `--state`): a content hash and the version the service returned for each item. It holds no
secrets, so a team can commit it or keep it on shared storage. A state file for another
environment is refused.

* An item whose hash is unchanged is only checked. If it is missing from the project, or the project
  has a different version than the one recorded (someone changed it by hand), it is deployed again.
* A toolbox change changes the hash of every agent that attaches it, so those agents get a new version.
* The state is saved after every successful step, so a failure keeps the progress made.
* `--dry-run` compares the configuration with the state only. It needs no credentials.

## Destructive changes (XF025)

An item that is in the state but no longer in the configuration is deleted from Foundry, with
every version. `deploy` refuses to do that unless you pass `--allow-destroy`, and `--dry-run`
reports it as `XF025` with a non-zero exit code so a pipeline can stop. Only items in the state
are ever deleted, so objects created by other means are left alone. Agents are deleted before
toolboxes. The rule covers the data plane; infrastructure deletions are not tracked (Bicep
deployments are incremental and never delete).

## Diagnostics

| Code | Meaning |
| --- | --- |
| XF025 | An item left the configuration and would be deleted; needs `--allow-destroy` (error) |
| XF210 | An MCP server uses authentication, so its tool points at a project connection of the same name that x-foundry does not create yet (w) |
| XF211 | Something is not deployed yet: a hosted agent (use an azd `azure.ai.agent` service), knowledge bases (Phase 4), or a tool type other than `mcp` and `codeInterpreter` (w) |

## Not done yet

* **Evaluation datasets and evaluators.** Uploading datasets needs the pending-upload flow, and
  running evaluations stays with the user's CI. Deferred.
* **Project connections** for authenticated MCP servers and connectors (an Azure Resource Manager
  resource, not a data-plane one). Until then the connection must exist; see `XF210`.
* **Knowledge bases and the other tool types** (Phase 4 and later).
* **Credential handling for toolbox attachment.** The Python SDK sample passes a short-lived
  bearer token in the agent's MCP tool to reach a toolbox. x-foundry omits it, because a token in
  an agent definition expires. Whether the service authorises the agent's own identity without
  one has not been checked against a live project. Check this first when you try `deploy`.

## Validation

The client is tested against a local HTTP server and the engine against an in-memory fake, so
the request shapes, ordering, retries, state handling and destroy approval are tested; nothing has
been run against a live Foundry project, which this repository has no access to. The SDK is the
source for the paths and field names; confirm them against a test project before relying on them.
