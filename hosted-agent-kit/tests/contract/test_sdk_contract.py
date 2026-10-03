"""Contract tests: the adapter driving the real azure-ai-projects and openai clients.

Only the HTTP layer is faked. These tests pin the wire shapes the adapter depends on, so an
SDK upgrade that changes them fails here rather than in production.
"""

from __future__ import annotations

import json
from typing import Any

import pytest

from hosted_agent_kit.adapters.foundry_sdk import SdkFoundryAdapter
from hosted_agent_kit.domain.enums import FoundrySessionStatus
from hosted_agent_kit.domain.errors import (
    FoundryRejected,
    FoundrySessionFailed,
    FoundrySessionNotFound,
    FoundryThrottled,
    FoundryUnavailable,
)
from hosted_agent_kit.domain.models import InvokeContext
from tests.contract.sdk_backend import SESSION_JSON, Backend, build_adapter

BASE = "/api/projects/proj"
AGENT = f"{BASE}/agents/agent-a"
SESSIONS = f"{AGENT}/endpoint/sessions"
RESPONSES = f"{AGENT}/endpoint/protocols/openai/responses"
AGENT_DETAILS = {
    "name": "agent-a",
    "id": "agent-a",
    "versions": {"latest": {"version": "7", "name": "agent-a", "id": "agent-a:7"}},
}


@pytest.fixture
async def rig() -> Any:
    backend = Backend()
    adapter = build_adapter(backend)
    await adapter.start()
    yield backend, adapter
    await adapter.close()


def context(stream: bool = False) -> InvokeContext:
    return InvokeContext(
        agent_name="agent-a",
        session_id="sess-1",
        payload={"input": "hello", "metadata": {"k": "v"}},
        timeout_seconds=9,
        request_id="r",
        correlation_id="c",
        stream=stream,
    )


async def test_list_sessions_uses_agent_scoped_endpoint_with_bearer_auth(rig: Any) -> None:
    backend, adapter = rig
    backend.route("GET", "/endpoint/sessions", 200, {"data": [SESSION_JSON], "has_more": False})
    sessions = [s async for s in adapter.list_sessions("agent-a")]
    request = backend.requests[0]
    assert request.method == "GET" and request.path == SESSIONS
    assert request.query["api-version"] == "v1" and request.query["limit"] == "100"
    assert request.headers["Authorization"] == "Bearer test-token"
    assert [s.session_id for s in sessions] == ["sess-1"]
    assert sessions[0].agent_version == "7" and sessions[0].status is FoundrySessionStatus.ACTIVE


async def test_list_sessions_follows_pagination(rig: Any) -> None:
    backend, adapter = rig
    page = {"data": [{**SESSION_JSON, "agent_session_id": "a"}], "has_more": True, "last_id": "a"}
    last = {"data": [{**SESSION_JSON, "agent_session_id": "b"}], "has_more": False}
    backend.routes[("GET", "/endpoint/sessions")] = lambda r: (
        200,
        last if "after" in r.query or "before" in r.query else page,
        {},
    )
    sessions = [s async for s in adapter.list_sessions("agent-a")]
    assert [s.session_id for s in sessions] == ["a", "b"]
    assert len(backend.requests) == 2
    cursor_keys = set(backend.requests[1].query) - {"limit", "api-version"}
    assert cursor_keys, "the second page must carry a cursor"


async def test_get_session(rig: Any) -> None:
    backend, adapter = rig
    backend.route("GET", "/endpoint/sessions/sess-1", 200, SESSION_JSON)
    session = await adapter.get_session("agent-a", "sess-1")
    assert backend.requests[0].path == f"{SESSIONS}/sess-1" and session.session_id == "sess-1"


@pytest.mark.parametrize(
    "status", ["creating", "active", "idle", "updating", "failed", "deleting", "deleted", "expired"]
)
async def test_every_documented_status_round_trips_through_the_real_models(
    rig: Any, status: str
) -> None:
    backend, adapter = rig
    backend.route("GET", "/endpoint/sessions/sess-1", 200, {**SESSION_JSON, "status": status})
    assert (await adapter.get_session("agent-a", "sess-1")).status.value == status


async def test_create_session_pins_the_latest_version_and_expects_201(rig: Any) -> None:
    backend, adapter = rig
    backend.route("GET", "/agents/agent-a", 200, AGENT_DETAILS)
    backend.route("POST", "/endpoint/sessions", 201, {**SESSION_JSON, "status": "creating"})
    session = await adapter.create_session("agent-a")
    lookup, create = backend.requests
    assert lookup.method == "GET" and lookup.path == AGENT
    assert create.method == "POST" and create.path == SESSIONS
    assert create.body == {"version_indicator": {"type": "version_ref", "agent_version": "7"}}
    assert session.status is FoundrySessionStatus.CREATING


