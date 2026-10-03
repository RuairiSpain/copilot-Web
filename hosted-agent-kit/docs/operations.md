# Operations runbook

Admin calls need a token with `Pool.Admin`. Examples use `$POOL` for the service URL and `$TOKEN`
for the bearer token.

## Health

| Probe | Meaning |
|---|---|
| `GET /health/live` | The process is up. Restart if this fails. |
| `GET /health/ready` | Configuration loaded, identity ready, reconcile workers running, and a first sync attempted for every agent. Foundry is probed only if `POOL_READINESS_PROBE_FOUNDRY=true`. An open circuit does not make the service unready. |

Keep the Foundry probe off by default. A Foundry outage would otherwise take the whole service out
of rotation, and the pool can already report that outage as `FOUNDRY_UNAVAILABLE`.

## Look at the pool

```bash
curl -s -H "Authorization: Bearer $TOKEN" $POOL/v1/admin/agents                     # counts, queue depth
curl -s -H "Authorization: Bearer $TOKEN" $POOL/v1/admin/agents/coding-agent/sessions
```

User ids are redacted as `u_<hash>`. The diagnostics role sees raw ids.

### Conditions and events

A pool and each session report *conditions*, which say what is true and why, like Kubernetes
resources do. Read them before the counters:

```bash
curl -s -H "Authorization: Bearer $TOKEN" $POOL/v1/admin/agents/coding-agent     # spec, status, conditions, events
curl -s -H "Authorization: Bearer $TOKEN" "$POOL/v1/admin/events?agent_name=coding-agent&limit=50"
```

| Pool condition | `False` or `Unknown` means |
|---|---|
| `Ready` | The first reason below that applies: `ConfigurationInvalid`, `AwaitingSync`, `FoundryUnreachable`. |
| `InitialSyncComplete` | Foundry has not yet been listed completely. |
| `FoundryReachable` | The circuit is open (`CircuitOpen`) or listings are failing (`ListingFailed`). |
| `CircuitOpen` | `True` while the circuit breaker is open. |
| `CapacityAvailable` | `False` (`AtCapacity`): every session is in use or being created. |
| `ConfigurationValid` | `False` (`ReloadRejected`): the last reload was refused. The old settings still apply. |
| `RecoveryBlocked` | `True`: stateful users' sessions cannot be found again until Foundry has been listed. |

`generation` is the version of the pool's settings and `observed_generation` the one the controllers
have applied. A gap that lasts means a controller is not running. A session shows `finalizers` and a
`deletion_timestamp` while it waits to be deleted, and a `DeletionFailed` condition if Foundry
refused. Events record what the controllers did (`Created`, `Adopted`, `Ignored`, `Missing`,
`DeleteFailed`, `VersionDrain`, `ReservationsExpired`, `ConditionChanged`). They are kept briefly and
are not state: nothing reads them.

### Reload the configuration

Edit the agent pool document and call `POST /v1/admin/config/reload`. It validates the whole
document first. A bad one changes nothing and is reported as `ConfigurationValid=False`. A good one
is applied without a restart, and the response lists the agents that changed with their new
generation. A reload cannot add or remove agents, and circuit breaker settings are read at start-up:
both need a restart.

## Metrics and suggested alerts

All series carry an `agent` label. User ids and session ids are never labels.

| Metric | Alert idea |
|---|---|
| `pool_queue_depth` | Sustained above 50 percent of `queue.max_depth`. |
| `pool_queue_wait_seconds` | p95 above half of `queue.max_wait_seconds`. |
| `pool_requests_total{outcome}` | Rising `QUEUE_FULL`, `QUEUE_WAIT_TIMEOUT` or `FOUNDRY_*`. |
| `pool_sessions{local_state}` | `available` at 0 for a long period while `leased` equals `max_sessions`. |
| `pool_failed_sessions_total` | Any sustained increase. |
| `pool_session_delete_total{outcome="failure"}` | Any increase. |
| `pool_reconcile_total{outcome}` | Repeated `incomplete` or `error`. |
| `pool_circuit_state` | 2 (open) for more than a minute. Values: 0 closed, 1 half open, 2 open. |
| `pool_circuit_transitions_total{to}` | Repeated `open` transitions (flapping). |
| `pool_sessions_restored_total` | Informational. Counts sessions taken back after a restart. |

Prometheus text is at `/metrics` when `POOL_METRICS_ENDPOINT_ENABLED=true` (admin role required).
With `APPLICATIONINSIGHTS_CONNECTION_STRING` set, metrics and logs also go to Azure Monitor.

## Runbooks

### Queue saturation (429 `QUEUE_FULL`, 504 `QUEUE_WAIT_TIMEOUT`)

1. Check `GET /v1/admin/agents`. Compare `sessions_leased` with `max_sessions`.
2. If every session is leased, requests are slow or stuck. Check Foundry latency and the agent's own
   logs.
3. If capacity is the limit, raise `max_sessions` and redeploy, within your Foundry quota.
4. For stateful agents, one slow user only blocks that user's own session. Look for a single hot
   user in the logs by `correlation_id`.

### Foundry outage (503 `FOUNDRY_UNAVAILABLE`, 504 `FOUNDRY_TIMEOUT`)

