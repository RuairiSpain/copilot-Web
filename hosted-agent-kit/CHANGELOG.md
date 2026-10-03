# Changelog

All notable changes are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.2.0] - 2026-10-03

### Added
- Quota: `SessionQuotaError` and `RegionalCapacityError` for Foundry quota 429s; an active-session
  limit (`max_active_sessions`) separate from `max_sessions`; stopping idle sessions to make room;
  kit budget (`quota.budget`) with an adaptive limit; `hack plan`; `/v1/admin/quota`; metrics.
- Optional Redis extra (`redis`): regional ledger and ownership leases, with Microsoft Entra auth.
- Several kits per project: `kit_id`, `owns`, `shard`, active/passive standby, `WrongShardError`.
- Per-user isolation headers (`user_isolation: off|key|delegated`).
- SDK: `HackSync`, `kit.lifespan`, `result.output_text`, `events()`, `text_deltas()`, SSE parser,
  OpenTelemetry spans and trace context, `EntraAuth`, error groups, `max_request_bytes`.
- Queue fairness (`queue.per_user_depth`, `queue.fairness: round_robin`).
- `scripts/live_canary.py` and a scheduled canary workflow; a 2,000-session soak test.
- Samples and YAML for the above; docs `quota.md`, ADRs 0011 and 0012.

### Fixed (found in review)
- The kit's CI, canary and publish workflows are now in the repository's `.github/workflows/`
  (as `hack-*.yml`); GitHub did not run them from the subfolder.
- Self-fencing now happens before the lease can expire, and leases are renewed during start-up.
- The quota count can no longer be partial while it is rebuilt.
- Borrowed permits that lapse in the ledger are dropped by the kit.
- `ownership.entra_auth` and `ownership.timeout_seconds`; a standby kit starts without the store.
- `ownership.renew_seconds` must now be at most a third of `ownership.ttl_seconds`.

### Changed
- Identifier hashing in logs is keyed (HMAC).
- `reporting_router` and `admin_router` refuse to build without `dependencies=` or
  `allow_unauthenticated=True`.
- Samples accept header sign-in only in demo mode or with `HACK_SAMPLE_INSECURE_AUTH=1`.
- The registry gives read-only views and counts incrementally, so 2,000 sessions stay fast.
- The reconciler jitters its interval by 10%.
- A start-up that gets 401 or 403 from Foundry fails instead of retrying silently.

## [0.1.0] - 2026-10-03

First release of the package as `hosted-agent-kit`, an SDK. Earlier work was developed as
`foundry-agent-pool` 1.x and is listed under "Before 0.1.0" below.

### Added
- `Hack`: embed the scheduler in a Python application. `responses`, `ask`, `invocations`,
  `describe`, `list_agents`, with streaming, conversation keys, timeouts and idempotency keys.
- `AgentResult`, `hosted_agent_kit.errors`, typed `reporting` and `admin` interfaces.
- `hosted_agent_kit.integrations.fastapi`: `install`, `KitDep`, `sse_response`, `response_for`,
  `reporting_router`, `admin_router`, problem+json error handlers.
- `hosted_agent_kit.testing`: `FakeFoundry`, `DemoFoundry`, `FakeClock`.
- `hosted_agent_kit.plugins`: write scheduler filters and scores.
- An optional `hack:` section in the scheduler YAML for non-secret runtime settings.
- `hack` command: `validate`, `samples list|copy`, `serve`.
- Samples: 21 FastAPI examples (response, invoke, reporting, administrative, miscellaneous), three
  hosted agents with `agent.py`, `azure.yaml`, `Dockerfile` and `scheduler.yaml`, and 14 YAML files
  that show every configuration option.
- Packaging for PyPI: hatchling build, extras (`fastapi`, `service`, `azure-monitor`, `all`),
  `py.typed`, distribution check script, CI package job, trusted-publishing workflow.

### Changed
- Renamed from `foundry-agent-pool` to `hosted-agent-kit`; the import package is
  `hosted_agent_kit`. Version restarts at 0.1.0.
- The standalone HTTP service moved to `hosted_agent_kit.service` and is an optional extra. Its
  console command is `hack-service` (also `hack serve`). Error `type` URNs and the
  `WWW-Authenticate` realm use the new name.
- `Settings` is split into `KitSettings` (runtime) and `Settings` (service).
- Python 3.12 or later (was 3.13). `azure-ai-projects` is `~=2.7.0` instead of an exact pin.
- Wiring shared by the SDK and the service is in `hosted_agent_kit.runtime`.