async def test_delete_session_is_idempotent_and_tolerates_404_as_not_found(rig: Any) -> None:
    backend, adapter = rig
    backend.route("DELETE", "/endpoint/sessions/sess-1", 204)
    await adapter.delete_session("agent-a", "sess-1")
    assert (
        backend.requests[0].method == "DELETE" and backend.requests[0].path == f"{SESSIONS}/sess-1"
    )
    backend.route(
        "DELETE", "/endpoint/sessions/sess-1", 404, {"error": {"code": "not_found", "message": "x"}}
    )
    with pytest.raises(FoundrySessionNotFound):
        await adapter.delete_session("agent-a", "sess-1")


async def test_list_agents_requests_hosted_kind(rig: Any) -> None:
    backend, adapter = rig
    backend.route(
        "GET",
        f"{BASE}/agents",
        200,
        {
            "data": [
                {"name": "agent-a", "id": "agent-a", "versions": {"latest": {"version": "1"}}}
            ],
            "has_more": False,
        },
    )
    agents = [a async for a in adapter.list_agents()]
    assert backend.requests[0].query["kind"] == "hosted" and [a.name for a in agents] == ["agent-a"]


# ------------------------------------------------------------------ invocation


async def test_invoke_posts_the_session_in_the_body_to_the_responses_endpoint(rig: Any) -> None:
    backend, adapter = rig
    backend.route(
        "POST",
        "/protocols/openai/responses",
        200,
        {
            "id": "resp_1",
            "object": "response",
            "status": "completed",
            "agent_session_id": "sess-1",
            "output": [],
        },
    )
    response = await adapter.invoke(context())
    request = backend.requests[0]
    assert request.method == "POST" and request.path == RESPONSES
    assert request.query["api-version"] == "v1"
    assert request.body == {"input": "hello", "metadata": {"k": "v"}, "agent_session_id": "sess-1"}
    assert request.headers["authorization"] == "Bearer test-token"
    assert "agent_session_id" not in request.query  # not an Invocations-protocol query parameter
    assert response.body is not None
    assert response.body["id"] == "resp_1" and response.body["status"] == "completed"
    assert "agent_session_id" not in json.dumps(response.body) and "sess-1" not in json.dumps(
        response.body
    )
    assert None not in response.body.values()  # null noise is dropped


async def test_invoke_streaming_relays_sse_events_without_the_session_id(rig: Any) -> None:
    backend, adapter = rig
    events = (
        b'event: response.created\ndata: {"type":"response.created","sequence_number":0,'
        b'"response":{"id":"resp_1","object":"response","status":"in_progress",'
        b'"agent_session_id":"sess-1","output":[]}}\n\n'
        b"event: response.output_text.delta\n"
        b'data: {"type":"response.output_text.delta","sequence_number":1,'
        b'"item_id":"i","output_index":0,"content_index":0,"delta":"Hi","logprobs":[]}\n\n'
    )
    backend.route(
        "POST", "/protocols/openai/responses", 200, events, {"content-type": "text/event-stream"}
    )
    response = await adapter.invoke(context(stream=True))
    assert (
        backend.requests[0].body["stream"] is True
        and backend.requests[0].body["agent_session_id"] == "sess-1"
    )
    assert response.stream is not None
    text = b"".join([frame async for frame in response.stream]).decode()
    assert "event: response.created" in text and "event: response.output_text.delta" in text
    assert "sess-1" not in text and '"delta":"Hi"' in text


# --------------------------------------------------- failure behaviour on the wire


async def test_throttling_is_surfaced_once_with_retry_after_and_never_retried_by_the_sdk(
    rig: Any,
) -> None:
    backend, adapter = rig
    backend.route(
        "GET",
        "/endpoint/sessions/sess-1",
        429,
        {"error": {"code": "throttled"}},
        {"Retry-After": "3"},
    )
    with pytest.raises(FoundryThrottled) as info:
        await adapter.get_session("agent-a", "sess-1")
    assert info.value.retry_after_seconds == 3.0
    assert len(backend.requests) == 1  # the pool owns retries


async def test_server_errors_are_surfaced_once(rig: Any) -> None:
    backend, adapter = rig
    backend.route("POST", "/endpoint/sessions", 503, {"error": {"code": "unavailable"}})
    backend.route("GET", "/agents/agent-a", 200, AGENT_DETAILS)
    with pytest.raises(FoundryUnavailable):
        await adapter.create_session("agent-a")
    assert [r.method for r in backend.requests] == ["GET", "POST"]


