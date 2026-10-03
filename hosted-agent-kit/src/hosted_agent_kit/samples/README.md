# Samples

Runnable FastAPI examples for hosted-agent-kit. Install with `pip install "hosted-agent-kit[fastapi]" uvicorn`,
then run any sample:

```bash
uvicorn hosted_agent_kit.samples.response.basic_ask:app --reload
curl -s localhost:8000/ask -H 'X-User-Id: alice' -H 'content-type: application/json' -d '{"message": "refund?"}'
```

Without `FOUNDRY_PROJECT_ENDPOINT` set, samples run in demo mode against an in-memory agent. Set it
(and run `az login`) to use real hosted agents; deploy them first from `agents/`.

Copy the samples to edit them: `hack samples copy ./hack-samples` (set `HACK_SAMPLES_DIR` to the copy).

| Group | Sample | Shows |
|---|---|---|
| **response** | `basic_ask` | `kit.ask`, listing agents |
| | `streaming` | Server-sent events, consuming a stream |
| | `conversations` | A stateful agent, `conversation_key` |
| | `idempotent_requests` | `idempotency_key` and replays |
| | `request_options` | Full Responses bodies, timeouts |
| **invoke** | `json_invocation` | JSON to an Invocations agent |
| | `file_upload` | Any body and content type |
| | `passthrough` | A relay endpoint, `describe` |
| **reporting** | `pool_status` | `reporting_router`, status per agent |
| | `events_and_metrics` | Events, metrics, Prometheus |
| | `session_inspector` | Sessions, conditions, identity redaction |
| **administrative** | `admin_api` | `admin_router` behind a guard |
| | `capacity_control` | Warm sessions, sync, reload |
| | `session_cleanup` | Deleting sessions |
| | `version_rollout` | Version pinning and drain status |
| **miscellaneous** | `error_handling` | `HackError`, problem documents |
| | `custom_scheduler_plugin` | Your own filter and score |
| | `custom_lifespan` | `async with kit`, readiness |
| | `configuration_in_code` | `from_dict`, `KitSettings` |
| | `multi_agent_gateway` | Both protocols from one endpoint |
| | `testing_your_app` | `FakeFoundry` and `TestClient` |

Also here: `agents/` (three hosted agents, each with `agent.py`, `azure.yaml`, `Dockerfile`,
`scheduler.yaml`) and `config/` (every scheduler YAML option).

Every sample trusts an `X-User-Id` header and `X-Admin-Key` so you can use curl. A real application
takes the user from a validated token and uses real authorisation for administration.
