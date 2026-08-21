# Infrastructure

Two deployable architectures, both as Bicep + a plain `az` CLI script (no
Bicep knowledge required to run either), plus two optional addons. All
templates are validated with the Bicep compiler (`bicep build`) — see
**Validation** at the bottom for how, and what wasn't (and can't be)
checked without a real Azure subscription.

| Path | What it is | Est. monthly cost* |
| --- | --- | --- |
| **`bicep/` (default)** | Azure Container Apps + Postgres Flexible Server + Key Vault | **~$25-40** |
| `bicep-vm/` (cheapest) | One small VM, app + Postgres via Docker Compose, Caddy for free TLS | **~$10-12** |
| `bicep-addons/frontdoor-waf.bicep` | Front Door (CDN + edge) + WAF, in front of either path | +$35/mo (Standard) or +$330/mo (Premium) |
| `scripts/enable-entra-sso.sh` | Gate the whole app behind Microsoft Entra ID sign-in | **free** |

\* East US, on-demand pricing, per official Azure pricing pages and
third-party trackers as of mid-2026 (see citations throughout). Azure
pricing varies by region and changes over time — treat these as
ballpark, and run `az deployment sub create --what-if` before applying
anything for real numbers.

## Why not GitHub Pages for the frontend?

Worth addressing head-on, because it's the natural instinct for "cheapest
static site hosting" and it doesn't fit this app. GitHub Pages (and Azure
Static Web Apps' free tier, for the same reason) only serves static files
— no server-side code at request time. This app isn't a static frontend
talking to a separate API; it's a single Next.js server doing all of:

- **Server-rendered pages** that check your login session and query the
  database before responding (the sessions list, the chat screen, the
  settings screen) — there's no build-time HTML to export, because the
  content depends on who's asking.
- **A WebSocket connection** (`/ws/sessions/:id`) held open for the
  lifetime of a chat session, terminated on the same process that holds
  the live `CopilotSession` in memory. A static host has no server to hold
  that connection at all.
- **One always-on Node.js process**, full stop — `@github/copilot-sdk`
  spawns a native runtime per session (see the main README), which is
  incompatible with static hosting, and with serverless/edge functions
  too, not just GitHub Pages specifically.

Splitting this into "static frontend + API backend" would mean rewriting
the app as a client-side SPA that does everything — including the
GitHub-OAuth-cookie session check that currently happens server-side
before a page ever renders — via client-side fetches to a separately
hosted API, on a different domain, with the cross-site cookie/CORS
complications that implies. That's a real rearchitecture, not a
deployment choice, and isn't part of this infra work. Both paths below
deploy the app as what it actually is: one server.

## Default: Container Apps + Postgres Flexible Server (`bicep/`)

Azure Container Apps runs the existing `Dockerfile` on Consumption pricing
(pay for actual vCPU-seconds/GiB-seconds used, not a reserved VM), with a
managed Postgres database and secrets in Key Vault pulled via the
container's own managed identity — nothing sensitive lives in the
Container App definition itself.

