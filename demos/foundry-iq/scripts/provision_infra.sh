#!/usr/bin/env bash
# Stand up the Azure resources the Foundry IQ demo needs, with az CLI only.
#
#   ./scripts/provision_infra.sh -g rg-foundry-iq-demo -l swedencentral
#
# Creates: storage account + container, an AI Search service, an Azure OpenAI
# account with a chat and an embedding deployment, the role assignments that
# make Entra-only auth work, and (optionally) diagnostic settings so command 4
# has server-side logs to read. Prints a ready-to-source .env at the end.
#
# Everything is idempotent: re-running it updates rather than duplicates.

set -euo pipefail

RESOURCE_GROUP=""
LOCATION="swedencentral"
PREFIX="fiqdemo$RANDOM"
SEARCH_SKU="standard"
CHAT_MODEL="gpt-5.4-mini"
CHAT_VERSION=""
EMBED_MODEL="text-embedding-3-large"
EMBED_VERSION=""
CONTAINER="eu-directives"
WITH_DIAGNOSTICS=0

usage() { sed -n '2,14p' "$0"; exit "${1:-0}"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    -g|--resource-group) RESOURCE_GROUP="$2"; shift 2 ;;
    -l|--location)       LOCATION="$2"; shift 2 ;;
    -p|--prefix)         PREFIX="$2"; shift 2 ;;
    --search-sku)        SEARCH_SKU="$2"; shift 2 ;;
    --chat-model)        CHAT_MODEL="$2"; shift 2 ;;
    --embed-model)       EMBED_MODEL="$2"; shift 2 ;;
    --container)         CONTAINER="$2"; shift 2 ;;
    --with-diagnostics)  WITH_DIAGNOSTICS=1; shift ;;
    -h|--help)           usage 0 ;;
    *) echo "unknown flag: $1" >&2; usage 1 ;;
  esac
done

[[ -n "$RESOURCE_GROUP" ]] || { echo "error: -g/--resource-group is required" >&2; usage 1; }
command -v az >/dev/null || { echo "error: az CLI not found" >&2; exit 1; }

SUBSCRIPTION=$(az account show --query id -o tsv)
PRINCIPAL=$(az ad signed-in-user show --query id -o tsv)
STORAGE="${PREFIX}stg"
SEARCH="${PREFIX}-search"
AOAI="${PREFIX}-aoai"
WORKSPACE="${PREFIX}-logs"

echo "subscription   $SUBSCRIPTION"
echo "resource group $RESOURCE_GROUP ($LOCATION)"
echo

az group create -n "$RESOURCE_GROUP" -l "$LOCATION" -o none

echo "==> storage account $STORAGE"
az storage account create -n "$STORAGE" -g "$RESOURCE_GROUP" -l "$LOCATION" \
  --sku Standard_LRS --kind StorageV2 --allow-blob-public-access false -o none
STORAGE_ID=$(az storage account show -n "$STORAGE" -g "$RESOURCE_GROUP" --query id -o tsv)
az storage container create --name "$CONTAINER" --account-name "$STORAGE" --auth-mode login -o none

echo "==> search service $SEARCH ($SEARCH_SKU)"
# `az search service create` has no --identity-type; identity_type exists only
# on `update` (see azure-cli search/custom.py::update_search_service), so the
# managed identity is assigned in a second call.
az search service create -n "$SEARCH" -g "$RESOURCE_GROUP" -l "$LOCATION" \
  --sku "$SEARCH_SKU" -o none
# --disable-local-auth true is what actually makes this Entra-only. The
# alternative, --auth-options aadOrApiKey, *permits* API keys as well and is
# mutually exclusive with --disable-local-auth.
az search service update -n "$SEARCH" -g "$RESOURCE_GROUP" \
  --identity-type SystemAssigned --disable-local-auth true -o none
SEARCH_ID=$(az search service show -n "$SEARCH" -g "$RESOURCE_GROUP" --query id -o tsv)
SEARCH_MI=$(az search service show -n "$SEARCH" -g "$RESOURCE_GROUP" --query identity.principalId -o tsv)

echo "==> azure openai $AOAI"
# --yes accepts the responsible-AI terms; without it the command prompts and
# an unattended run hangs.
az cognitiveservices account create -n "$AOAI" -g "$RESOURCE_GROUP" -l "$LOCATION" \
  --kind OpenAI --sku S0 --custom-domain "$AOAI" --assign-identity --yes -o none
