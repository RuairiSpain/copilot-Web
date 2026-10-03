# Configuration

## Environment variables

Prefix `POOL_` unless shown. Values are validated at start-up. A bad value stops the process with a
message that names the setting.

| Variable | Default | Purpose |
|---|---|---|
| `FOUNDRY_PROJECT_ENDPOINT` | none (required) | Foundry project endpoint. |
| `AGENT_POOL_CONFIG` | none | Path to a standalone pool config file. |
| `APPLICATIONINSIGHTS_CONNECTION_STRING` | none | Enables Azure Monitor export. |
| `POOL_SESSION_ID_KEY` | none | Secret (at least 32 characters) that derives each stateful user's session id. Setting it lets the pool find users' sessions again after a restart. Keep it stable. See below. |
| `POOL_FOUNDRY_ISOLATION_KEY` | none | Constant `x-ms-user-isolation-key` sent on every Foundry call. Only agents that use the Header authorisation scheme need it. |
| `POOL_STARTUP_SYNC_TIMEOUT_SECONDS` | `30` | SDK only: how long `Hack.start()` waits for the first sync with Foundry before it carries on. |
| `POOL_AUTH_MODE` | `entra` | `entra`, or `development` (no token checks, local only). |
| `POOL_ENTRA_TENANT_ID` | none | Required in `entra` mode. |
| `POOL_ENTRA_AUDIENCE` | none | Required in `entra` mode. Comma separated list allowed. |
| `POOL_ENTRA_AUTHORITY_HOST` | `https://login.microsoftonline.com` | Change for sovereign clouds. |
| `POOL_INVOKE_ROLE` / `POOL_ADMIN_ROLE` / `POOL_DIAGNOSTICS_ROLE` | `Pool.Invoke` / `Pool.Admin` / `Pool.Diagnostics` | Role names. |
| `POOL_APP_ONLY_POLICY` | `reject` | `reject` or `allow`. For stateful agents, `reject` refuses an app-only token that does not name the end user in `X-Pool-Subject`. |
| `POOL_DELEGATE_ROLE` | `Pool.Delegate` | Role an app-only caller needs to name an end user. |
| `POOL_IDEMPOTENCY_TTL_SECONDS` | 600 | How long a repeated `Idempotency-Key` replays the stored response. 0 turns deduplication off. |
| `POOL_IDEMPOTENCY_MAX_ENTRIES` | 1000 | Most stored keys. The oldest finished ones are dropped first. |
| `POOL_IDEMPOTENCY_MAX_BODY_BYTES` | 1048576 | Largest response that is stored. A larger one is returned but not replayed. |
| `POOL_JWKS_CACHE_SECONDS` | 3600 | Signing key cache lifetime. |
| `POOL_JWT_LEEWAY_SECONDS` | 60 | Clock skew allowance. |
| `POOL_MAX_BODY_BYTES` | 1048576 | Request body limit (413 above it). |
| `POOL_DEFAULT_TIMEOUT_SECONDS` | 120 | Used when a request gives no timeout. |
| `POOL_MAX_TIMEOUT_SECONDS` | 900 | Upper clamp for a request timeout. Applies to one upstream call. |
| `POOL_MAX_STREAM_SECONDS` | 600 | Longest a stream may run. |
| `POOL_MAX_REQUEST_SECONDS` | unset | Ceiling for a whole request before streaming starts: queue wait, session creation, backoff and every attempt. `504 REQUEST_TIMEOUT` when passed. |
| `POOL_MAX_RESPONSE_BYTES` | 16777216 | Largest non-streaming Invocations response the service will buffer (`502 RESPONSE_TOO_LARGE` above it). |
| `POOL_SHUTDOWN_GRACE_SECONDS` | 30 | On shutdown, how long to wait for running requests and streams after new ones are refused. |
| `POOL_RESERVATION_TTL_SECONDS` | 1800 | A capacity reservation that was never used or released is expired by garbage collection after this long. |
| `POOL_RETRY_AFTER_SECONDS` | 10 | `Retry-After` for queue and capacity errors. |
| `POOL_UPSTREAM_THROTTLE_RETRIES` | 2 | Retries after a Foundry 429. |
| `POOL_DELETE_RETRIES` | 3 | Retries for transient delete failures. |
| `POOL_CREATE_READY_TIMEOUT_SECONDS` | 120 | Wait for a new session to become active. |
| `POOL_BACKOFF_BASE_SECONDS` / `POOL_BACKOFF_MAX_SECONDS` | 0.5 / 30 | Retry backoff bounds. |
| `POOL_READINESS_PROBE_FOUNDRY` | false | Also call Foundry from `/health/ready`. |
| `POOL_METRICS_ENDPOINT_ENABLED` | false | Serve Prometheus text at `/metrics`. |
| `POOL_DIAGNOSTIC_SESSION_ID_HEADER` | false | Return `X-Pool-Session-Id` to the diagnostics role. |
| `POOL_LOG_LEVEL` | `INFO` | Log level. |
| `POOL_HOST` / `POOL_PORT` | `127.0.0.1` / 8080 | Bind address. The container image sets `0.0.0.0`. |

