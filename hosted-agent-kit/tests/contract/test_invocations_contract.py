"""The Invocations protocol, the isolation-key header and caller-chosen session ids.

The real azure-ai-projects client sends every request. Only the HTTP layer is faked.
"""

from __future__ import annotations

import json
from typing import Any

import pytest

from hosted_agent_kit.adapters.foundry_sdk import SdkFoundryAdapter
from hosted_agent_kit.domain.enums import AgentProtocol
from hosted_agent_kit.domain.errors import (
    FoundryConflict,
    FoundryRejected,
    FoundryResponseTooLarge,
    FoundrySessionFailed,
    FoundrySessionNotFound,
    FoundryThrottled,
    FoundryTimeout,
    FoundryUnavailable,
)
from hosted_agent_kit.domain.models import InvokeContext
from tests.contract.sdk_backend import SESSION_JSON, Backend, build_adapter

AGENT = "/api/projects/proj/agents/agent-a"
INVOCATIONS = f"{AGENT}/endpoint/protocols/invocations"
ROUTE = "/protocols/invocations"
HEADER = "x-ms-user-isolation-key"


@pytest.fixture
async def rig() -> Any:
    backend = Backend()
    adapter = build_adapter(backend)
    await adapter.start()
    yield backend, adapter
    await adapter.close()


def context(stream: bool = False, timeout: float = 9) -> InvokeContext:
    return InvokeContext(
        agent_name="agent-a",
        session_id="sess-1",
        payload={"task": "summarise", "n": 2, "stream": True},
        timeout_seconds=timeout,
        request_id="r",
        correlation_id="c",
        stream=stream,
        protocol=AgentProtocol.INVOCATIONS,
    )


async def test_the_payload_is_posted_with_the_session_in_the_query_string(rig: Any) -> None:
    backend, adapter = rig
    backend.route("POST", ROUTE, 200, {"answer": 42, "agent_session_id": "sess-1"})
    response = await adapter.invoke(context())
    request = backend.requests[0]
    assert request.method == "POST" and request.path == INVOCATIONS
    assert request.query == {"api-version": "v1", "agent_session_id": "sess-1"}
    assert request.body == {"task": "summarise", "n": 2, "stream": True}  # sent as given
    assert request.headers["Authorization"] == "Bearer test-token"
    assert HEADER not in {k.lower() for k in request.headers}
    assert response.raw == b'{"answer":42}' and response.body is None and response.stream is None
    assert response.media_type == "application/json" and response.status_code == 200
    assert b"sess-1" not in response.raw


@pytest.mark.parametrize(
    ("body", "content_type", "expected"),
    [
        (b"plain text answer", "text/plain; charset=utf-8", b"plain text answer"),
        (b"{not json", "application/json", b"{not json"),
        ([1, 2, {"agent_session_id": "x", "k": 1}], "application/json", b'[1,2,{"k":1}]'),
        ({"agent_session_id": "x", "k": 1}, "application/vnd.api+json", b'{"k":1}'),
        (b"\x00\x01binary", "application/octet-stream", b"\x00\x01binary"),
    ],
)
async def test_responses_are_returned_as_the_agent_sent_them(
    rig: Any, body: Any, content_type: str, expected: bytes
) -> None:
    backend, adapter = rig
    backend.route("POST", ROUTE, 200, body, {"content-type": content_type})
    response = await adapter.invoke(context())
    assert response.raw == expected and response.media_type == content_type


async def test_a_response_without_a_content_type_is_labelled_as_bytes(rig: Any) -> None:
    backend, adapter = rig
    backend.routes[("POST", ROUTE)] = lambda _: (202, b"accepted", {"content-type": ""})
    response = await adapter.invoke(context())
    assert response.status_code == 202 and response.raw == b"accepted"
    assert response.media_type == "application/octet-stream"


async def test_a_server_sent_event_response_is_relayed_as_a_stream(rig: Any) -> None:
    backend, adapter = rig
    events = b'event: token\ndata: {"t":"Hi"}\n\nevent: done\ndata: {}\n\n'
    backend.route("POST", ROUTE, 200, events, {"content-type": "text/event-stream; charset=utf-8"})
    response = await adapter.invoke(context())
    assert response.raw is None and response.stream is not None
    assert response.media_type.startswith("text/event-stream")
    assert b"".join([chunk async for chunk in response.stream]) == events


# ------------------------------------------------------------------ failures