async def test_authorisation_failures_are_rejections_not_retries(rig: Any) -> None:
    backend, adapter = rig
    backend.route("GET", "/endpoint/sessions/sess-1", 403, {"error": {"code": "forbidden"}})
    with pytest.raises(FoundryRejected) as info:
        await adapter.get_session("agent-a", "sess-1")
    assert info.value.status_code == 403 and len(backend.requests) == 1


async def test_invoke_throttle_comes_through_the_openai_client_once(rig: Any) -> None:
    backend, adapter = rig
    backend.route(
        "POST",
        "/protocols/openai/responses",
        429,
        {"error": {"message": "slow down"}},
        {"Retry-After": "4"},
    )
    with pytest.raises(FoundryThrottled) as info:
        await adapter.invoke(context())
    assert info.value.retry_after_seconds == 4.0
    assert len(backend.requests) == 1  # max_retries=0 on the OpenAI client


async def test_invoke_not_found_distinguishes_a_missing_session_from_a_missing_route(
    rig: Any,
) -> None:
    backend, adapter = rig
    backend.route("POST", "/protocols/openai/responses", 404, {"error": {"message": "not found"}})
    backend.route("GET", "/endpoint/sessions/sess-1", 404, {"error": {"code": "not_found"}})
    with pytest.raises(FoundrySessionNotFound):
        await adapter.invoke(context())
    backend.route("GET", "/endpoint/sessions/sess-1", 200, SESSION_JSON)
    with pytest.raises(FoundryRejected):
        await adapter.invoke(context())


async def test_invoke_server_error_on_a_failed_session_is_session_failed(rig: Any) -> None:
    backend, adapter = rig
    backend.route("POST", "/protocols/openai/responses", 500, {"error": {"message": "boom"}})
    backend.route("GET", "/endpoint/sessions/sess-1", 200, {**SESSION_JSON, "status": "failed"})
    with pytest.raises(FoundrySessionFailed):
        await adapter.invoke(context())


# ------------------------------------------------------------ whole stack, real SDK


async def test_pool_end_to_end_over_the_real_sdk_clients() -> None:
    from hosted_agent_kit.adapters.memory_affinity import MemoryAffinityStore
    from hosted_agent_kit.adapters.memory_queue import MemoryQueueManager
    from hosted_agent_kit.adapters.memory_registry import MemorySessionRegistry
    from hosted_agent_kit.services.clock import SystemClock
    from hosted_agent_kit.services.metrics import InMemoryMetrics
    from hosted_agent_kit.services.pool import PoolService
    from hosted_agent_kit.services.reconciler import Reconciler
    from hosted_agent_kit.services.scheduler import Scheduler
    from tests.conftest import make_config, make_request, make_settings

    backend = Backend()
    created: list[str] = []

    def create(_: Any) -> tuple[int, Any, dict[str, str]]:
        sid = f"sess-{len(created) + 1}"
        created.append(sid)
        return 201, {**SESSION_JSON, "agent_session_id": sid, "status": "active"}, {}

    backend.routes[("POST", "/endpoint/sessions")] = create
    backend.route("GET", "/agents/agent-a", 200, AGENT_DETAILS)
    backend.routes[("GET", "/endpoint/sessions")] = lambda _: (
        200,
        {"data": [{**SESSION_JSON, "agent_session_id": sid} for sid in created], "has_more": False},
        {},
    )
    backend.route(
        "POST",
        "/protocols/openai/responses",
        200,
        {"id": "resp", "object": "response", "status": "completed", "output": []},
    )
    adapter = build_adapter(backend)
    await adapter.start()
    config = make_config({"agent-a": {"mode": "stateful", "max_sessions": 2}}, {})
    registry = MemorySessionRegistry()
    metrics = InMemoryMetrics()
    clock = SystemClock()
    pool = PoolService(
        config=config,
        adapter=adapter,
        registry=registry,
        affinity=MemoryAffinityStore(),
        queue=MemoryQueueManager(),
        metrics=metrics,
        scheduler=Scheduler(),
        clock=clock,
        settings=make_settings(),
    )
    reconciler = Reconciler(
        config=config, adapter=adapter, registry=registry, pool=pool, metrics=metrics, clock=clock
    )
    try:
        for user in ("u1", "u1", "u2"):
            result = await pool.execute(make_request("agent-a", user))
            assert result.body is not None and result.body["status"] == "completed"
        invokes = [r for r in backend.requests if r.path.endswith("/responses")]
        assert [r.body["agent_session_id"] for r in invokes] == ["sess-1", "sess-1", "sess-2"]
        report = await reconciler.run_agent("agent-a")
        assert report.outcome == "success" and report.discovered == 2 and report.removed == 0
    finally:
        await adapter.close()
    assert isinstance(adapter, SdkFoundryAdapter)