**Why `minReplicas: 1`, not 0 (the usual Container Apps cost trick):**
scaling to zero would kill the whole point of **auto** mode — an
in-memory `CopilotSession` dies with its replica, so a session "working
in the background while you're offline" can't survive an idle scale-down.
One always-on replica at the smallest size (0.25 vCPU / 0.5 GiB) costs
roughly $14-20/mo net of Container Apps' monthly free grant (180,000
vCPU-seconds + 360,000 GiB-seconds + 2M requests, per [Azure's Container
Apps pricing page](https://azure.microsoft.com/en-us/pricing/details/container-apps/)) —
plus Postgres Flexible Server Burstable B1ms at ~$12-13/mo compute (per
[bytebase's Azure Postgres pricing tracker](https://www.bytebase.com/dbcost/azure-flexible/instance/B1ms/),
consistent with Azure's own pricing page), plus a few dollars of Postgres
storage/backup and Log Analytics ingestion. **~$25-40/mo all-in.**

### Deploy

```bash
# 1. Create a GitHub OAuth App: https://github.com/settings/developers
#    (callback URL comes from step 3's output — you'll update it after)
#
# 2. Build & push the image somewhere Container Apps can pull from.
#    Cheapest option: GitHub Container Registry (free), via the
#    repo's own Dockerfile:
docker build -t ghcr.io/YOUR_GH_USERNAME/copilot-web:latest .
echo "$GITHUB_PAT" | docker login ghcr.io -u YOUR_GH_USERNAME --password-stdin
docker push ghcr.io/YOUR_GH_USERNAME/copilot-web:latest
#    (or let .github/workflows/deploy.yml do this for you — see CI/CD below)

# 3. Deploy
export NAME_PREFIX=copilotweb
export CONTAINER_IMAGE=ghcr.io/YOUR_GH_USERNAME/copilot-web:latest
export REGISTRY_USERNAME=YOUR_GH_USERNAME
export REGISTRY_PASSWORD=<a GitHub PAT with read:packages>
export GITHUB_CLIENT_ID=<from step 1>
export GITHUB_CLIENT_SECRET=<from step 1>
./infra/scripts/deploy.sh
```

The script prints your app's URL, generates `AUTH_SECRET` /
`TOKEN_ENCRYPTION_KEY` / the Postgres password if you didn't set them,
and tells you exactly what to do next (update the OAuth App's callback
URL, run `prisma migrate deploy`).

### CI/CD (optional)

`.github/workflows/deploy.yml` builds the image, pushes to GHCR, and
updates the Container App — manual (`workflow_dispatch`) by default; flip
the trigger to `push: { branches: [main] }` once you trust it. It logs
into Azure via OIDC federated credentials, not a stored client secret:

```bash
# One-time setup — create an app registration + federated credential
# trusting this repo's GitHub Actions:
az ad app create --display-name copilot-web-deploy --query appId -o tsv
# (note the appId, then:)
az ad app federated-credential create --id <appId> --parameters '{
  "name": "github-actions",
  "issuer": "https://token.actions.githubusercontent.com",
  "subject": "repo:YOUR_GH_ORG/copilot-Web:ref:refs/heads/main",
  "audiences": ["api://AzureADTokenExchange"]
}'
az role assignment create --assignee <appId> --role Contributor \
  --scope "/subscriptions/$(az account show --query id -o tsv)/resourceGroups/copilotweb-rg"
```

Then set repo secrets `AZURE_CLIENT_ID` (the appId above),
`AZURE_TENANT_ID`, `AZURE_SUBSCRIPTION_ID`, and repo variables
`AZURE_RESOURCE_GROUP` / `AZURE_CONTAINER_APP_NAME` (from `deploy.sh`'s
output).

## Cheapest: one VM + Docker Compose (`bicep-vm/`)

A single `Standard_B1s` VM (~$7.59/mo compute per [Azure VM pricing
trackers](https://cloudprice.net/vm/Standard_B1s); +~$2-3/mo for a 30GB
SSD OS disk) running the app and Postgres together via the repo's own
`docker-compose.yml` pattern, with [Caddy](https://caddyserver.com/) doing
automatic free Let's Encrypt HTTPS. **~$10-12/mo total, no other Azure
resources.**

The tradeoff for that price: you own OS patching, Docker upgrades, and
backups (the Postgres data lives on the VM's disk — snapshot it
yourself, e.g. `az snapshot create`, on whatever schedule you're
comfortable with). Good fit if you're already comfortable SSHing into a
box; the default path above is the better fit if you'd rather Azure
manage all of that for a bit more money.

```bash
export NAME_PREFIX=copilotweb
export SSH_PUBLIC_KEY_FILE=~/.ssh/id_ed25519.pub
export DOMAIN=copilot.yourdomain.com   # A record must point at the VM's IP
export CONTAINER_IMAGE=ghcr.io/YOUR_GH_USERNAME/copilot-web:latest
export GITHUB_CLIENT_ID=<from your GitHub OAuth App>
export GITHUB_CLIENT_SECRET=<from your GitHub OAuth App>
./infra/scripts/deploy-vm.sh
```

The script provisions the VM, waits for you to point DNS at it, then
pushes `docker-compose.yml`/`Caddyfile`/`.env` over `az vm run-command`
and starts everything. See the script's header comment for exactly what
it does and why secrets are pushed this way rather than baked into the
VM's cloud-init (Azure doesn't fully protect `customData` from anyone with
Reader RBAC on the VM — fine for personal-scale secrets, worth knowing).

## Alternative: Application Firewall + CDN (Front Door)

Deploy the default path first, then:

```bash
export NAME_PREFIX=copilotweb
export ORIGIN_HOSTNAME=<containerAppFqdn output from deploy.sh>
az deployment group create \
  --resource-group "${NAME_PREFIX}-rg" \
  --template-file infra/bicep-addons/frontdoor-waf.bicep \
  --parameters namePrefix=$NAME_PREFIX originHostname=$ORIGIN_HOSTNAME
```

**Standard tier (default, ~$35/mo base + usage)**: CDN/global edge, TLS,
and a WAF policy with your own custom rules — this template includes a
per-IP rate limit (300 req/min by default) as a starting point. **Premium
tier (~$330/mo base)** additionally gets Microsoft-managed WAF rule sets
(OWASP) and bot protection — [per Azure's Front Door
pricing](https://azure.microsoft.com/en-us/pricing/details/frontdoor/)
and third-party trackers, Premium is roughly 10x Standard's base cost, so
only reach for it if you specifically need the managed rule sets or bot
protection; Standard's custom rules cover basic abuse (rate limiting,
geo-blocking, path/header matching) at a fraction of the price.

**Cheaper alternative achieving similar protection:** put
[Cloudflare's free tier](https://www.cloudflare.com/plans/) in front of
your domain instead — free CDN, basic WAF/firewall rules, and DDoS
protection, proxying to whichever Azure path you deployed. Not
Azure-native (one more account, one more DNS hop to manage), but genuinely
$0 versus Front Door Standard's ~$35/mo+ for a low-traffic personal tool.
Azure's edge network also already provides baseline DDoS protection
(Basic tier) automatically and for free at the platform level, regardless
of which of these you choose.

**Harden the origin once Front Door is in front of it:** without
restricting direct access, someone could still hit the Container App's
own `*.azurecontainerapps.io` URL and skip Front Door (and its WAF)
entirely. Have the app check Front Door's `X-Azure-FDID` request header
matches your profile's ID and reject anything without it — this is the
[documented, reliable way](https://learn.microsoft.com/en-us/azure/frontdoor/front-door-faq)
to lock an origin to Front Door traffic only; it's a small app-level
change, not something expressed in Bicep, so it's not included here.

## Alternative: restrict the whole app to Microsoft SSO

Yes — and it's free, not a paid add-on. Azure Container Apps (and App
Service) have **built-in authentication** ("Easy Auth"): configure it once
and the platform itself refuses any request that isn't signed in via
Microsoft Entra ID, *before* the request ever reaches your container. No
app code changes.

```bash
export RESOURCE_GROUP=copilotweb-rg
export CONTAINER_APP_NAME=copilotweb-app
export APP_DISPLAY_NAME="Copilot Web"
./infra/scripts/enable-entra-sso.sh
```

This is a **second, outer gate** in front of the app's own GitHub login,
not a replacement for it — the app still needs a GitHub token to act on
your repos, which only its own GitHub OAuth flow provides. With Entra SSO
enabled: Microsoft sign-in first (controls *who can even open the app* —
your whole tenant by default, or specific people/groups, see the script's
output for narrowing it), then GitHub login as normal *inside* the app
(controls *which GitHub repos it can act on*). Good fit if this is an
internal tool for your org and you want a hard perimeter beyond "whoever
has the URL and a GitHub account."

## Teardown

```bash
az group delete --name copilotweb-rg --yes --no-wait        # default path
az group delete --name copilotweb-vm-rg --yes --no-wait     # VM path
```

Front Door/WAF resources live in the same resource group as the default
path, so the first command removes those too. Entra ID app registrations
created by `enable-entra-sso.sh` are **not** removed by `az group
delete` (they're tenant-level, not resource-group-scoped) — delete with
`az ad app delete --id <appId>` if you're done with it.

## Folder structure

```
infra/
  bicep/                  default path: Container Apps + Postgres + Key Vault
    main.bicep             subscription-scope orchestrator
    main.parameters.json   template — copy, don't edit secrets into this file directly
    modules/
  bicep-vm/                cheapest path: VM + Docker Compose + Caddy
    main.bicep
    network.bicep
    vm.bicep
    docker-compose.yml     pushed to the VM by deploy-vm.sh, not run locally
    Caddyfile
  bicep-addons/
    frontdoor-waf.bicep    optional: CDN + WAF in front of either path
  scripts/
    deploy.sh               default path, wraps main.bicep
    deploy-vm.sh             VM path, wraps bicep-vm/main.bicep + pushes app files
    enable-entra-sso.sh      gates the app behind Microsoft SSO
```

## Validation

Every `.bicep` file here compiles cleanly with the Bicep CLI
(`bicep build`, no errors, no linter warnings after the two intentional
`#disable-next-line`s documented inline) — that catches resource-type
typos, missing required properties, and parameter/output wiring mistakes
without needing a live Azure subscription. It does **not** catch
everything: things like exact quota limits, regional SKU availability, or
IAM propagation timing only show up on a real `az deployment ... create`
(or `--what-if` first, which is free and doesn't create anything — do
that before your first real deploy). The `az containerapp auth` commands
in `enable-entra-sso.sh` are cited from Microsoft Learn's CLI reference
but not run end-to-end here (no Azure subscription in this environment) —
the script's inline comment flags the one flag worth double-checking
against your installed CLI version's `--help` output before relying on it.
