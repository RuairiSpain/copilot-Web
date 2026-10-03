# hosted-agent-kit

The Hosted Agent Controller Kit (HACK) is a Python SDK that schedules calls to Microsoft Foundry
hosted agents. You import it into your FastAPI application and write your own endpoints. The kit
decides which Foundry session serves each call, so your code never handles session ids.

- **Stateless agents**: any free session may serve any call.
- **Stateful agents**: one session per user (and per conversation). The same user returns to the
  same session.
- **Safe concurrency**: a session serves one call at a time. Capacity is a hard limit. When the
  pool is full, calls wait in a bounded queue or fail fast with a retry hint.
- **Self-healing**: a controller compares the pool with Foundry, replaces sessions that failed or
  were deleted, and keeps warm sessions ready.
- **Both hosted-agent protocols**: Responses (JSON, streaming) and Invocations (any body, any
  content type).

State is held in memory in your process. Run one replica of the application that embeds the kit.

## Install

```bash
pip install "hosted-agent-kit[fastapi]"
```

| Extra | Adds |
|---|---|
| (none) | The scheduler, the Foundry adapter, the CLI. No web framework. |
| `fastapi` | `hosted_agent_kit.integrations.fastapi` and the samples. |
| `service` | The standalone HTTP service (`hack serve`): FastAPI, uvicorn, token validation. |
| `azure-monitor` | Export metrics to Application Insights. |

Python 3.12 or later.

## A complete application

`scheduler.yaml` says how to schedule each agent. The names are the names of your hosted agents in
Foundry.

```yaml
agentPool:
  agents:
    support-bot:
      mode: stateless
      min_warm_sessions: 2
      max_sessions: 8
    memory-bot:
      mode: stateful        # one session per user
      max_sessions: 20
```

`app.py` is an ordinary FastAPI application:

```python
from fastapi import FastAPI
from pydantic import BaseModel

from hosted_agent_kit import Hack
from hosted_agent_kit.integrations.fastapi import KitDep, install

kit = Hack.from_yaml("scheduler.yaml")
app = FastAPI()
install(app, kit)  # starts and stops the kit with the app; maps kit errors to HTTP responses


class Question(BaseModel):
    message: str


@app.post("/ask")
async def ask(question: Question, kit: KitDep):
    user = "alice"  # take this from your authentication
    result = await kit.ask("support-bot", question.message, user_id=user)
    return result.json()
```

```bash
export FOUNDRY_PROJECT_ENDPOINT="https://<account>.services.ai.azure.com/api/projects/<project>"
az login                       # or any credential that DefaultAzureCredential finds
uvicorn app:app
```

## Calling agents

```python
# Responses agent
result = await kit.responses("support-bot", user_id=user, input={"input": "Hello"})
result.json()

# Streaming: relay server-sent events to your caller
result = await kit.responses("support-bot", user_id=user, input={"input": "Hello"}, stream=True)
return sse_response(result)            # from hosted_agent_kit.integrations.fastapi

# Several conversations per user with a stateful agent
await kit.ask("memory-bot", "remember I like trains", user_id=user, conversation_key="trip")

# Invocations agent: any body, any content type
result = await kit.invocations("doc-processor", user_id=user, body=pdf_bytes,
                               content_type="application/pdf")
return response_for(result)            # the agent's own status code, type and body

# Retried requests run once
await kit.ask("support-bot", "Summarise order 42", user_id=user, idempotency_key="order-42")
```

Every call accepts `timeout_seconds`. Errors derive from `hosted_agent_kit.errors.HackError` and
carry `status`, `code`, `phase`, `retry_safe` and `retry_after_seconds`.

## Reporting and administration

```python
await kit.reporting.agents()                      # capacity, queue depth, circuit state, conditions
await kit.reporting.sessions("memory-bot")        # users and conversations are hashed by default
kit.reporting.events("support-bot", limit=20)     # what the controllers did and why
kit.reporting.metrics(); kit.reporting.prometheus(); kit.reporting.health()

await kit.admin.provision_warm("support-bot")     # add a ready session
await kit.admin.sync("support-bot")               # compare with Foundry now
await kit.admin.delete_session("memory-bot", session_id)
kit.admin.reload_config()                         # re-read scheduler.yaml
```

