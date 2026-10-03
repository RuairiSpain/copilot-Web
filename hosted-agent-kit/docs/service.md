# Standalone HTTP service

The same scheduler, packaged as a separate HTTP service (a backend-for-frontend) in front of Microsoft Foundry hosted agents. Use it when callers are not Python applications, or when you want one deployment that several applications share. If you write a Python FastAPI application, embed the kit instead (see the README and `sdk.md`). It decides
which Foundry session serves each request, so callers never handle session identifiers.

- **Stateless agents**: any free session may serve any request.
- **Stateful agents**: one session per (user, agent). The same user always returns to the same
  session.
- **Safe concurrency**: a session serves one request at a time, capacity is a hard limit, and
  waiting requests queue in order with a bounded depth and wait time.
- **Self-healing**: the pool reconciles with Foundry, removes failed sessions and replaces sessions
  that were deleted outside the service.

State is held in memory, so the service runs as **one replica**. Read the
[architecture](architecture.md) and [ADR 0003](adr/0003-single-replica-in-memory-state.md)
before deploying.

## Quick start (local, no authentication)

Requirements: Python 3.12 or later.

```bash
pip install "hosted-agent-kit[service]"
az login                                   # or set AZURE_TENANT_ID, AZURE_CLIENT_ID, AZURE_CLIENT_SECRET
export FOUNDRY_PROJECT_ENDPOINT="https://<account>.services.ai.azure.com/api/projects/<project>"
export AGENT_POOL_CONFIG=./agent-pool.example.yaml   # list your own hosted agents here
export POOL_AUTH_MODE=development                    # local only: disables token checks
hack serve
```

```bash
curl -s localhost:8080/v1/agents/coding-agent/chat \
  -H 'content-type: application/json' -H 'X-Dev-User-Id: alice' \
  -d '{"message": "Write a haiku about queues"}'
```

The agent names in `agent-pool.example.yaml` must match hosted agents in your Foundry project.
Interactive API documentation is served at `/docs`.

## API summary

| Method and path | Role | Purpose |
|---|---|---|
| `GET /v1/agents`, `GET /v1/agents/{agent}` | `Pool.Invoke` | Discover agents: protocol, streaming, endpoints, limits. No admin access needed. |
| `POST /v1/agents/{agent}/responses` | `Pool.Invoke` | Call a Responses agent. JSON envelope, or SSE with `stream: true`. |
| `POST /v1/agents/{agent}/invocations` | `Pool.Invoke` | Call an Invocations agent with any request body (JSON of any shape, text, multipart, binary). The agent's response comes back as it is, or in an envelope with `?envelope=true`. |
| `POST /v1/agents/{agent}/chat` | `Pool.Invoke` | Chat with a Responses agent. `stream: true` returns SSE. |
| `POST /v1/agents/{agent}/invoke` | `Pool.Invoke` | **Deprecated.** One endpoint whose response shape depends on the agent. Use `responses` or `invocations`. |
| `GET /v1/admin/agents` | `Pool.Admin` | Pool counts, queue depth, generation and conditions per agent. |
| `GET /v1/admin/agents/{agent}` | `Pool.Admin` | One pool as spec and status: conditions, observed generation and recent events. |
| `GET /v1/admin/events` | `Pool.Admin` | What the controllers did and why, newest first. |
| `POST /v1/admin/config/reload` | `Pool.Admin` | Re-read and apply the agent pool document without a restart. |
| `GET /v1/admin/agents/{agent}/sessions` | `Pool.Admin` | Sessions. User ids are redacted. |
| `GET /v1/admin/agents/{agent}/sessions/{id}` | `Pool.Admin` | One session. |
| `DELETE /v1/admin/agents/{agent}/sessions/{id}` | `Pool.Admin` | Delete (202 if leased, deferred). |
| `POST /v1/admin/agents/{agent}/sync` | `Pool.Admin` | Reconcile now. 409 if already running. |
| `GET /v1/admin/metrics`, `GET /metrics` | `Pool.Admin` | JSON snapshot, Prometheus text (opt-in). |
| `GET /health/live`, `GET /health/ready` | none | Probes. |

Errors use `application/problem+json` with a stable `error_code`. See [errors](errors.md).

### Invocations agents

Set `protocol: invocations` on the agent and send any body to `invocations`. It is passed to the
agent as received, with its content type. The agent's body, status and content type come back;
upstream headers are dropped and the Foundry session id is removed from JSON bodies:

```bash
curl -s -X POST http://127.0.0.1:8080/v1/agents/pipeline-agent/invocations \
  -H 'content-type: application/json' \
  -d '[{"task": "summarise"}, {"task": "translate"}]'
```