`POOL_DEFAULT_TIMEOUT_SECONDS` must not exceed `POOL_MAX_TIMEOUT_SECONDS`.

## Settings in the YAML file (`hack:`)

When you embed the kit, `Hack.from_yaml` also reads an optional top-level `hack:` section. Its keys
are the settings below without the `POOL_` prefix (for example `default_timeout_seconds`). Only the
runtime settings are allowed: the service's authentication and HTTP settings are not. Secrets
(`session_id_key`, `foundry_isolation_key`, `applicationinsights_connection_string`) are refused in
the file. An environment variable always wins over the file. See
`samples/config/hack-settings.yaml` in the package.

## Credentials for Foundry

The service uses `DefaultAzureCredential`. In Azure it uses the managed identity. For a
user-assigned identity, set `AZURE_CLIENT_ID` to its client id. Locally it can use `az login` or a
service principal (`AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, `AZURE_CLIENT_SECRET`).

## Agent pool document

```yaml
agentPool:
  defaults:            # optional, merged into every agent
    mode: stateless
    max_sessions: 10
  agents:
    coding-agent:
      mode: stateful
      min_warm_sessions: 1
      queue: { max_depth: 100, max_wait_seconds: 60 }
```

Precedence: the file named by `AGENT_POOL_CONFIG`, then `./azure.yaml`. A section is never merged
across files. See [`agent-pool.example.yaml`](../agent-pool.example.yaml).

| Key | Default | Rule |
|---|---|---|
| `mode` | none (required) | `stateless` or `stateful`. |
| `affinity` | `none` for stateless, `user` for stateful | If given, must match the mode. |
| `scheduler` | `first_available` | `first_available`, `round_robin`, `oldest_idle`, `newest_idle`. |
| `min_warm_sessions` | 0 | At least 0 and no more than `max_sessions`. |
| `max_sessions` | 10 | At least 1. Hard limit, including sessions being created. |
| `queue.enabled` | true | When false, a busy pool returns `POOL_CAPACITY_EXCEEDED`. |
| `queue.max_depth` | 500 | At least 1 when the queue is enabled. |
| `queue.max_wait_seconds` | 120 | Greater than 0. |
| `sync_interval_seconds` | 60 | At least 10. |
| `create_retries` | 3 | 0 to 10. |
| `telemetry.enabled` | true | Per-agent business metrics. Logs are not affected. |
| `protocol` | `responses` | `responses` or `invocations`. See below. |
| `circuit_breaker.enabled` | true | Per-agent circuit breaker. |
| `circuit_breaker.failure_threshold` | 5 | Consecutive Foundry failures that open the circuit (1 to 1000). |
| `circuit_breaker.open_seconds` | 30 | How long the circuit stays open before a probe is allowed. Greater than 0. |
| `circuit_breaker.half_open_max_calls` | 1 | Probe calls allowed at once while testing recovery (1 to 100). |
| `agent_version` | unset | Pin new sessions to this agent version. Unset means the latest version when the session is created. |
| `version_drain` | `never` | `never`, `unbound` or `idle`. Which idle sessions on a version other than the target the sync retires. See [operations](operations.md#rolling-out-a-new-agent-version). |
| `scheduler_profile.filters` | none | Filters added to the core ones (`Ready`, `AffinityCompatible`, `RestoreHeld`): `VersionCompatible`, `NotExpiring`, or one an application registered. |
| `scheduler_profile.scores` | the `scheduler` strategy, weight 1 | Score plugin names with weights (1 to 1000) that replace the strategy: `FirstAvailable`, `RoundRobin`, `OldestIdle`, `NewestIdle`, `AffinityPreference`, `VersionPreferred`, `ExpiryRisk`. An unknown name stops start-up. |
| `adopt_unbound_sessions` | `true` for stateless, `false` for stateful | Whether the reconciler registers Foundry sessions this process did not create. Keep it `false` for stateful agents, because a session may hold another user's files. |

Merge rules: `defaults` merge into each agent, nested maps merge key by key, and a key set to
`null` counts as unset. Agent names must match `[A-Za-z0-9][A-Za-z0-9_-]{0,127}`.

Rejected at start-up: unknown keys, duplicate YAML keys, an empty `agents` map, a mode and affinity
that disagree, `min_warm_sessions` above `max_sessions`, and invalid values. The error names the
path, for example `agents.coding-agent.max_sessions`.

## Entra ID setup

1. Register an application for the pooling API and set an Application ID URI such as
   `api://<client-id>`. Use that value for `POOL_ENTRA_AUDIENCE`.
2. Add app roles `Pool.Invoke`, `Pool.Admin` and `Pool.Diagnostics`, allowed for users and
   applications. Add `Pool.Delegate` for services that call on behalf of their users.
3. Assign users, groups or service principals to the roles in the enterprise application.
4. Clients request a token for the API. The service accepts v1 and v2 token issuers for the
   configured tenant and rejects tokens from any other tenant.

`oid` is the user's object id (or the service principal's object id for app-only tokens). It is the
affinity key, so it must be stable. It is.