`reporting_router()` and `admin_router()` expose the same calls as FastAPI routers. Protect them
with your own dependency: `app.include_router(admin_router(), prefix="/ops", dependencies=[...])`.

## Samples

The package ships runnable FastAPI samples, three hosted agents (each with `agent.py`,
`azure.yaml`, `Dockerfile` and its `scheduler.yaml`) and a folder of YAML that shows every
configuration option:

```bash
hack samples list
hack samples copy ./hack-samples
uvicorn hosted_agent_kit.samples.response.basic_ask:app --reload    # demo mode: no Azure needed
hack validate ./hack-samples/config/*.yaml
```

| Group | Samples |
|---|---|
| `response` | `basic_ask`, `streaming`, `conversations`, `idempotent_requests`, `request_options` |
| `invoke` | `json_invocation`, `file_upload`, `passthrough` |
| `reporting` | `pool_status`, `events_and_metrics`, `session_inspector` |
| `administrative` | `admin_api`, `capacity_control`, `session_cleanup`, `version_rollout` |
| `miscellaneous` | `error_handling`, `custom_scheduler_plugin`, `custom_lifespan`, `configuration_in_code`, `multi_agent_gateway`, `testing_your_app` |
| `agents/` | `support-bot` (Responses, stateless), `memory-bot` (Responses, stateful), `doc-processor` (Invocations) |
| `config/` | `minimal`, `kitchen-sink` (every key), strategies, profiles, queues, circuit breaker, warm pool, version pinning, telemetry, `hack:` settings, `azure.yaml` embedding |

Without `FOUNDRY_PROJECT_ENDPOINT`, samples run against an in-memory agent. They read the user from
an `X-User-Id` header so you can try them with curl. Do not do that in production.

## Configuration

Two layers:

1. **Scheduler YAML** (`agentPool:`): per-agent mode, queue, warm sessions, circuit breaker, version
   pinning, scheduler strategy or weighted profile. A standalone file, or the `agentPool` section of
   your application's `azure.yaml`. Unknown and duplicate keys are errors. `defaults:` applies to
   every agent.
2. **Runtime settings** (`POOL_*` environment variables, or an optional `hack:` section of the same
   YAML for the non-secret ones): timeouts, limits, retries, idempotency. Environment variables win.
   Secrets (`POOL_SESSION_ID_KEY`, `POOL_FOUNDRY_ISOLATION_KEY`) come only from the environment.

`Hack.from_dict({...})` and `Hack(config, settings=KitSettings(...))` configure it in code.

## Testing your application

```python
from hosted_agent_kit import Hack
from hosted_agent_kit.testing import FakeFoundry, UpstreamResponse

foundry = FakeFoundry()
foundry.invoke_handler = lambda ctx: UpstreamResponse(body={"output_text": "42"})
kit = Hack.from_yaml("scheduler.yaml", adapter=foundry)   # then use FastAPI's TestClient
```

`FakeFoundry` records calls and can be scripted to fail or stall. `DemoFoundry` answers like a small
agent. `FakeClock` removes real waiting from backoff and timeouts.

## Standalone service

The same scheduler also runs as its own HTTP service with Microsoft Entra authentication
(`pip install "hosted-agent-kit[service]"` then `hack serve`). Use it when callers are not Python
applications. See `docs/service.md` in the source distribution. Python and TypeScript clients for it
are in `clients/`.

## Status and limits

- Beta (0.x): the API can change between minor versions.
- One process: pool state is in memory. Run one replica.
- Tested against the real Azure SDK clients over a fake HTTP layer. It has not been run against a
  live Foundry project from the build environment. Hosted agents and the `azd ai agent` extension
  are in preview: check the `azure.yaml` keys in the samples against the current Microsoft
  Foundry `azure.yaml` reference before you deploy.

## Licence

MIT.