## Before 0.1.0 (developed as foundry-agent-pool)

### Architecture
- The service is structured as a request plane, a state plane and a control plane, following
  Kubernetes control-plane patterns in one process (ADR 0009).
- `AgentPool` and `AgentSession` are resources with `generation`, `resource_version`, finalizers and
  conditions. The session key is `(agent name, session id)`.
- Remote deletion is finalizer-driven. A session is recorded the moment Foundry creates it, and a
  failed or cancelled creation marks it for deletion instead of deleting it inline.
- Controllers (session, observation, pool, garbage collection, health, status) run under a
  `ControllerManager` with keyed, deduplicating, rate-limited work queues. `Reconciler` orchestrates
  them and keeps its API.
- A scheduling framework (PreFilter, Filter, Score, Reserve, Permit, Bind, Unreserve) with built-in
  plugins, a profile per agent (`scheduler_profile`) and a registry for application plugins.
- The session store versions every record, announces changes, and updates with compare-and-set.
- Configuration can be reloaded without a restart. `generation` and `observed_generation` show
  whether a change has been applied.
- Graceful shutdown: not ready first, new requests refused, running requests and streams awaited.

### Added (control plane)
- Admin API: pool conditions, `generation` and `observed_generation`, `GET /v1/admin/agents/{agent}`,
  `GET /v1/admin/events`, `POST /v1/admin/config/reload`, and conditions, finalizers and deletion
  fields on sessions.
- Settings `POOL_SHUTDOWN_GRACE_SECONDS` and `POOL_RESERVATION_TTL_SECONDS`.
- Errors `SERVICE_SHUTTING_DOWN`, `CONFIG_INVALID` and `RELOAD_UNAVAILABLE`.
- Garbage collection of sessions stuck provisioning, stale affinity entries and unused capacity
  reservations.

### Fixed
- Registry and affinity lookups are keyed by agent and session id. A session id is unique only
  within one agent, so two agents can no longer replace each other's records.
- A session that was created but never became ready (timeout, cancellation or a polling failure) is
  deleted. If the delete fails it is kept as `retiring` and the reconciler retries it.
- A failed delete of a failed session keeps a `retiring` record instead of forgetting the session.
- A failing close of the upstream stream no longer strands the lease or replaces the original error.
- A streaming half-open probe settles the circuit from how the stream ends, not when it opens.
  A probe stream that is closed early frees its probe slot.
- A leased session that has disappeared from Foundry is marked `retiring` and is never offered again.
- An error before the first stream frame is a normal error response instead of a 200 with an
  error event. Error events now carry `error_code`, `title`, `detail`, `correlation_id` and, where
  relevant, `retry_after_seconds`.
- A Foundry `Retry-After` is a minimum delay, accepts HTTP dates, and is returned to the caller when
  it exceeds `POOL_BACKOFF_MAX_SECONDS` instead of being cut short.
- Non-streaming Invocations responses are limited by `POOL_MAX_RESPONSE_BYTES`.
- A client that stops reading a stream can no longer hold the lease past `POOL_MAX_STREAM_SECONDS`.
- The request duration metric is end to end for every outcome.
- Concurrent requests share one JWKS download.
- A failed start-up closes the Foundry and JWKS clients.
- `POOL_FOUNDRY_ISOLATION_KEY` is a secure Bicep parameter and a Container App secret.
- The OpenAPI document describes the JSON, event-stream and raw responses of `invoke` and `chat`.
- Documentation corrected: `Retry-After` on queue, sticky and unavailable errors, "retry" counts,
  FIFO among eligible requests, Invocations responses, and `pytest --cov`.

### Added
- Protocol endpoints `POST /v1/agents/{agent}/responses` and `POST /v1/agents/{agent}/invocations`.
  `invocations` accepts any request body and content type and can wrap the response in an envelope
  (`?envelope=true`). `invoke` is deprecated.
- `GET /v1/agents` and `GET /v1/agents/{agent}` describe protocol, streaming, endpoints and limits.
- Conversation keys (`X-Conversation-Key` or `conversation_key`): several sessions per user and agent.
- App-only callers on stateful agents must name the end user in `X-Pool-Subject` and hold
  `Pool.Delegate` (`POOL_APP_ONLY_POLICY`, `POOL_DELEGATE_ROLE`).
- `Idempotency-Key` now deduplicates non-streaming requests (`POOL_IDEMPOTENCY_*`), with
  `Idempotent-Replayed`, `IDEMPOTENCY_KEY_REUSED` and `IDEMPOTENCY_IN_PROGRESS`.
