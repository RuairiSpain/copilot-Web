#!/usr/bin/env bash
#
# Provision the Azure side of the decision engine and deploy the container.
#
#   ./scripts/provision_azure.sh                 # create everything, then deploy
#   RG=my-rg LOCATION=northeurope ./scripts/...  # override any variable below
#   SKIP_INFRA=1 ./scripts/provision_azure.sh    # rebuild and redeploy only
#
# Every name is derived from PREFIX so two people can run this in the same
# subscription without colliding, and re-running is safe: each step checks for
# an existing resource first.
#
# Auth to blob storage defaults to the Container App's managed identity with an
# RBAC role assignment — no account key in an environment variable. Set
# USE_STORAGE_KEY=1 for the connection-string path instead (useful when you
# cannot create role assignments in the subscription).
set -euo pipefail

PREFIX="${PREFIX:-jevbyom}"
RG="${RG:-${PREFIX}-rg}"
LOCATION="${LOCATION:-westeurope}"
STORAGE="${STORAGE:-${PREFIX}store$(echo "${RG}" | cksum | cut -c1-6)}"
CONTAINER="${CONTAINER:-jev-calibration}"
ACR="${ACR:-${PREFIX}acr$(echo "${RG}" | cksum | cut -c1-6)}"
APP_ENV="${APP_ENV:-${PREFIX}-env}"
APP_NAME="${APP_NAME:-${PREFIX}-app}"
IMAGE_NAME="${IMAGE_NAME:-jev-byom}"
IMAGE_TAG="${IMAGE_TAG:-$(date -u +%Y%m%d%H%M%S)}"
LOG_ANALYTICS="${LOG_ANALYTICS:-${PREFIX}-logs}"

# Container sizing. The base model is held in-process, so memory is the real
# constraint; a DistilBERT-class model on CPU is comfortable at 2Gi.
CPU="${CPU:-1.0}"
MEMORY="${MEMORY:-2.0Gi}"
MIN_REPLICAS="${MIN_REPLICAS:-1}"   # 0 would cold-start the model on every scale-up
MAX_REPLICAS="${MAX_REPLICAS:-5}"

# Engine configuration passed through to the container.
MODEL_NAME="${MODEL_NAME:-distilbert-base-uncased}"
MODEL_BACKEND="${MODEL_BACKEND:-huggingface}"
MODEL_NUM_LABELS="${MODEL_NUM_LABELS:-2}"
API_KEY="${API_KEY:-}"              # empty leaves the endpoint unauthenticated
REQUIRE_CALIBRATION="${REQUIRE_CALIBRATION:-false}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

say() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }

require() { command -v "$1" >/dev/null || { echo "missing required tool: $1" >&2; exit 1; }; }
require az
require docker

if [[ "${SKIP_INFRA:-0}" != "1" ]]; then
  say "Resource group ${RG} (${LOCATION})"
  az group create -n "$RG" -l "$LOCATION" -o none

  say "Storage account ${STORAGE} and container ${CONTAINER}"
  az storage account create -n "$STORAGE" -g "$RG" -l "$LOCATION" \
    --sku Standard_LRS --kind StorageV2 --min-tls-version TLS1_2 \
    --allow-blob-public-access false -o none
  # --auth-mode login uses your own Entra identity; it avoids printing a key.
  az storage container create --account-name "$STORAGE" -n "$CONTAINER" \
    --auth-mode login -o none

  say "Container registry ${ACR}"
  az acr create -n "$ACR" -g "$RG" -l "$LOCATION" --sku Basic -o none

  say "Container Apps environment ${APP_ENV}"
  az extension add --name containerapp --upgrade --allow-preview false -o none 2>/dev/null || \
    az extension add --name containerapp --upgrade -o none
  az provider register -n Microsoft.App --wait -o none
  az provider register -n Microsoft.OperationalInsights --wait -o none
  az containerapp env create -n "$APP_ENV" -g "$RG" -l "$LOCATION" \
    --logs-destination log-analytics --logs-workspace-name "$LOG_ANALYTICS" -o none 2>/dev/null || \
    az containerapp env create -n "$APP_ENV" -g "$RG" -l "$LOCATION" -o none
fi

LOGIN_SERVER="$(az acr show -n "$ACR" -g "$RG" --query loginServer -o tsv)"
IMAGE="${LOGIN_SERVER}/${IMAGE_NAME}:${IMAGE_TAG}"