## Protocols

Each hosted agent exposes the Responses protocol, the Invocations protocol, or both. Set
`protocol` to the one the pool should use for that agent.

| `protocol` | Endpoints | Request | Response |
|---|---|---|---|
| `responses` (default) | `chat`, `invoke` | A message, or a raw Responses payload in `input`. | A JSON envelope, or server-sent events with `stream: true`. |
| `invocations` | `invoke` only | `input` is sent to the agent as the request body, unchanged. | The agent's body, status and content type. Upstream headers are dropped and the Foundry session id is removed from JSON bodies. |

For an Invocations agent the `chat` endpoint returns 422. An Invocations agent decides for itself
whether to stream: a `text/event-stream` response is relayed as it arrives and the session lease is
held until the stream ends. A `stream` key in `input` is passed to the agent like any other field.

The pool sends the session id in the `agent_session_id` query parameter, as Foundry requires. It
never forwards upstream headers, and it removes `agent_session_id` from JSON responses. It cannot
scrub other formats, so an agent that echoes its session id in plain text would expose it.

## Circuit breaker

Each agent has its own breaker, in front of every Foundry call for that agent. Only `Unavailable`
(5xx, connection) and `Timeout` results count as failures. A 404, a rejected request or a 429 proves
Foundry is answering, so it does not count.

1. **Closed.** Calls pass. `failure_threshold` failures in a row open the circuit.
2. **Open.** Calls fail at once with `503 FOUNDRY_CIRCUIT_OPEN` and a `Retry-After` for the time
   left. Nothing is sent to Foundry.
3. **Half open.** After `open_seconds`, up to `half_open_max_calls` probe calls pass. One success
   closes the circuit. One failure opens it again for another `open_seconds`.

The breaker applies to the reconciler too, so a sync during an outage is `incomplete` and deletes
nothing. A failure after a stream has started also counts. The health endpoints do not depend on the
breaker. State is per process and is lost on restart.

## Restoring stateful sessions after a restart

Without `POOL_SESSION_ID_KEY`, a restart loses which session belongs to which user. Stateful agents
then ignore sessions they did not create (see `adopt_unbound_sessions`).

With the key set, the pool creates each stateful user's session under an id it derives with
HMAC-SHA256 from the key, the agent name and the user id. After a restart:

- The first sync finds sessions in that id format and holds each one for its owner.
- When a user returns, the pool derives their id and takes that session. A user can only ever derive
  their own id, so nobody can claim another user's session.
- A user who arrives before the first sync finishes is handled too: the pool looks the session up
  directly.

Rules and limits:

- The key must be the same on every start. If it changes, existing sessions can no longer be found.
  Treat it like a password and store it in Key Vault or an equivalent.
- Stateful agents cannot use `min_warm_sessions` when the key is set, because a warm session has no
  user yet and its id cannot be changed. The service refuses to start with that combination.
- If a user's derived session is failed, deleting, deleted or expired, the pool creates a
  replacement under a random id. That replacement is not found after a restart.
- Sessions held for owners count against `max_sessions`, as bound sessions always have. A pool full
  of sessions for users who never return blocks new users until Foundry expires those sessions.
- Stateless agents are unaffected.

## Isolation keys

Foundry scopes sessions with an isolation key. In the default Entra scheme the platform takes it
from the caller's token, so all sessions made by the pool's identity share one key. If an agent
endpoint uses the Header scheme, Foundry requires `x-ms-user-isolation-key` on every call. Set
`POOL_FOUNDRY_ISOLATION_KEY` to one constant value so the pool's calls are accepted and all its
sessions stay in one partition. The platform ignores the header in the Entra scheme.