AOAI_ENDPOINT=$(az cognitiveservices account show -n "$AOAI" -g "$RESOURCE_GROUP" --query properties.endpoint -o tsv)
AOAI_ID=$(az cognitiveservices account show -n "$AOAI" -g "$RESOURCE_GROUP" --query id -o tsv)

deploy_model() {
  local name="$1" model="$2" version="$3"
  echo "    deployment $name ($model)"
  if [[ -z "$version" ]]; then
    version=$(az cognitiveservices account list-models -n "$AOAI" -g "$RESOURCE_GROUP" \
      --query "[?name=='$model'] | [0].version" -o tsv 2>/dev/null || true)
  fi
  [[ -n "$version" ]] || { echo "    !! $model not available in $LOCATION — pick another region or --chat-model/--embed-model" >&2; return 1; }
  az cognitiveservices account deployment create -n "$AOAI" -g "$RESOURCE_GROUP" \
    --deployment-name "$name" --model-name "$model" --model-version "$version" \
    --model-format OpenAI --sku-capacity 50 --sku-name Standard -o none
}
deploy_model "$CHAT_MODEL" "$CHAT_MODEL" "$CHAT_VERSION"
deploy_model "$EMBED_MODEL" "$EMBED_MODEL" "$EMBED_VERSION"

echo "==> role assignments"
assign() {  # role, principal, scope
  az role assignment create --role "$1" --assignee-object-id "$2" --assignee-principal-type "$3" --scope "$4" -o none 2>/dev/null \
    || echo "    (already assigned: $1)"
}
# You: manage and query the search service, read/write blobs, call AOAI.
assign "Search Service Contributor"    "$PRINCIPAL" User "$SEARCH_ID"
assign "Search Index Data Contributor" "$PRINCIPAL" User "$SEARCH_ID"
assign "Storage Blob Data Contributor" "$PRINCIPAL" User "$STORAGE_ID"
assign "Cognitive Services OpenAI User" "$PRINCIPAL" User "$AOAI_ID"
# The search service: read the directives container, call AOAI for ingestion
# and for the query-time vectorizer.
assign "Storage Blob Data Reader"      "$SEARCH_MI" ServicePrincipal "$STORAGE_ID"
assign "Cognitive Services OpenAI User" "$SEARCH_MI" ServicePrincipal "$AOAI_ID"

WORKSPACE_ID=""
if [[ "$WITH_DIAGNOSTICS" == "1" ]]; then
  echo "==> diagnostics -> $WORKSPACE"
  az monitor log-analytics workspace create -g "$RESOURCE_GROUP" -n "$WORKSPACE" -l "$LOCATION" -o none
  WORKSPACE_RID=$(az monitor log-analytics workspace show -g "$RESOURCE_GROUP" -n "$WORKSPACE" --query id -o tsv)
  WORKSPACE_ID=$(az monitor log-analytics workspace show -g "$RESOURCE_GROUP" -n "$WORKSPACE" --query customerId -o tsv)
  az monitor diagnostic-settings create --name search-diag --resource "$SEARCH_ID" \
    --workspace "$WORKSPACE_RID" \
    --logs '[{"category":"OperationLogs","enabled":true}]' \
    --metrics '[{"category":"AllMetrics","enabled":true}]' -o none
fi

cat <<ENV

================================ .env ================================
SEARCH_ENDPOINT=https://${SEARCH}.search.windows.net
AOAI_ENDPOINT=${AOAI_ENDPOINT%/}
AOAI_CHAT_DEPLOYMENT=${CHAT_MODEL}
AOAI_CHAT_MODEL=${CHAT_MODEL}
AOAI_EMBEDDING_DEPLOYMENT=${EMBED_MODEL}
AOAI_EMBEDDING_MODEL=${EMBED_MODEL}
STORAGE_ACCOUNT=${STORAGE}
STORAGE_CONTAINER=${CONTAINER}
STORAGE_RESOURCE_ID=${STORAGE_ID}
KNOWLEDGE_BASE=es-employment-kb
BLOB_KNOWLEDGE_SOURCE=eu-directives-ks
INDEX_KNOWLEDGE_SOURCE=hr-templates-ks
HR_INDEX=hr-templates-index
LOG_ANALYTICS_WORKSPACE=${WORKSPACE_ID}
======================================================================

Write that to demos/foundry-iq/.env, then:
  set -a && . ./.env && set +a
  python scripts/1_scrape.py
  python scripts/2_provision.py --project <your-foundry-project>

Role assignments can take a couple of minutes to propagate. A 403 on the first
run usually just means you were faster than Entra.
ENV