1. Confirm in Azure Service Health and the Foundry portal.
2. Expect `pool_reconcile_total{outcome="incomplete"}`. The pool marks unseen sessions unavailable
   and does not delete anything while the listing is incomplete.
3. No action is needed after recovery. The next sync restores sessions.

### Circuit open (503 `FOUNDRY_CIRCUIT_OPEN`)

The agent's breaker opened after repeated Foundry failures. `GET /v1/admin/agents` shows
`circuit_state` per agent, and the log has `circuit_open`, `circuit_half_open` and `circuit_closed`
events.

1. Treat it as a Foundry outage (see above). The breaker protects Foundry and your callers from
   retry storms.
2. After `open_seconds` one probe call tests recovery. A success closes the circuit.
3. Flapping means Foundry is partly failing. Raise `failure_threshold` or `open_seconds` only after
   checking the cause.
4. The breaker cannot be reset by hand. A restart resets it, but only do that if you are sure the
   cause has gone.

### Throttling (429 `UPSTREAM_THROTTLED`)

Foundry is rate limiting. The service already honours `Retry-After`. Lower the call rate, reduce
warm-session churn, or request more quota.

### Failed sessions

Failed sessions are deleted automatically by the sync, and counted in `pool_failed_sessions_total`.
A `session_failed` log event has the hashed session id. If deletes fail, the record stays
`retiring` and is retried each sync. To force a retry, call
`POST /v1/admin/agents/{agent}/sync`.

### Drift between the pool and Foundry

Run a manual sync. The response lists `discovered`, `removed`, `failed_sessions` and `uncertain`.
A 409 means a sync is already running. Delete a stuck session with
`DELETE /v1/admin/agents/{agent}/sessions/{id}`. It returns 204, or 202 if the session is in use and
deletion is deferred until the lease ends.

### Restart or redeploy

Restarting interrupts in-flight requests. After a restart:

- Stateless agents adopt the existing sessions and carry on.
- Stateful agents with `POOL_SESSION_ID_KEY` set find each user's session again. The first sync
  holds each session for its owner, who takes it back on the next request. Watch
  `pool_sessions_restored_total`. In the admin session list, `restorable: true` marks a session held
  for a user who has not returned yet.
- Stateful agents without the key lose the user-to-session mapping. By default the service
  **ignores** sessions it did not create, so no user can inherit another user's files. Users get new
  sessions, and the ignored sessions stay in Foundry until they expire (30 days) or someone deletes
  them. They do not count against `max_sessions`.
- Set `adopt_unbound_sessions: true` on a stateful agent only if its sessions hold no user data.
- Never change `POOL_SESSION_ID_KEY` on a running system. Sessions created under the old key can
  no longer be matched to users.

On shutdown the service marks itself not ready, refuses new requests with
`503 SERVICE_SHUTTING_DOWN`, and waits up to `POOL_SHUTDOWN_GRACE_SECONDS` (30) for running requests
and streams to finish before it stops the controllers. Give the platform a termination grace period
at least that long.

Schedule restarts in quiet periods. Do not run two replicas to get zero downtime. See ADR 0003.

### Rolling out a new agent version

New sessions use the latest agent version, so after a deployment the pool holds sessions on both.
To move idle sessions over:

1. Optionally pin the version with `agent_version`, so every new session is created on it.
2. Set `version_drain: unbound` to retire idle pooled sessions on any other version. Warm sessions
   are re-created on the target. Use `idle` only when losing user conversation state is acceptable.
3. Run `POST /v1/admin/agents/{agent}/sync`, or wait for the next one. The report's `drained` is the
   number retired. A `VersionDrain` event names each session.
4. Leased sessions finish on the version they started with and are drained by a later sync.

Set `version_drain` back to `never` once the pool is on the new version if you do not want
automatic draining on the next deployment.

### Rollback

Deploy the previous image tag, with `az containerapp update --image <previous-tag>` or `azd deploy`
from the previous commit. Behaviour after a rollback is the same as after a restart.

### Authentication failures after a key rotation

The service refreshes signing keys when it sees an unknown key id, at most once every 30 seconds.
Tokens signed with a new key start working within that window.

## Logs

JSON, one event per line, with `event`, `timestamp`, `level`, `correlation_id` and `request_id`.
Key events: `service_started`, `session_created`, `session_failed`, `session_delete`,
`session_ignored`, `session_restored`, `session_id_conflict`, `circuit_open`, `circuit_half_open`,
`circuit_closed`, `reconcile_completed`, `reconcile_error`, `client_disconnected`,
`readiness_probe_failed`, `development_auth_enabled`. Session ids appear only as `session_id_hash`.
Tokens, prompts and responses are never logged.

## Deployment checklist

- One replica (the Bicep template enforces `minReplicas = maxReplicas = 1`).
- The managed identity has a Foundry role that allows hosted agent session create, list and delete.
  Confirm this in your tenant before go-live.
- Entra app roles exist and callers are assigned.
- `POOL_AUTH_MODE` is `entra`.
- Agent names in the pool configuration match hosted agents in the project.
- Alerts are in place for the metrics above.
- If you want sessions restored after a restart, `POOL_SESSION_ID_KEY` is set from a secret store.
- Agents that use the Header authorisation scheme have `POOL_FOUNDRY_ISOLATION_KEY` set.
