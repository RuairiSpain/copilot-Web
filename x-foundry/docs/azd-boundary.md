# What azd owns and what x-foundry owns

The `azure.yaml` file that azd reads already describes the Foundry project, its model
deployments, agents, toolboxes, connections, skills and routines, as azd services
(`azure.ai.project`, `azure.ai.agent`, `azure.ai.toolbox`, `azure.ai.connection`,
`azure.ai.skill`, `azure.ai.routine`). x-foundry does not repeat any of that. There is one
place to declare each thing.

x-foundry is the **landing zone around the azd project**: the shared infrastructure the
project needs and the policy that applies across projects.

## Ownership

| Concern | Owner | Notes |
| --- | --- | --- |
| Foundry resource and project | azd (`azure.ai.project`) | x-foundry `projects[].name` must match the azd project name |
| Model deployments, content filters, version upgrade options | azd (`azure.ai.project` `deployments`) | x-foundry keeps only the *policy* (`models.default/allowed/denied`) used by the gateway and the `modelPolicy` check |
| Agents (hosted, prompt, voice), toolboxes, MCP servers, connections, skills, routines, evaluation | azd | Removed from the x-foundry schema |
| Virtual network, subnets, private DNS | x-foundry | Outputs `VNET_RESOURCE_ID`, `PE_SUBNET_NAME`, `AGENT_SUBNET_NAME`, `PRIVATE_DNS_*` |
| Storage, Cosmos DB, AI Search, Key Vault | x-foundry | Outputs `STORAGE_ACCOUNT_RESOURCE_ID`, `COSMOS_DB_RESOURCE_ID`, `AI_SEARCH_RESOURCE_ID` (`AI_SEARCH_<SCOPE>_RESOURCE_ID` for hub and project Search services), `KEY_VAULT_RESOURCE_ID` |
| Log Analytics, Application Insights | x-foundry | Outputs `LOG_ANALYTICS_WORKSPACE_ID`, `APPLICATIONINSIGHTS_RESOURCE_ID` |
| Delete locks, diagnostic settings, operator roles | x-foundry | |
| Foundry IQ knowledge bases (data plane on Search) | x-foundry (Phase 4) | They reference azd-owned connections and embedding deployments by name |
| APIM AI gateway, token limits | x-foundry (Phase 5) | Endpoints refer to azd-owned agents by name |
| Governance: policy, Defender, budgets, required tags | x-foundry (Phase 5) | |
| WAF profiles, `doctor`, `promote` | x-foundry (Phase 6) | |

## How the two fit together

1. `xfoundry generate azure.yaml --out infra` writes a Bicep project for the shared
   infrastructure.
2. Deploy it (`az deployment sub create`, see the generated README).
3. Set the outputs as azd environment values and reference them from the azd `azure.ai.project`
   service with `${VAR}` (for example the VNet and subnet names under `network`).
4. Run `azd up` for the Foundry project and everything azd provisions.

x-foundry never creates anything in the Foundry data plane that azd manages. The `deploy`
command and the Foundry client from the earlier Phase 3 were removed for that reason.

## Things not verified

This repository has no Azure subscription, so none of this has been deployed. Two points need a
test in a real subscription before the first release:

* Whether the azd `azure.ai.project` service can use an existing Storage account, Cosmos DB and
  AI Search service (the standard agent setup) from the values above. The azd reference reviewed
  for this change shows `network` and `deployments` but not these.
* How azd treats an `infra` folder: its presence replaces azd's own synthesis, so the order
  "x-foundry infrastructure first, then the azd project" may need a hook or a separate
  environment.

## What is not generated

Foundry role assignments for admins and developers on the Foundry resource and projects need
the resources azd creates. They are listed as "not generated" in the output. Phase 6 should
decide between a post-provision step and `doctor` comments.
