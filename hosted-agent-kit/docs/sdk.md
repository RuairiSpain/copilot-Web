# SDK guide

`hosted-agent-kit` is a library. You create one `Hack` object, start it with your application, and
call agents from your own endpoints. This guide describes the API. The `samples/` folder of the
package shows each call in a runnable FastAPI application (`hack samples copy ./dir`).

## Concepts

- **Agent**: a hosted agent in your Foundry project. Its name in the scheduler YAML is its Foundry
  name.
- **Session**: one running sandbox of an agent. A session serves one call at a time.
- **Pool**: the sessions of one agent, between zero and `max_sessions`.
- **User**: the `user_id` you pass. For a stateful agent, it selects the session. For a stateless
  agent it only scopes idempotency keys.
- **Conversation**: an optional `conversation_key` that gives one user several sessions with a
  stateful agent.

## Lifecycle

```python
kit = Hack.from_yaml("scheduler.yaml")      # reads and validates; nothing is started yet
await kit.start()                            # connects to Foundry, first sync, starts controllers
...
await kit.stop()                             # refuses new calls, waits for running ones, closes
```

`async with kit:` does the same. `install(app, kit)` (FastAPI) starts the kit before your own
startup code and stops it after your shutdown code. `start()` waits up to
`startup_sync_timeout_seconds` (30) for the first sync so a first call can reuse sessions that
already exist. `stop()` waits up to `shutdown_grace_seconds` (30) for running calls, including
streams. Calls made before `start()` raise `RuntimeError`. A call made while stopping raises
`ServiceDrainingError`.

Creating a `Hack`:

| Constructor | Use |
|---|---|
| `Hack.from_yaml(path)` | A scheduler file, or an `azure.yaml` that has an `agentPool` section. Supports `reload_config()`. |
| `Hack.from_dict(document)` | The same structure as the YAML, built in Python. |
| `Hack.from_yaml_text(text)` | YAML in a string. |
| `Hack(config, settings=...)` | An `AgentPoolConfig` you built yourself. |

All accept `adapter=` (a Foundry adapter, normally `FakeFoundry` in tests), `clock=` and
`scheduler_plugins=`.

## Calls

| Method | For | Returns |
|---|---|---|
| `responses(agent, *, user_id, input, stream=False, ...)` | Responses agents. `input` is the request body. | `AgentResult` |
| `ask(agent, message, *, user_id, ...)` | Responses agents, one text message. | `AgentResult` |
| `invocations(agent, *, user_id, body, content_type=None, ...)` | Invocations agents. `body` is `bytes`, `str`, a mapping (sent as JSON) or `None`. | `AgentResult` |
| `describe(agent)` / `list_agents()` | Discovery: protocol, statefulness, streaming, queue, limits. | `AgentInfo` |

Common keyword arguments: `conversation_key`, `timeout_seconds` (one upstream call, capped by
`max_timeout_seconds`), `idempotency_key`.

Calling a Responses agent with `invocations()`, or the reverse, raises `ValidationFailedError`.
`user_id` is 1 to 128 characters of letters, digits and `. _ : -`. Take it from a validated token in
production, such as the Entra `oid` claim. If your application is itself a service that acts for
its users, pass the end user's id, not the service's.

### `AgentResult`

| Member | Meaning |
|---|---|
| `status_code`, `media_type`, `content` | Exactly what the agent returned (Invocations), or the Responses JSON. |
| `json()`, `text()` | Parsed or decoded body. |
| `ok` | `200 <= status_code < 300`. |
| `replayed` | True when an idempotency key returned a stored answer. |
| `is_stream`, `chunks()` | For `stream=True`: an async iterator of server-sent-event bytes. |
| `aclose()`, `async with` | Release a stream early. Always safe, also on a normal result. |

The session returns to the pool when a normal call returns and when a stream ends or is closed.
`sse_response(result)` and `response_for(result)` do this for you inside FastAPI, even when the
caller disconnects. A failure after streaming started arrives as a final `event: error` frame with
`error_code`, `title`, `detail`, `correlation_id` and, where relevant, `retry_after_seconds`.

### Idempotency

With `idempotency_key`, the first call runs. The same key with the same request returns the stored
answer (`replayed=True`). The same key with a different request raises `IdempotencyKeyReusedError`
(422). A repeat while the first is running raises `IdempotencyInProgressError` (409). Keys are
scoped to user and agent. Only successful, non-streaming answers are stored, in memory, for
`idempotency_ttl_seconds` (600). `0` turns it off.

### Errors

Every error derives from `hosted_agent_kit.errors.HackError`.

