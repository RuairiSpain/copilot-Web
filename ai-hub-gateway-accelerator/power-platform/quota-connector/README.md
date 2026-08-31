# Quota Override Custom Connector

The Power Platform front door to `bicep/infra/modules/quota-service/quota-api.bicep`
— what `power-platform/quota-connector/canvas-app/`'s two screens call.
Deploy the authorization-gap fix first (the Quota Override API in APIM,
the `Quota.Approve` Entra app role) — this connector has nothing to talk
to without it. See `guides/quota-override-approval.md`'s "Implementation
status" for the full picture.

**HONESTY NOTE**: these files follow Microsoft's documented, stable
`paconn`/custom-connector file shapes from training knowledge — they were
**not** imported into a live Power Platform maker portal or Entra tenant
in this session (no Power Platform environment available here). Verify
the import actually succeeds in your own environment before relying on
it, same disclosure as everything else in this fork that couldn't be
executed.

## Files

| File | Purpose |
| --- | --- |
| `apiDefinition.swagger.json` | Swagger 2.0 (deliberately, not OpenAPI 3 — see below) — the two operations, `SubmitQuotaRequest` and `DecideQuotaRequest`, pointed at your APIM gateway's `/quota` path. |
| `apiProperties.json` | OAuth2 (Azure AD/Entra ID) connection settings — only consumed by the `paconn` CLI import path, not by a manual portal import (see "Two ways to import" below). |

### Why Swagger 2.0, not OpenAPI 3

Power Platform custom connectors now broadly support OpenAPI 3.0 imports
(rolled out around December 2025), but coverage wasn't confirmed as
universal across every tenant/region as of this fork's own research
(`guides/quota-override-approval.md`'s prior follow-up). Swagger 2.0 is
the long-established, unambiguously-supported format — the safer choice
for something meant to just work, not a bet on rollout completeness in
your specific tenant. If your tenant's connector wizard handles OpenAPI 3
cleanly, converting is a mechanical exercise, not a redesign — the
operations/schemas are the same either way.

## Before you start

1. The **authorization-gap fix** must be deployed first: the
   `Quota.Approve` Entra app role
   (`bicep/infra/entra-id-setup/setup.ps1`) and the Quota Override API
   (`bicep/infra/modules/quota-service/quota-api.bicep`) in front of
   `src/quota-service`.
2. Know your APIM gateway's hostname (e.g. `my-citadel-gw.azure-api.net`)
   and the gateway's Entra app registration's **client ID** and **tenant
   ID** (the same app `entra-id-setup/setup.ps1` provisions/updates —
   nothing new to register here).
3. Whoever will **approve** requests needs the `Quota.Approve` app role
   assigned to them in Entra ID (Enterprise applications → the gateway
   app → Users and groups → Add assignment). Anyone with a valid
   sign-in can **submit** a request — no special role needed for that
   half.

## Fill in the placeholders

Both files have `REPLACE-WITH-...` placeholders:
- `apiDefinition.swagger.json`: `host` → your APIM gateway hostname.
- `apiProperties.json`: `clientId`/`AzureActiveDirectoryResourceId`/
  `resourceUri` → `api://<gateway-app-client-id>` (three occurrences,
  same value); `tenantId` → your Entra tenant ID.

## Two ways to import

**Option A — `paconn` CLI (uses both files, recommended):**

```bash
pip install paconn
paconn login
paconn create --api-def apiDefinition.swagger.json --api-prop apiProperties.json --secret <a-client-secret-if-your-flow-needs-one>
```

This is the only path that actually consumes `apiProperties.json`'s
OAuth2 settings automatically.

**Option B — Power Apps/Power Automate maker portal (manual, Swagger file
only):**

1. Maker portal → Data → Custom connectors → New custom connector →
   Import an OpenAPI file → select `apiDefinition.swagger.json`.
2. On the **Security** tab, the portal does *not* read
   `apiProperties.json` — re-enter the same OAuth2 settings by hand:
   Authentication type "OAuth 2.0", Identity Provider "Azure Active
   Directory", Client id / Client secret (from your Entra app
   registration), Authorization URL / Token URL (already filled correctly
   by the Swagger file's `securityDefinitions`), Resource URL =
   `api://<gateway-app-client-id>`, Scope = `user_impersonation`.
3. Save, then **Test** — create a new connection, sign in as yourself,
   and try `SubmitQuotaRequest` with a small test payload against a real
   (non-production) access contract before trusting it further.

## Using it from the canvas app

See `canvas-app/README.md` — once this connector shows up in Power Apps
Studio's Data panel, it's referenced as `QuotaConnector.SubmitQuotaRequest(...)`
/ `QuotaConnector.DecideQuotaRequest(...)` in Power Fx.