@pytest.mark.parametrize(
    ("status", "headers", "expected"),
    [
        (429, {"Retry-After": "3"}, FoundryThrottled),
        (400, {}, FoundryRejected),
        (403, {}, FoundryRejected),
        (503, {}, FoundryUnavailable),
        (504, {}, FoundryTimeout),
    ],
)
async def test_http_errors_are_translated_without_extra_calls(
    rig: Any, status: int, headers: dict[str, str], expected: type[Exception]
) -> None:
    backend, adapter = rig
    backend.route("POST", ROUTE, status, {"error": {"message": "x"}}, headers)
    with pytest.raises(expected) as info:
        await adapter.invoke(context())
    if expected is FoundryThrottled:
        assert info.value.retry_after_seconds == 3.0  # type: ignore[attr-defined]
    assert len(backend.requests) == 1


async def test_a_404_is_a_missing_session_only_if_the_session_is_really_gone(rig: Any) -> None:
    backend, adapter = rig
    backend.route("POST", ROUTE, 404, {"error": {"message": "nf"}})
    backend.route("GET", "/endpoint/sessions/sess-1", 404, {"error": {"code": "not_found"}})
    with pytest.raises(FoundrySessionNotFound):
        await adapter.invoke(context())
    backend.route("GET", "/endpoint/sessions/sess-1", 200, SESSION_JSON)
    with pytest.raises(FoundryRejected) as info:
        await adapter.invoke(context())
    assert info.value.status_code == 404


async def test_a_500_is_session_failed_or_not_found_or_unavailable_by_what_the_session_says(
    rig: Any,
) -> None:
    backend, adapter = rig
    backend.route("POST", ROUTE, 500, {"error": {"message": "boom"}})
    backend.route("GET", "/endpoint/sessions/sess-1", 200, {**SESSION_JSON, "status": "failed"})
    with pytest.raises(FoundrySessionFailed):
        await adapter.invoke(context())
    backend.route("GET", "/endpoint/sessions/sess-1", 200, SESSION_JSON)
    with pytest.raises(FoundryUnavailable):
        await adapter.invoke(context())
    backend.route("GET", "/endpoint/sessions/sess-1", 404, {"error": {"code": "not_found"}})
    with pytest.raises(FoundrySessionNotFound):
        await adapter.invoke(context())


async def test_a_slow_agent_times_out(rig: Any) -> None:
    backend, adapter = rig
    backend.route("POST", ROUTE, 200, {"ok": True})
    backend.delay = 0.5
    with pytest.raises(FoundryTimeout):
        await adapter.invoke(context(timeout=0.05))


# ------------------------------------------------- isolation key and caller ids


async def test_an_isolation_key_is_sent_on_every_call_when_configured() -> None:
    backend = Backend()
    adapter = build_adapter(backend, isolation_key="pool-key")
    await adapter.start()
    try:
        backend.route("GET", "/endpoint/sessions", 200, {"data": [SESSION_JSON], "has_more": False})
        backend.route("GET", "/endpoint/sessions/sess-1", 200, SESSION_JSON)
        backend.route("DELETE", "/endpoint/sessions/sess-1", 204)
        backend.route("POST", ":stop", 204)
        backend.route("POST", ROUTE, 200, {"ok": True})
        backend.route(
            "POST",
            "/protocols/openai/responses",
            200,
            {"id": "r", "object": "response", "status": "completed", "output": []},
        )
        _ = [s async for s in adapter.list_sessions("agent-a")]
        await adapter.get_session("agent-a", "sess-1")
        await adapter.stop_session("agent-a", "sess-1")
        await adapter.delete_session("agent-a", "sess-1")
        await adapter.invoke(context())
        responses = context().model_copy(update={"protocol": AgentProtocol.RESPONSES})
        await adapter.invoke(responses)
    finally:
        await adapter.close()
    assert len(backend.requests) == 6
    for request in backend.requests:
        headers = {k.lower(): v for k, v in request.headers.items()}
        assert headers.get(HEADER) == "pool-key", request.path