- `phase`, `retry_safe` and `request_id` on every error, in problem responses and stream events.
- Per-agent `agent_version` and `version_drain`; the sync report lists `drained`, and the admin views
  show the pin, the policy and each session's conversation.
- Python (`clients/python`) and TypeScript (`clients/typescript`) client libraries.
- ADR 0008.
- `POOL_MAX_REQUEST_SECONDS` (optional) for one ceiling over queue, creation, backoff and attempts,
  with `504 REQUEST_TIMEOUT`.
- `POOL_MAX_RESPONSE_BYTES` and `502 RESPONSE_TOO_LARGE`.
- The sync report lists `errors`.

### Changed
- **Breaking:** an app-only token (no `scp` claim) is refused on stateful agents unless it names the
  end user. Set `POOL_APP_ONLY_POLICY=allow` to keep the old behaviour.
- `GET /v1/admin/sessions/{id}` is now `GET /v1/admin/agents/{agent}/sessions/{id}`.

## [1.1.0] - 2026-10-03

### Added
- Invocations protocol. Set `protocol: invocations` on an agent. `invoke` sends `input` as the
  request body and returns the agent's response unchanged, including server-sent events.
- Restart recovery for stateful agents. With `POOL_SESSION_ID_KEY` set, sessions are created under
  an id derived from the user and agent, and found again after a restart.
- Per-agent circuit breaker (`circuit_breaker.*`). An open circuit returns
  `503 FOUNDRY_CIRCUIT_OPEN` with `Retry-After`.
- `POOL_FOUNDRY_ISOLATION_KEY` for agents that use the Header authorisation scheme.
- Metrics `pool_circuit_state`, `pool_circuit_transitions_total` and
  `pool_sessions_restored_total`. The admin agent view has `circuit_state` and the admin session
  view has `restorable`.
- Readiness check `initial_sync`.
- ADRs 0006 and 0007.

### Changed
- `/health/ready` is not ready until every agent has had a first sync attempt.
- An unexpected 409 from Foundry is reported as `UPSTREAM_ERROR`.
- With `POOL_SESSION_ID_KEY` set, the service refuses to start if a stateful agent has
  `min_warm_sessions` above zero.
- ADR 0002 and 0003 corrected: Foundry scopes sessions by an isolation key taken from the caller's
  token, so one service identity cannot tell its users apart.

### Known limitations
- A key change orphans existing stateful sessions.
- Circuit breaker state is per process and cannot be reset by hand.
- Foundry's answer to a duplicate session id, and whether a deleted id can be reused, are not
  verified. The pool falls back to a random id.

## [1.0.0] - 2026-10-02

First release (PRD version V1).

### Added
- Session pooling for Foundry hosted agents behind a REST API (`invoke`, `chat`, streaming).
- Stateless and stateful modes. Stateful agents keep one session per (user, agent).
- Exclusive session leases, bounded per-agent FIFO queues and hard per-agent capacity limits.
- Scheduler strategies: `first_available`, `round_robin`, `oldest_idle`, `newest_idle`.
- Reconciliation against Foundry at start-up, on an interval and on demand.
- Warm session maintenance (`min_warm_sessions`).
- Microsoft Entra ID bearer-token validation. The user identity is the `oid` claim.
- Admin API with role checks and redacted user identifiers.
- Structured JSON logs, correlation ids, OpenTelemetry metrics, optional Prometheus text endpoint
  and optional Azure Monitor export.
- RFC 7807 `application/problem+json` errors with stable error codes.
- Azure Container Apps deployment (Bicep and `azd`), Dockerfile, Docker Compose and CI workflow.
- Test suite with a 90 percent statement and branch coverage gate, SDK contract tests, an OpenAPI
  snapshot, load tests and a mutation check script.

### Known limitations
- State is in memory. Run exactly one replica. See `docs/adr/0003-single-replica-in-memory-state.md`.
- After a restart stateless agents re-adopt existing sessions. User affinity was not recoverable in
  1.0.0, so stateful agents ignore unknown sessions by default (`adopt_unbound_sessions`). 1.1.0
  adds recovery with `POOL_SESSION_ID_KEY`.
- Only agents that use the Responses protocol were supported in 1.0.0. 1.1.0 adds Invocations.
- 1.0.0 had no circuit breaker. 1.1.0 adds one.
- The optional `estimated_compute_seconds` metric is not implemented.