| Attribute | Meaning |
|---|---|
| `status` | The HTTP status it maps to. |
| `code` | A stable string, such as `QUEUE_FULL`. |
| `phase` | `request`, `auth`, `queue`, `create`, `invoke` or `stream`. |
| `retry_safe` | True when a retry cannot repeat work the agent already did. |
| `retry_after_seconds` | For capacity and throttling errors. |

Common errors: `AgentNotConfiguredError` (404), `QueueFullError` and
`PoolCapacityExceededError` (429), `UpstreamThrottledError` (429), `QueueWaitTimeoutError`,
`StickySessionTimeoutError` and `RequestTimeoutError` (504), `ServiceDrainingError` (503). The full
list with causes is in `errors.md`. `install(app, kit)` returns them as `application/problem+json`.

## Reporting (read-only)

`kit.reporting` never changes anything.

| Method | Returns |
|---|---|
| `agents()` / `agent_summary(name)` | Capacity, queue depth, circuit state, `generation`, `observed_generation`, conditions. |
| `pool(name)` | The pool resource: spec, status, conditions, latest events. |
| `sessions(name, *, reveal_identities=False)` / `session(name, id)` | Sessions. Users and conversations are hashed unless revealed. |
| `events(name=None, *, limit=100)` | What the controllers did and why, newest first. |
| `metrics()` / `prometheus()` | JSON snapshot or Prometheus text. |
| `health()` | `ready` or `not_ready` with the checks. No call to Foundry. |

## Administration

`kit.admin` changes the running pool. Always protect it.

| Method | Effect |
|---|---|
| `delete_session(agent, id)` | Marks the session for deletion. A finalizer holds the record until Foundry confirms. Returns False when the session is leased and deletion waits for the lease to end. |
| `sync(agent)` | Compares Foundry with the pool now and returns a report. |
| `provision_warm(agent)` | Creates one ready session. False when the pool is full. |
| `reload_config()` | Re-reads the YAML. Sizes, queues, profiles, versions and strategies apply at once. Adding or removing agents needs a restart. |

## FastAPI integration

`hosted_agent_kit.integrations.fastapi`:

| Name | Purpose |
|---|---|
| `install(app, kit, error_handlers=True)` | Lifecycle, problem+json error handlers, `app.state.hack`. |
| `KitDep`, `get_kit` | Dependency that returns the kit. |
| `sse_response(result)` | Relay a stream as server-sent events and always release the session. |
| `response_for(result)` | Relay any result with the agent's status code and content type. |
| `reporting_router(reveal_identities=False)` | Read-only endpoints. |
| `admin_router()` | Delete a session, sync, warm, reload. |
| `install_error_handlers(app)` | Only the error handlers, if you manage the lifecycle yourself. |

## Scheduling plugins

A filter decides whether a session may serve a request. A score ranks the sessions that pass.
Register both in a `PluginRegistry` and name them in an agent's `scheduler_profile`:

```python
from hosted_agent_kit.plugins import default_plugins

plugins = default_plugins()
plugins.register_filter("YoungEnough", YoungEnough)     # has .name and .filter(request, session, state)
plugins.register_score("PreferOlder", PreferOlder)      # has .name and .score(request, session, state)
kit = Hack.from_yaml("scheduler.yaml", scheduler_plugins=plugins)
```

Built-in filters: `NotExpiring`. Built-in scores: `FirstAvailable`, `RoundRobin`, `OldestIdle`,
`NewestIdle`, `AffinityPreference`, `VersionPreferred`, `ExpiryRisk`. Core filters (readiness,
affinity, version compatibility) always run.

## Settings

`KitSettings` reads `POOL_*` environment variables. `Hack.from_yaml` also reads the optional `hack:`
section of the file for the non-secret settings; environment variables win. See
`configuration.md` for the table.

## Deploying

- Run **one replica** of the application. Pool state is in memory.
- Give the application an identity that can use the Foundry project (managed identity with a role
  on the project). Credentials come from `DefaultAzureCredential`. Nothing secret goes in YAML.
- Set `FOUNDRY_PROJECT_ENDPOINT`. Set `POOL_SESSION_ID_KEY` (32+ characters, stable) so stateful
  users find their sessions again after a restart.
- Set your orchestrator's termination grace period above `shutdown_grace_seconds`.
- Deploy each hosted agent with `azd` (see `samples/agents/*/azure.yaml`). The agent name in
  Foundry must equal the agent name in the scheduler YAML.

## Testing

`hosted_agent_kit.testing`: `FakeFoundry` (records every call; script failures with
`create_errors`, `invoke_errors`, `invoke_handler`, `invoke_gate`), `DemoFoundry` (answers like a
small agent), `FakeClock`. See `samples/miscellaneous/testing_your_app.py`.