async def test_create_session_sends_the_header_and_the_caller_chosen_id() -> None:
    backend = Backend()
    adapter = build_adapter(backend, isolation_key="pool-key")
    await adapter.start()
    try:
        backend.route(
            "GET",
            "/agents/agent-a",
            200,
            {
                "name": "agent-a",
                "id": "agent-a",
                "versions": {"latest": {"version": "7", "name": "agent-a", "id": "agent-a:7"}},
            },
        )
        backend.route(
            "POST",
            "/endpoint/sessions",
            201,
            {**SESSION_JSON, "agent_session_id": "pool-abc", "status": "creating"},
        )
        session = await adapter.create_session("agent-a", "pool-abc")
        random_id = await adapter.create_session("agent-a")
    finally:
        await adapter.close()
    first, second = [r for r in backend.requests if r.method == "POST"]
    assert first.body["agent_session_id"] == "pool-abc"
    assert first.body["version_indicator"]["agent_version"] == "7"
    assert "agent_session_id" not in (second.body or {}) and session.session_id == "pool-abc"
    assert random_id is not None
    assert {k.lower(): v for k, v in first.headers.items()}[HEADER] == "pool-key"


async def test_an_existing_session_id_is_reported_as_a_conflict(rig: Any) -> None:
    backend, adapter = rig
    backend.route(
        "GET",
        "/agents/agent-a",
        200,
        {
            "name": "agent-a",
            "id": "agent-a",
            "versions": {"latest": {"version": "7", "name": "agent-a", "id": "agent-a:7"}},
        },
    )
    backend.route(
        "POST", "/endpoint/sessions", 409, {"error": {"code": "conflict", "message": "exists"}}
    )
    with pytest.raises(FoundryConflict):
        await adapter.create_session("agent-a", "pool-abc")
    assert isinstance(adapter, SdkFoundryAdapter)
    assert json.dumps(backend.requests[-1].body)


async def test_a_response_larger_than_the_limit_is_refused() -> None:
    backend = Backend()
    adapter = build_adapter(backend, max_response_bytes=20)
    await adapter.start()
    try:
        backend.route("POST", ROUTE, 200, b"x" * 64, {"Content-Type": "application/octet-stream"})
        with pytest.raises(FoundryResponseTooLarge):
            await adapter.invoke(context())
        backend.route("POST", ROUTE, 200, b"y" * 20)
        response = await adapter.invoke(context())  # exactly at the limit is allowed
        assert response.raw == b"y" * 20
    finally:
        await adapter.close()


# ------------------------------------------------------- per-user isolation headers

IDENTITY = "x-ms-user-identity"


async def test_per_user_headers_replace_the_constant_key_on_invocations_only() -> None:
    backend = Backend()
    adapter = build_adapter(backend, isolation_key="pool-key")
    await adapter.start()
    try:
        backend.route("POST", ROUTE, 200, {"ok": True})
        backend.route(
            "POST",
            "/protocols/openai/responses",
            200,
            {"id": "r", "object": "response", "status": "completed", "output": []},
        )
        base = context().model_copy(update={"isolation_key": "user-key", "acting_user": "alice"})
        await adapter.invoke(base)
        await adapter.invoke(base.model_copy(update={"protocol": AgentProtocol.RESPONSES}))
        await adapter.invoke(context().model_copy(update={"isolation_key": "user-key"}))
    finally:
        await adapter.close()
    first, second, third = ({k.lower(): v for k, v in r.headers.items()} for r in backend.requests)
    for headers in (first, second):
        assert headers[HEADER] == "user-key" and headers[IDENTITY] == "alice"
    assert third[HEADER] == "user-key" and IDENTITY not in third  # key only, no acting user


async def test_without_per_user_values_the_constant_key_is_unchanged() -> None:
    backend = Backend()
    adapter = build_adapter(backend, isolation_key="pool-key")
    await adapter.start()
    try:
        backend.route("POST", ROUTE, 200, {"ok": True})
        await adapter.invoke(context())
    finally:
        await adapter.close()
    headers = {k.lower(): v for k, v in backend.requests[0].headers.items()}
    assert headers[HEADER] == "pool-key" and IDENTITY not in headers


async def test_the_trace_context_is_sent_with_an_invocation(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from opentelemetry.sdk.trace import TracerProvider

    from hosted_agent_kit import tracing

    monkeypatch.setattr(tracing, "_tracer", TracerProvider().get_tracer("test"))
    backend = Backend()
    adapter = build_adapter(backend)
    await adapter.start()
    try:
        backend.route("POST", ROUTE, 200, {"ok": True})
        with tracing.span("caller"):
            await adapter.invoke(context())
        await adapter.invoke(context())  # outside any span: no header
    finally:
        await adapter.close()
    inside = {k.lower(): v for k, v in backend.requests[0].headers.items()}
    outside = {k.lower(): v for k, v in backend.requests[1].headers.items()}
    assert inside["traceparent"].startswith("00-") and "baggage" not in inside
    assert "traceparent" not in outside
