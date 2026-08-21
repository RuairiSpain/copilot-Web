#!/usr/bin/env bash
# Gates the whole Container App behind Microsoft Entra ID SSO — nobody
# reaches the app (not even the login page) without signing in with a
# Microsoft account first. This is Container Apps' built-in
# "Easy Auth" feature: free, no app code changes, enforced at the
# platform edge before a request ever reaches the container.
#
# This is a SECOND, outer layer in front of the app's own GitHub login —
# it answers "who's allowed to even open this app" (your org / specific
# Microsoft accounts), not "which GitHub repos can this session touch"
# (still GitHub OAuth, inside the app, unchanged). Users pass both: Entra
# ID first, then GitHub login as normal once they're in.
#
# Required env vars:
#   RESOURCE_GROUP, CONTAINER_APP_NAME   from your `deploy.sh` output
#   APP_DISPLAY_NAME                      e.g. "Copilot Web"
#
# Optional:
#   SIGN_IN_AUDIENCE   AzureADMyOrg (default — only your tenant) or
#                       AzureADMultipleOrgs (any work/school account)
set -euo pipefail

command -v az >/dev/null || { echo "Azure CLI (az) is required" >&2; exit 1; }

: "${RESOURCE_GROUP:?Set RESOURCE_GROUP}"
: "${CONTAINER_APP_NAME:?Set CONTAINER_APP_NAME}"
: "${APP_DISPLAY_NAME:?Set APP_DISPLAY_NAME, e.g. 'Copilot Web'}"
SIGN_IN_AUDIENCE="${SIGN_IN_AUDIENCE:-AzureADMyOrg}"

TENANT_ID=$(az account show --query tenantId -o tsv)
APP_FQDN=$(az containerapp show -g "$RESOURCE_GROUP" -n "$CONTAINER_APP_NAME" --query properties.configuration.ingress.fqdn -o tsv)
REDIRECT_URI="https://${APP_FQDN}/.auth/login/aad/callback"

echo "==> Creating the Entra ID App Registration..."
APP_ID=$(az ad app create \
  --display-name "$APP_DISPLAY_NAME" \
  --sign-in-audience "$SIGN_IN_AUDIENCE" \
  --web-redirect-uris "$REDIRECT_URI" \
  --enable-id-token-issuance true \
  --query appId -o tsv)

# A corresponding service principal is what actually lets people in your
# tenant sign in to this app — the app registration alone isn't enough.
az ad sp create --id "$APP_ID" -o none 2>/dev/null || echo "(service principal already exists, continuing)"

echo "==> Creating a client secret..."
CLIENT_SECRET=$(az ad app credential reset --id "$APP_ID" --append --years 2 --query password -o tsv)

echo "==> Wiring it up to the Container App..."
az containerapp auth microsoft update \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP_NAME" \
  --client-id "$APP_ID" \
  --client-secret "$CLIENT_SECRET" \
  --issuer "https://login.microsoftonline.com/${TENANT_ID}/v2.0" \
  -o none

echo "==> Requiring authentication for every request..."
# NOTE: verify --unauthenticated-client-action's exact accepted values and
# whether a --redirect-provider flag is needed on your installed az CLI
# version (`az containerapp auth update --help`) before relying on this —
# the auth CLI surface has changed across versions. RedirectToLoginPage
# sends anyone unauthenticated straight to the Microsoft login page;
# Return401 is the API-friendly alternative if you'd rather the app (or a
# client script) handle the redirect itself.
az containerapp auth update \
  --resource-group "$RESOURCE_GROUP" \
  --name "$CONTAINER_APP_NAME" \
  --unauthenticated-client-action RedirectToLoginPage \
  -o none

cat <<EOF

==> Done. https://${APP_FQDN} now requires Microsoft sign-in before anyone
    reaches it — including the GitHub login page.

Entra ID App Registration: ${APP_ID} (tenant ${TENANT_ID})
Sign-in audience: ${SIGN_IN_AUDIENCE}

To restrict it further to specific people (rather than "anyone in this
tenant"), go to Entra ID > Enterprise Applications > ${APP_DISPLAY_NAME} >
Properties, set "Assignment required?" to Yes, then add specific
users/groups under "Users and groups".

To undo:
  az containerapp auth update -g "$RESOURCE_GROUP" -n "$CONTAINER_APP_NAME" \\
    --unauthenticated-client-action AllowAnonymous
EOF
