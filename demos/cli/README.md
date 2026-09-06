# Foundry from the CLI, with no portal

Everything below assumes you have CLI access to the subscription and **cannot
open the Azure portal**. Where a value normally comes from a blade, there's a
command that prints it instead.

## Which CLI

Two, and they do different jobs:

| CLI | Scope | Install |
| --- | --- | --- |
| `az` | Azure resources: accounts, search services, storage, model deployments, roles, diagnostics | [Azure CLI](https://learn.microsoft.com/cli/azure/install-azure-cli) |
| `azd ai …` | Foundry objects inside a project: agents, connections, toolboxes, skills, routines | `azd ext install microsoft.foundry` |

There is **no `az foundry` command group**. The Foundry surface is a set of
Azure Developer CLI extensions:

```bash
azd ext install microsoft.foundry      # bundle: installs all of the below

azd ext install azure.ai.agents        # azd ai agent       — ship agents
azd ext install azure.ai.connections   # azd ai connection  — project connections
azd ext install azure.ai.projects      # azd ai project     — project context
azd ext install azure.ai.toolboxes     # azd ai toolbox     — versioned tool collections
azd ext install azure.ai.skills        # azd ai skill       — reusable agent guidelines
azd ext install azure.ai.routines      # azd ai routine     — timers, schedules, event triggers
azd ext install azure.ai.inspector     # azd ai inspector   — local agent inspector UI

azd ext list                           # what's installed
azd ext upgrade microsoft.foundry
azd ai agent version                   # per-extension version
```

## 0. Orient yourself

```bash
az login                                        # add --use-device-code over SSH
az account show -o table
az account set --subscription "<name-or-id>"

az account list-locations --query "[].name" -o tsv | sort   # valid --location values
az group list -o table
```

Your own object id, which you need for role assignments:

```bash
az ad signed-in-user show --query id -o tsv
```

## 1. Resource group

```bash
az group create -n rg-foundry-demo -l swedencentral
```

## 2. The Foundry account and project

A Foundry account is a Cognitive Services account of kind `AIServices`. The
custom domain is required — it's what gives you a stable `services.ai.azure.com`
hostname.

```bash
az cognitiveservices account create \
  -n foundry-demo -g rg-foundry-demo -l swedencentral \
  --kind AIServices --sku S0 \
  --custom-domain foundry-demo \
  --assign-identity

az cognitiveservices account show -n foundry-demo -g rg-foundry-demo \
  --query "{endpoint:properties.endpoint, id:id, mi:identity.principalId}" -o json
```

**Projects are child resources of that account, and `az` has no first-class
command for them.** Rather than trusting a command I can't verify against your
subscription, discover the resource type and use the generic ARM path:

```bash
# what child types does this provider actually expose in your subscription?
az provider show --namespace Microsoft.CognitiveServices \
  --query "resourceTypes[].resourceType" -o tsv | grep -i project

# then create one through the generic resource command
SUB=$(az account show --query id -o tsv)
az resource create \
  --id "/subscriptions/$SUB/resourceGroups/rg-foundry-demo/providers/Microsoft.CognitiveServices/accounts/foundry-demo/projects/demo-project" \
  --api-version 2025-06-01 \
  --location swedencentral \
  --properties '{"displayName":"demo-project"}'
```

Check the api-version the grep above implies before running it — provider
api-versions move, and this is the one command here most likely to need a
version bump:

```bash
az provider show --namespace Microsoft.CognitiveServices \
  --query "resourceTypes[?resourceType=='accounts/projects'].apiVersions[]" -o tsv | head
```

Then pin the project for the `azd ai` commands so you stop passing it everywhere:

```bash
azd ai project set --endpoint "https://foundry-demo.services.ai.azure.com/api/projects/demo-project"
azd ai project show
```

## 3. Models

```bash
# what's actually deployable in this account and region — check before you commit to a model
az cognitiveservices account list-models -n foundry-demo -g rg-foundry-demo \
  --query "[].{name:name, version:version, format:format, sku:skus[0].name}" -o table

az cognitiveservices account deployment create \
  -n foundry-demo -g rg-foundry-demo \
  --deployment-name gpt-5.4-mini \
  --model-name gpt-5.4-mini --model-version <version-from-above> \
  --model-format OpenAI \
  --sku-name Standard --sku-capacity 50

az cognitiveservices account deployment list -n foundry-demo -g rg-foundry-demo -o table
```

Region availability is the usual failure here: `list-models` returning nothing
for your model means pick another region, not that the model is gone.

## 4. Supporting resources

**Azure AI Search** (knowledge bases, indexes, agentic retrieval):

```bash
az search service create -n foundry-demo-search -g rg-foundry-demo \
  -l swedencentral --sku standard --identity-type SystemAssigned

az search service show -n foundry-demo-search -g rg-foundry-demo \
  --query "{endpoint:join('',['https://',name,'.search.windows.net']), mi:identity.principalId}" -o json
```

**Storage** (blob knowledge sources):

```bash
az storage account create -n foundrydemostg -g rg-foundry-demo \
  -l swedencentral --sku Standard_LRS --kind StorageV2 --allow-blob-public-access false

az storage container create --name eu-directives --account-name foundrydemostg --auth-mode login
az storage account show -n foundrydemostg -g rg-foundry-demo --query id -o tsv   # the ResourceId= value
```

## 5. Roles — the step that actually bites

Entra-only auth means nothing works until these land, and propagation takes a
minute or two. A 403 on your first call usually just means you were quicker
than Entra.

```bash
ME=$(az ad signed-in-user show --query id -o tsv)
SEARCH_ID=$(az search service show -n foundry-demo-search -g rg-foundry-demo --query id -o tsv)
SEARCH_MI=$(az search service show -n foundry-demo-search -g rg-foundry-demo --query identity.principalId -o tsv)
STORAGE_ID=$(az storage account show -n foundrydemostg -g rg-foundry-demo --query id -o tsv)
AOAI_ID=$(az cognitiveservices account show -n foundry-demo -g rg-foundry-demo --query id -o tsv)

# you
az role assignment create --role "Search Service Contributor"     --assignee-object-id $ME --assignee-principal-type User --scope $SEARCH_ID
az role assignment create --role "Search Index Data Contributor"  --assignee-object-id $ME --assignee-principal-type User --scope $SEARCH_ID
az role assignment create --role "Storage Blob Data Contributor"  --assignee-object-id $ME --assignee-principal-type User --scope $STORAGE_ID
az role assignment create --role "Cognitive Services OpenAI User" --assignee-object-id $ME --assignee-principal-type User --scope $AOAI_ID

# the search service, so it can read blobs and call the model for ingestion
# and for the query-time vectorizer
az role assignment create --role "Storage Blob Data Reader"       --assignee-object-id $SEARCH_MI --assignee-principal-type ServicePrincipal --scope $STORAGE_ID
az role assignment create --role "Cognitive Services OpenAI User" --assignee-object-id $SEARCH_MI --assignee-principal-type ServicePrincipal --scope $AOAI_ID

az role assignment list --assignee $ME --all -o table   # confirm
```

## 6. Project objects with `azd ai`

```bash
azd ai connection create my-api-conn \
  --kind remote-tool --target https://api.example.com \
  --auth-type custom-keys --custom-key "Authorization=Bearer $TOKEN"
azd ai connection list

azd ai toolbox create hr-tools --from-file ./toolbox.yaml
azd ai toolbox version list hr-tools
azd ai toolbox publish hr-tools <version_id>

azd ai skill list
azd ai routine list
azd ai agent list
```

`toolbox.yaml` is what `../foundry-toolbox/scripts/create_toolbox.py --via yaml`
writes.

## 7. Every endpoint and setting a developer needs

```bash
RG=rg-foundry-demo
echo "PROJECT_ENDPOINT=https://foundry-demo.services.ai.azure.com/api/projects/demo-project"
echo "AOAI_ENDPOINT=$(az cognitiveservices account show -n foundry-demo -g $RG --query properties.endpoint -o tsv)"
echo "SEARCH_ENDPOINT=https://$(az search service show -n foundry-demo-search -g $RG --query name -o tsv).search.windows.net"
echo "STORAGE_ACCOUNT=$(az storage account show -n foundrydemostg -g $RG --query name -o tsv)"
echo "STORAGE_RESOURCE_ID=$(az storage account show -n foundrydemostg -g $RG --query id -o tsv)"
```

Derived endpoints, once you know the two above:

```
toolbox MCP (default version)   {PROJECT_ENDPOINT}/toolboxes/{name}/mcp?api-version=v1
toolbox MCP (pinned version)    {PROJECT_ENDPOINT}/toolboxes/{name}/versions/{version}/mcp?api-version=v1
knowledge base MCP              {SEARCH_ENDPOINT}/knowledgebases/{name}/mcp?api-version=2026-08-01-preview
knowledge base retrieve         {SEARCH_ENDPOINT}/knowledgebases/{name}/retrieve?api-version=2026-08-01-preview
```

Everything else in the project — agents, toolboxes, skills, knowledge bases,
indexes, connections — is enumerable without the portal:

```bash
python ../foundry-toolbox/scripts/inventory.py --all --out inventory.md
```

## 8. Diagnostics

Needed for the server-side view in the Foundry IQ demo's trace command.

```bash
az monitor log-analytics workspace create -g rg-foundry-demo -n foundry-demo-logs -l swedencentral
WS=$(az monitor log-analytics workspace show -g rg-foundry-demo -n foundry-demo-logs --query id -o tsv)

az monitor diagnostic-settings create --name search-diag \
  --resource "$SEARCH_ID" --workspace "$WS" \
  --logs '[{"category":"OperationLogs","enabled":true}]' \
  --metrics '[{"category":"AllMetrics","enabled":true}]'

# the customerId is what `az monitor log-analytics query --workspace` wants
az monitor log-analytics workspace show -g rg-foundry-demo -n foundry-demo-logs --query customerId -o tsv
```

## 9. Tear down

```bash
az group delete -n rg-foundry-demo --yes --no-wait
```

## Getting unstuck without a portal

```bash
az <group> <command> --help
az find "az search service"                  # examples from real usage
az rest --method get --url "<arm-resource-id>?api-version=<v>"   # raw ARM when no command exists
az provider show --namespace Microsoft.CognitiveServices --query "resourceTypes[].resourceType" -o tsv
az account get-access-token --resource https://search.azure.com --query accessToken -o tsv   # curl the data plane
```

That last one is how you test a data-plane call by hand:

```bash
TOKEN=$(az account get-access-token --resource https://search.azure.com --query accessToken -o tsv)
curl -s -H "Authorization: Bearer $TOKEN" \
  "https://foundry-demo-search.search.windows.net/knowledgebases?api-version=2026-08-01-preview" | jq .
```

## What to verify before relying on this

Two things here I could not check against a live subscription:

- **Project creation** (§2). The `az resource create` path is the generic ARM
  route and the api-version is a placeholder — run the `az provider show` query
  first and use what it returns.
- **`azd ai` sub-command flags** beyond the ones shown in the toolbox docs.
  `azd ai <group> --help` is authoritative over this file.

Everything else is either standard `az` or taken from the Foundry docs.