Add `?envelope=true` to get one JSON shape (`request_id`, `status_code`, `content_type` and one of
`body_json`, `body_text`, `body_base64`). `responses` is for Responses agents only and refuses an
Invocations agent, and the other way round. See [configuration](configuration.md#protocols).

### Conversations

A stateful agent gives each user one session. Send `X-Conversation-Key` (or `conversation_key` in
the body) to give one user several independent conversations, each with its own session. The key is
opaque, up to 128 characters of `A-Za-z0-9._:-`, and is always scoped under the authenticated user.

### How it is built

The service follows Kubernetes control-plane patterns inside one process. Pools and sessions are
resources with a spec, a status, a generation, finalizers and conditions. Controllers reconcile
them from keyed work queues: a session is deleted remotely before its record goes, so a remote
session is never forgotten. A scheduler with filter and score plugins matches requests to sessions.
See [architecture](architecture.md) and [ADR 0009](adr/0009-control-plane-architecture.md).

### Callers that are services

A service calling for its users has one identity, so every user behind it would share one stateful
session. For stateful agents an app-only token is therefore refused (`403 END_USER_REQUIRED`) unless
the call names the end user in `X-Pool-Subject` and the token has the `Pool.Delegate` role. Sessions
are scoped to the calling service and that subject. `POOL_APP_ONLY_POLICY=allow` restores the old
behaviour. Delegated (user) tokens need nothing extra.

### Idempotency

Send `Idempotency-Key` on a non-streaming call. The first request runs. A repeat with the same
request returns the stored response with `Idempotent-Replayed: true`, for `POOL_IDEMPOTENCY_TTL_SECONDS`
(600). The same key with a different request is `422 IDEMPOTENCY_KEY_REUSED`, and a repeat while
the first is still running is `409 IDEMPOTENCY_IN_PROGRESS`. Failures are not stored, so a retry
runs again. The key also still lets the service retry once on a new session after a session failure.
State is in memory and does not survive a restart.

### Agent versions

Sessions are created on the latest agent version unless the agent sets `agent_version`. To roll out
a new version, set `version_drain` (`unbound` or `idle`) and the sync retires idle sessions on any
other version. See [operations](operations.md#rolling-out-a-new-agent-version).

### Client libraries

[Python](../clients/python/README.md) and [TypeScript](../clients/typescript/README.md) clients handle
authentication, problem details, retry guidance, correlation ids and event streams.

### Circuit breaker and restart recovery

Each agent has a circuit breaker. After repeated Foundry failures, calls return
`503 FOUNDRY_CIRCUIT_OPEN` with `Retry-After` until a probe succeeds.

Set `POOL_SESSION_ID_KEY` and stateful users get their sessions back after a restart. See
[configuration](configuration.md#restoring-stateful-sessions-after-a-restart).

## Configuration

Two layers: environment variables for the service ([`.env.example`](../.env.example)) and a YAML
document for the agent pool. The pool document is read from `AGENT_POOL_CONFIG`, or from the
`agentPool` section of `azure.yaml`. See [configuration](configuration.md).

## Run in a container

```bash
cp .env.example .env     # set FOUNDRY_PROJECT_ENDPOINT and credentials
docker compose up --build
```

## Deploy to Azure Container Apps

```bash
azd env set FOUNDRY_RESOURCE_GROUP <rg>
azd env set FOUNDRY_ACCOUNT_NAME <account>
azd env set FOUNDRY_PROJECT_NAME <project>
azd env set POOL_ENTRA_AUDIENCE api://<client-id>
azd up
```

The template creates a Container App pinned to one replica, a managed identity, a registry, Log
Analytics and Application Insights, and grants the identity a role on the Foundry project. Create
the Entra app registration and app roles first. See [operations](operations.md).

Optional, to restore stateful sessions after a restart:

```bash
azd env set POOL_SESSION_ID_KEY "$(openssl rand -hex 32)"
```

`azd` keeps environment values in a local file, so for production supply the key from a secret
store instead. The template passes it to the app as a Container App secret. Set
`POOL_FOUNDRY_ISOLATION_KEY` the same way only if an agent endpoint uses the Header authorisation
scheme.

## Develop and test

```bash
uv run ruff format --check src tests scripts clients
uv run ruff check src tests scripts clients
uv run mypy                   # src, tests, scripts and the Python client
(cd clients/typescript && npm ci && npm test)
uv run bandit -c pyproject.toml -r src
uv run python scripts/verify_versions.py
uv run pytest --cov           # adds the 90 percent statement and branch coverage gate
uv run python scripts/mutation_check.py --workers 4    # optional, slower
```

| Folder | Content |
|---|---|
| `tests/unit` | Pool, queue, affinity, scheduler, reconciler, circuit breaker, restart recovery, configuration, security, metrics. |
| `tests/integration` | The ASGI app with a fake Foundry behind the port. |
| `tests/contract` | The adapter driving the real Azure SDK clients over a fake HTTP layer, and the OpenAPI snapshot. |
| `tests/load` | 100 concurrent requests, queue isolation, lease exclusivity, memory stability. |

After an intentional API change, regenerate the snapshot with
`UPDATE_OPENAPI_SNAPSHOT=1 uv run pytest tests/contract/test_openapi_contract.py`.

## Limitations in V1

- One replica only. State is in memory.
- Stateful affinity survives a restart only if `POOL_SESSION_ID_KEY` is set. Without it, stateful
  agents ignore sessions they did not create and users get new sessions. With it, stateful agents
  cannot keep warm sessions, and changing the key orphans existing sessions. See
  [ADR 0007](adr/0007-derived-session-ids.md).
- The circuit breaker is per process, resets on restart and has no manual reset.
- Invocations responses are returned as the agent sends them. Only JSON bodies are scrubbed of the
  session id.
- Tested against the real Azure SDK clients over a fake HTTP layer. It has not been run against a
  live Foundry project from the build environment.

## Licence

MIT. See [LICENSE](../LICENSE).