say "Building ${IMAGE}"
# ACR build keeps the ~1 GB of layers off your uplink and needs no local
# docker login; swap for `docker build && docker push` if you prefer.
az acr build -r "$ACR" -t "${IMAGE_NAME}:${IMAGE_TAG}" -f docker/Dockerfile "$HERE"

ENV_VARS=(
  "BLOB_CONTAINER_NAME=${CONTAINER}"
  "JEV_MODEL_BACKEND=${MODEL_BACKEND}"
  "JEV_MODEL_NAME=${MODEL_NAME}"
  "JEV_MODEL_NUM_LABELS=${MODEL_NUM_LABELS}"
  "JEV_REQUIRE_CALIBRATION=${REQUIRE_CALIBRATION}"
  "JEV_LOG_JSON=true"
)
if [[ "${USE_STORAGE_KEY:-0}" == "1" ]]; then
  SA_KEY="$(az storage account keys list -n "$STORAGE" -g "$RG" --query '[0].value' -o tsv)"
  ENV_VARS+=("BLOB_CONN_STR=DefaultEndpointsProtocol=https;AccountName=${STORAGE};AccountKey=${SA_KEY};EndpointSuffix=core.windows.net")
else
  ENV_VARS+=("BLOB_ACCOUNT_URL=https://${STORAGE}.blob.core.windows.net")
fi
[[ -n "$API_KEY" ]] && ENV_VARS+=("JEV_API_KEY=secretref:jev-api-key")

say "Deploying ${APP_NAME}"
if az containerapp show -n "$APP_NAME" -g "$RG" -o none 2>/dev/null; then
  [[ -n "$API_KEY" ]] && az containerapp secret set -n "$APP_NAME" -g "$RG" \
    --secrets "jev-api-key=${API_KEY}" -o none
  az containerapp update -n "$APP_NAME" -g "$RG" --image "$IMAGE" \
    --cpu "$CPU" --memory "$MEMORY" \
    --min-replicas "$MIN_REPLICAS" --max-replicas "$MAX_REPLICAS" \
    --set-env-vars "${ENV_VARS[@]}" -o none
else
  CREATE_ARGS=(-n "$APP_NAME" -g "$RG" --environment "$APP_ENV" --image "$IMAGE"
    --target-port 8000 --ingress external --transport auto
    --cpu "$CPU" --memory "$MEMORY"
    --min-replicas "$MIN_REPLICAS" --max-replicas "$MAX_REPLICAS"
    --registry-server "$LOGIN_SERVER" --system-assigned
    --env-vars "${ENV_VARS[@]}")
  [[ -n "$API_KEY" ]] && CREATE_ARGS+=(--secrets "jev-api-key=${API_KEY}")
  az containerapp create "${CREATE_ARGS[@]}" -o none
fi

PRINCIPAL_ID="$(az containerapp show -n "$APP_NAME" -g "$RG" --query identity.principalId -o tsv)"

if [[ "${USE_STORAGE_KEY:-0}" != "1" ]]; then
  say "Granting the app's identity access to ${STORAGE}"
  STORAGE_ID="$(az storage account show -n "$STORAGE" -g "$RG" --query id -o tsv)"
  az role assignment create --assignee-object-id "$PRINCIPAL_ID" \
    --assignee-principal-type ServicePrincipal \
    --role "Storage Blob Data Contributor" --scope "$STORAGE_ID" -o none 2>/dev/null || \
    echo "role assignment already present (or you lack permission to create it)"
fi

say "Granting the app's identity pull access to ${ACR}"
ACR_ID="$(az acr show -n "$ACR" -g "$RG" --query id -o tsv)"
az role assignment create --assignee-object-id "$PRINCIPAL_ID" \
  --assignee-principal-type ServicePrincipal \
  --role AcrPull --scope "$ACR_ID" -o none 2>/dev/null || \
  echo "AcrPull already present (or you lack permission to create it)"

APP_URL="https://$(az containerapp show -n "$APP_NAME" -g "$RG" \
  --query properties.configuration.ingress.fqdn -o tsv)"

say "Deployed"
cat <<SUMMARY
  App URL        : ${APP_URL}
  Health         : ${APP_URL}/health
  OpenAPI        : ${APP_URL}/openapi.json
  Image          : ${IMAGE}
  Storage        : ${STORAGE}/${CONTAINER}
  Identity       : ${PRINCIPAL_ID}

Next:
  python scripts/smoke_test.py --base-url "${APP_URL}"${API_KEY:+ --api-key "\$API_KEY"}
  python scripts/foundry_tool_spec.py --base-url "${APP_URL}" --out foundry-decision-tool.json
SUMMARY
