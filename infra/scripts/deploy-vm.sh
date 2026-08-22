#!/usr/bin/env bash
# Deploys the absolute-cheapest architecture: one VM running the app and
# Postgres together via Docker Compose, with Caddy for free automatic
# HTTPS. See ../README.md for the cost/tradeoffs vs. the default
# (Container Apps) path in ./deploy.sh.
#
# Required env vars:
#   NAME_PREFIX          short, unique-ish resource name prefix
#   SSH_PUBLIC_KEY_FILE   path to your SSH public key, e.g. ~/.ssh/id_ed25519.pub
#   DOMAIN               a domain/subdomain you own, with its A record
#                         already pointed at the VM's public IP (this
#                         script prints that IP — you add the DNS record,
#                         then re-run the docker-compose-up step below)
#   CONTAINER_IMAGE      e.g. ghcr.io/you/copilot-web:latest
#   GITHUB_CLIENT_ID / GITHUB_CLIENT_SECRET   from your GitHub OAuth App
#
# Optional (auto-generated if unset): AUTH_SECRET, TOKEN_ENCRYPTION_KEY,
# POSTGRES_ADMIN_PASSWORD. Optional: LOCATION (default eastus),
# ALLOWED_SSH_CIDR (default: your current public IP /32).
set -euo pipefail

command -v az >/dev/null || { echo "Azure CLI (az) is required" >&2; exit 1; }
command -v openssl >/dev/null || { echo "openssl is required" >&2; exit 1; }

: "${NAME_PREFIX:?Set NAME_PREFIX, e.g. copilotweb}"
: "${SSH_PUBLIC_KEY_FILE:?Set SSH_PUBLIC_KEY_FILE, e.g. ~/.ssh/id_ed25519.pub}"
: "${DOMAIN:?Set DOMAIN — a domain/subdomain you own}"
: "${CONTAINER_IMAGE:?Set CONTAINER_IMAGE, e.g. ghcr.io/you/copilot-web:latest}"
: "${GITHUB_CLIENT_ID:?Set GITHUB_CLIENT_ID from your GitHub OAuth App}"
: "${GITHUB_CLIENT_SECRET:?Set GITHUB_CLIENT_SECRET from your GitHub OAuth App}"

[ -f "$SSH_PUBLIC_KEY_FILE" ] || { echo "No such file: $SSH_PUBLIC_KEY_FILE" >&2; exit 1; }

LOCATION="${LOCATION:-eastus}"
AUTH_SECRET="${AUTH_SECRET:-$(openssl rand -base64 33)}"
TOKEN_ENCRYPTION_KEY="${TOKEN_ENCRYPTION_KEY:-$(openssl rand -base64 32)}"
POSTGRES_ADMIN_PASSWORD="${POSTGRES_ADMIN_PASSWORD:-$(openssl rand -base64 24)}"
MY_IP="$(curl -s https://ifconfig.me || true)"
ALLOWED_SSH_CIDR="${ALLOWED_SSH_CIDR:-${MY_IP}/32}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VM_DIR="$SCRIPT_DIR/../bicep-vm"

echo "==> Provisioning the VM (resource group '${NAME_PREFIX}-vm-rg' in $LOCATION)..."
DEPLOY_OUTPUT=$(az deployment sub create \
  --name "copilot-web-vm-$(date +%s)" \
  --location "$LOCATION" \
  --template-file "$VM_DIR/main.bicep" \
  --parameters \
    location="$LOCATION" \
    namePrefix="$NAME_PREFIX" \
    sshPublicKey="$(cat "$SSH_PUBLIC_KEY_FILE")" \
    allowedSshSourceCidr="$ALLOWED_SSH_CIDR" \
  --query properties.outputs -o json)

VM_IP=$(echo "$DEPLOY_OUTPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["publicIpAddress"]["value"])')
RG_NAME=$(echo "$DEPLOY_OUTPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["resourceGroupName"]["value"])')
VM_NAME="${NAME_PREFIX}-vm"

echo ""
echo "VM public IP: $VM_IP"
echo "Point ${DOMAIN}'s DNS A record at this IP now if you haven't already, then press Enter to continue."
read -r _

# .env for docker-compose — pushed to the VM below, never committed.
ENV_CONTENT=$(cat <<EOF
CONTAINER_IMAGE=${CONTAINER_IMAGE}
DOMAIN=${DOMAIN}
POSTGRES_PASSWORD=${POSTGRES_ADMIN_PASSWORD}
PORT=3000
NODE_ENV=production
AUTH_URL=https://${DOMAIN}
AUTH_TRUST_HOST=true
AUTH_GITHUB_ID=${GITHUB_CLIENT_ID}
AUTH_GITHUB_SECRET=${GITHUB_CLIENT_SECRET}
AUTH_SECRET=${AUTH_SECRET}
TOKEN_ENCRYPTION_KEY=${TOKEN_ENCRYPTION_KEY}
DATABASE_URL=postgresql://copilot:${POSTGRES_ADMIN_PASSWORD}@db:5432/copilot_web
EOF
)

echo "==> Pushing docker-compose.yml, Caddyfile, and .env to the VM..."
COMPOSE_B64=$(base64 -w0 "$VM_DIR/docker-compose.yml")
CADDYFILE_B64=$(base64 -w0 "$VM_DIR/Caddyfile")
ENV_B64=$(echo "$ENV_CONTENT" | base64 -w0)

az vm run-command invoke \
  --resource-group "$RG_NAME" \
  --name "$VM_NAME" \
  --command-id RunShellScript \
  --scripts "
    mkdir -p /opt/copilot-web
    echo '$COMPOSE_B64' | base64 -d > /opt/copilot-web/docker-compose.yml
    echo '$CADDYFILE_B64' | base64 -d > /opt/copilot-web/Caddyfile
    echo '$ENV_B64' | base64 -d > /opt/copilot-web/.env
    chmod 600 /opt/copilot-web/.env
    cd /opt/copilot-web && docker compose pull && docker compose up -d
  " -o none

cat <<EOF

==> Deployed.

  App URL:          https://${DOMAIN}
  VM:                ${VM_NAME} (${VM_IP})
  Resource group:    ${RG_NAME}
  SSH:               ssh copilotadmin@${VM_IP}

Next steps:

1. Update your GitHub OAuth App's callback URL to:
     https://${DOMAIN}/api/auth/callback/github

2. Run database migrations (Postgres is only reachable from the VM
   itself, not the public internet, so run this over SSH, inside the
   already-running app container):
     ssh copilotadmin@${VM_IP} 'cd /opt/copilot-web && docker compose exec app npx prisma migrate deploy'

3. Save these generated secrets somewhere safe (not git):
     AUTH_SECRET=${AUTH_SECRET}
     TOKEN_ENCRYPTION_KEY=${TOKEN_ENCRYPTION_KEY}
     POSTGRES_ADMIN_PASSWORD=${POSTGRES_ADMIN_PASSWORD}

4. Visit https://${DOMAIN} and sign in. Caddy takes 10-30s to obtain its
   first Let's Encrypt certificate — expect a certificate warning for the
   first request or two.

To tear everything down later:
  az group delete --name ${RG_NAME} --yes --no-wait
EOF
