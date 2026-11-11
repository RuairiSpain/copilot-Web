#!/usr/bin/env bash
# Calendar mode deploy: the agent reads its Outlook calendar through Microsoft Graph and joins its Teams
# meetings. Separate from deploy.sh invite mode: no Foundry, ACS, VNet or phone number involved.
#
#   ./deploy.sh --mode calendar --name tvm --agent-mailbox agent@contoso.com --callback-url https://bot.contoso.com/api/calling
#
# You host the notification receiver yourself (`teams-voice calendar run`, port 8080) behind that public HTTPS URL.
# Needs: az (logged in), permission to create Entra apps and grant admin consent, python3 3.10+.
set -euo pipefail
cd "$(dirname "$0")"
ROOT="$(cd .. && pwd)"

NAME=tvm LOCATION=westeurope SUBSCRIPTION="" MAILBOX="" CALLBACK=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode) shift 2 ;;
    --name) NAME="$2"; shift 2 ;;
    --location) LOCATION="$2"; shift 2 ;;
    --subscription) SUBSCRIPTION="$2"; shift 2 ;;
    --agent-mailbox) MAILBOX="$2"; shift 2 ;;
    --callback-url) CALLBACK="$2"; shift 2 ;;
    -h|--help) sed -n '2,8p' "$0"; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; exit 2 ;;
  esac
done
[[ -n "$MAILBOX" && -n "$CALLBACK" ]] || { echo "--agent-mailbox and --callback-url are required" >&2; exit 2; }
[[ "$CALLBACK" == https://* ]] || { echo "--callback-url must be https://" >&2; exit 2; }

log()  { printf '\n==> %s\n' "$*"; }
warn() { printf 'WARNING: %s\n' "$*" >&2; }
command -v az >/dev/null || { echo "Azure CLI (az) is required" >&2; exit 1; }

[[ -n "$SUBSCRIPTION" ]] && az account set --subscription "$SUBSCRIPTION"
az account show >/dev/null 2>&1 || az login >/dev/null
TENANT_ID="$(az account show --query tenantId -o tsv)"
RG="rg-$NAME-cal"
GRAPH_APP=00000003-0000-0000-c000-000000000000

log "Resource providers and resource group"
az provider register --namespace Microsoft.BotService --wait >/dev/null
az group create --name "$RG" --location "$LOCATION" >/dev/null

log "Entra app registration (single tenant)"
APP_NAME="$NAME-teams-voice-calendar"
APP_ID="$(az ad app list --filter "displayName eq '$APP_NAME'" --query "[0].appId" -o tsv)"
if [[ -z "$APP_ID" ]]; then
  APP_ID="$(az ad app create --display-name "$APP_NAME" --sign-in-audience AzureADMyOrg --query appId -o tsv)"
  az ad sp create --id "$APP_ID" >/dev/null
fi

log "Graph application permissions: Calendars.Read, Calls.JoinGroupCall.All"
role_id() { az ad sp show --id "$GRAPH_APP" --query "appRoles[?value=='$1' && contains(allowedMemberTypes,'Application')].id | [0]" -o tsv; }
az ad app permission add --id "$APP_ID" --api "$GRAPH_APP" \
  --api-permissions "$(role_id Calendars.Read)=Role" "$(role_id Calls.JoinGroupCall.All)=Role" >/dev/null
az ad app permission admin-consent --id "$APP_ID" >/dev/null 2>&1 \
  || warn "Admin consent failed. A tenant admin must grant it: https://login.microsoftonline.com/$TENANT_ID/adminconsent?client_id=$APP_ID"

log "Azure Bot with the Teams calling webhook set to $CALLBACK"
az deployment group create --resource-group "$RG" --name teams-voice-calendar --template-file calendar.bicep \
  --parameters prefix="$NAME" botAppId="$APP_ID" callbackUrl="$CALLBACK" >/dev/null

ENV_FILE="$ROOT/.env.calendar"
if grep -q '^AZURE_CLIENT_SECRET=' "$ENV_FILE" 2>/dev/null; then
  SECRET="$(grep '^AZURE_CLIENT_SECRET=' "$ENV_FILE" | cut -d= -f2-)"
else
  log "Creating a client secret (stored only in $ENV_FILE)"
  SECRET="$(az ad app credential reset --id "$APP_ID" --append --display-name teams-voice-calendar --years 1 --query password -o tsv)"
fi
umask 077
cat > "$ENV_FILE" <<ENV
JOIN_MODE=calendar
AGENT_MAILBOX=$MAILBOX
CALLBACK_URL=$CALLBACK
GRAPH_TENANT_ID=$TENANT_ID
AZURE_TENANT_ID=$TENANT_ID
AZURE_CLIENT_ID=$APP_ID
AZURE_CLIENT_SECRET=$SECRET
ENV

log "Python CLI"
[[ -d "$ROOT/.venv" ]] || python3 -m venv "$ROOT/.venv"
"$ROOT/.venv/bin/python" -m pip install -q -e "$ROOT"

log "Done. Remaining manual steps"
cat <<EOT
  1. Exchange admin: make the agent mailbox accept invites automatically, and limit the app to that mailbox:
       Set-CalendarProcessing -Identity $MAILBOX -AutomateProcessing AutoAccept
       (and an Exchange application access policy or RBAC for Applications scoped to $MAILBOX)
  2. Host the receiver at $CALLBACK:
       set -a; . $ENV_FILE; set +a; $ROOT/.venv/bin/teams-voice run
  3. Test: $ROOT/.venv/bin/teams-voice calendar list   then invite $MAILBOX to a Teams meeting.
  Limit: the agent joins the meeting but has no live audio to a voice agent yet (see README).
EOT
