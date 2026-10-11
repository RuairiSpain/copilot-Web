# Authentication, routing profiles and Foundry Agent Service

## Who may call the router

Every routing endpoint (`/v1/chat/completions`, `/openai/v1/chat/completions`, `/v1/route`)
requires a credential. `/health/*` and `/metrics` do not. Code: `decision-router/decision_router/inbound.py`.

Two kinds of credential are accepted. Configure one or both; when both are configured, a
request may present either one.

| Credential | Configure | The caller sends |
|---|---|---|
| API key | `ROUTER_API_KEYS=key1,key2` (several keys allow rotation) | `api-key: <key>`, `x-api-key: <key>` or `Authorization: Bearer <key>` |
| Microsoft Entra ID token | `ROUTER_ENTRA_TENANT_ID`, `ROUTER_ENTRA_AUDIENCE`, optionally `ROUTER_ENTRA_ALLOWED_CLIENT_IDS` | `Authorization: Bearer <access token>` |

An Entra token can come from a managed identity (an Azure workload, or a Foundry project's
identity calling through API Management) or from an app registration using OAuth 2.0 client
credentials. The router checks:

- the signature, against the tenant's published signing keys (fetched once and cached for 24 hours);
- the issuer (v1.0 and v2.0 tokens are both accepted);
- the audience (`ROUTER_ENTRA_AUDIENCE`, for example `api://decision-router`);
- the expiry;
- the calling client: when `ROUTER_ENTRA_ALLOWED_CLIENT_IDS` is set, the token's `azp` (or `appid`)
  must be one of those IDs. Leave it unset to accept any client in the tenant that can get a
  token for the audience.

Failures return `401` with an OpenAI-style error and `WWW-Authenticate: Bearer`. The decision
log records who called: `"caller": {"method": "api_key", "identity": "1"}` (the key's position
in `ROUTER_API_KEYS`) or `{"method": "entra", "identity": "<client id>"}`.

With neither configured, the router refuses to start. Set `ROUTER_ALLOW_UNAUTHENTICATED=true`
only when something in front of it already authenticates callers. The Azure Machine Learning
deployment does this, because its managed online endpoint uses `auth_mode: aad_token`.

To set up Entra ID: register an application (or reuse one) to represent the router, set its
Application ID URI (for example `api://decision-router`), and use that URI as
`ROUTER_ENTRA_AUDIENCE`. Callers request a token for `api://decision-router/.default`.

## Routing profiles: choosing a mode by model name

Some callers can't add `routing_mode`, `compatibility` or `routing_constraints` to the body,
Foundry Agent Service among them. For those callers, the `model` field can name a **profile**
that supplies those fields:

| `model` | Effect |
|---|---|
| `decision-router-cost` | `routing_mode: cost` |
| `decision-router-balanced` | `routing_mode: balanced` |
| `decision-router-quality` | `routing_mode: quality` |
| anything else | no profile; the router's defaults apply (as before) |

Add or replace profiles with `ROUTER_PROFILES`, a JSON object of name → fields. A profile can
set any of `routing_mode`, `compatibility`, `routing_constraints` and `claude_translation`:

```json
{"agent-eu-support": {"routing_mode": "cost", "compatibility": "new",
                      "routing_constraints": {"inference_in_azure": true, "region": "swedencentral"}},
 "agent-research":   {"routing_mode": "quality", "compatibility": "new"}}
```

Fields in the request win over the profile. The decision log records the profile used
(`"profile": "agent-eu-support"`), and `/health/ready` lists the configured profile names.

This is the same idea as Microsoft's advice to create several Model Router deployments with
different modes and model subsets and give each agent its own: here, each agent gets its own
profile name on one router.

## Using the router from a Foundry agent

Foundry Agent Service can call models through a **model connection** (Microsoft docs: "Bring
your own model with the AI Gateway"). Your code talks to the agent with the Responses API.
**The agent calls the connected model with Chat Completions**: Foundry appends
`chat/completions` to the connection's base URL, and models behind a connection "must implement
the OpenAI-compatible chat completions API". That is the API this router serves, so no
Responses endpoint is needed.

1. In the Foundry portal: **Manage** → **Resource details** → **Admin-connected models** → **Add**.
2. **Connection type:** *Other source* (router called directly) or *Azure API Management*
   (router behind APIM).
3. **Base URL:** `https://<router-host>/openai/v1`. The agent then calls
   `https://<router-host>/openai/v1/chat/completions`.
4. **Authentication:**
   - *Other source* offers **API key** or **OAuth 2.0**. For an API key, set the header name to
     `api-key` (or `Authorization` with value `Bearer {api_key}`). For OAuth 2.0, enter an app
     registration's client ID and secret, token URL
     `https://login.microsoftonline.com/<tenant-id>/oauth2/v2.0/token` and scope
     `api://decision-router/.default`, and add that app's client ID to
     `ROUTER_ENTRA_ALLOWED_CLIENT_IDS`.
   - *Azure API Management* offers **API key** or **Managed identity**. With managed identity,
     the Foundry project's identity gets a token, and APIM validates it and forwards the request.
     The router can then trust APIM through an API key that APIM adds, or validate the same
     token itself if APIM passes the `Authorization` header through.
5. **Models:** add one entry per profile, such as `decision-router-cost` and
   `decision-router-quality`. In code the agent refers to `<connection-name>/decision-router-cost`.

What Microsoft's page says about this setup: it works for prompt agents only. The supported
tools are Code Interpreter, Functions, File Search, OpenAPI, Foundry IQ, SharePoint Grounding,
Fabric Data Agent, MCP and Browser Automation. The router must be reachable from Agent
Service's network.

### Tools

Agent Service runs its tools and sends the model chat-completions requests. Any tool the model
should call arrives in the `tools` array, so stage 1 removes models that can't call tools
through Chat Completions:

- Phi models, Llama 4 Maverick (`tools: no`);
- gpt-6.1-sol, whose tool calling works only through the Responses API.

Claude models stay eligible: their tool calls are translated to and from the Messages API.
