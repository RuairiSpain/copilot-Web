#!/usr/bin/env bash
# Deploys the default (cheapest-that-actually-works) architecture:
# Azure Container Apps + Postgres Flexible Server + Key Vault.
# See ../README.md for the full walkthrough and cost breakdown.
#
# Required env vars (secrets — never commit these):
#   NAME_PREFIX                short, unique-ish resource name prefix
#   CONTAINER_IMAGE             e.g. ghcr.io/you/copilot-web:latest
#   REGISTRY_USERNAME           your GitHub username (for ghcr.io)
#   REGISTRY_PASSWORD           a GitHub PAT with read:packages
#   GITHUB_CLIENT_ID            from your GitHub OAuth App
#   GITHUB_CLIENT_SECRET        from your GitHub OAuth App
#
# Optional (auto-generated if unset):
#   AUTH_SECRET, TOKEN_ENCRYPTION_KEY, POSTGRES_ADMIN_PASSWORD
#
# Optional:
#   LOCATION (default eastus), ALLOW_MY_IP (default: your current
#   public IP, added as a Postgres firewall rule so `prisma migrate
#   deploy` can run from here afterwards)
set -euo pipefail

command -v az >/dev/null || { echo "Azure CLI (az) is required: https://learn.microsoft.com/cli/azure/install-azure-cli" >&2; exit 1; }
command -v openssl >/dev/null || { echo "openssl is required" >&2; exit 1; }

: "${NAME_PREFIX:?Set NAME_PREFIX, e.g. copilotweb}"
: "${CONTAINER_IMAGE:?Set CONTAINER_IMAGE, e.g. ghcr.io/you/copilot-web:latest}"
: "${REGISTRY_USERNAME:?Set REGISTRY_USERNAME (your GitHub username)}"
: "${REGISTRY_PASSWORD:?Set REGISTRY_PASSWORD (a GitHub PAT with read:packages)}"
: "${GITHUB_CLIENT_ID:?Set GITHUB_CLIENT_ID from your GitHub OAuth App}"
: "${GITHUB_CLIENT_SECRET:?Set GITHUB_CLIENT_SECRET from your GitHub OAuth App}"

LOCATION="${LOCATION:-eastus}"
AUTH_SECRET="${AUTH_SECRET:-$(openssl rand -base64 33)}"
TOKEN_ENCRYPTION_KEY="${TOKEN_ENCRYPTION_KEY:-$(openssl rand -base64 32)}"
POSTGRES_ADMIN_PASSWORD="${POSTGRES_ADMIN_PASSWORD:-$(openssl rand -base64 24)}"

MY_IP="${ALLOW_MY_IP:-$(curl -s https://ifconfig.me || true)}"
if [ -n "$MY_IP" ]; then
  ALLOWED_RANGES_JSON="[{\"name\":\"deployer\",\"startIp\":\"$MY_IP\",\"endIp\":\"$MY_IP\"}]"
  echo "Allowing $MY_IP through the Postgres firewall (so migrations can run from here) — see infra/README.md to remove this later."
else
  ALLOWED_RANGES_JSON="[]"
  echo "Could not determine your public IP; skipping the Postgres firewall rule. Add one yourself (or run this from the VM/CI that will run migrations) before running prisma migrate deploy." >&2
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BICEP_DIR="$SCRIPT_DIR/../bicep"

echo "==> Deploying (this creates a new resource group '${NAME_PREFIX}-rg' in $LOCATION)..."
DEPLOY_OUTPUT=$(az deployment sub create \
  --name "copilot-web-$(date +%s)" \
  --location "$LOCATION" \
  --template-file "$BICEP_DIR/main.bicep" \
  --parameters \
    location="$LOCATION" \
    namePrefix="$NAME_PREFIX" \
    containerImage="$CONTAINER_IMAGE" \
    registryUsername="$REGISTRY_USERNAME" \
    registryPassword="$REGISTRY_PASSWORD" \
    githubClientId="$GITHUB_CLIENT_ID" \
    githubClientSecret="$GITHUB_CLIENT_SECRET" \
    authSecret="$AUTH_SECRET" \
    tokenEncryptionKey="$TOKEN_ENCRYPTION_KEY" \
    postgresAdminPassword="$POSTGRES_ADMIN_PASSWORD" \
    postgresAllowedIpRanges="$ALLOWED_RANGES_JSON" \
  --query properties.outputs -o json)

APP_URL=$(echo "$DEPLOY_OUTPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["appUrl"]["value"])')
PG_FQDN=$(echo "$DEPLOY_OUTPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["postgresServerFqdn"]["value"])')
RG_NAME=$(echo "$DEPLOY_OUTPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["resourceGroupName"]["value"])')

cat <<EOF

==> Deployed.

  App URL:          $APP_URL
  Resource group:    $RG_NAME
  Postgres host:      $PG_FQDN

Next steps:

1. Update your GitHub OAuth App's callback URL to:
     ${APP_URL}/api/auth/callback/github

2. Run database migrations (from a machine allowed through the Postgres
   firewall — this script added yours if it could detect your IP):
     DATABASE_URL="postgresql://copilotadmin:${POSTGRES_ADMIN_PASSWORD}@${PG_FQDN}:5432/copilot_web?sslmode=require" \\
       npx prisma migrate deploy

3. Save these generated secrets somewhere safe (a password manager, not
   git) — you'll need them again if you ever redeploy without setting
   them explicitly:
     AUTH_SECRET=${AUTH_SECRET}
     TOKEN_ENCRYPTION_KEY=${TOKEN_ENCRYPTION_KEY}
     POSTGRES_ADMIN_PASSWORD=${POSTGRES_ADMIN_PASSWORD}

4. Visit $APP_URL and sign in.

To tear everything down later:
  az group delete --name $RG_NAME --yes --no-wait
EOF
