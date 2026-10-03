"""The Python client, against the real service and against scripted responses."""

from __future__ import annotations

import json
from collections.abc import AsyncIterator
from typing import Any

import httpx
import pytest

from hosted_agent_kit.domain.errors import FoundryUnavailable
from hosted_agent_kit.ports.foundry import UpstreamResponse
from hosted_agent_kit_client import (
    PoolClient,
    PoolError,
    PoolStreamError,
    SseEvent,
    parse_sse,
)
from tests.integration.conftest import Api, start_api

AGENTS: dict[str, dict[str, Any]] = {
    "chat": {"mode": "stateful", "max_sessions": 3},
    "pool": {"mode": "stateless", "max_sessions": 2},
    "inv": {"mode": "stateless", "protocol": "invocations", "max_sessions": 2},
}


@pytest.fixture
async def rig() -> AsyncIterator[tuple[PoolClient, Api]]:
    api, manager = await start_api(agents=AGENTS, defaults={})
    client = PoolClient("http://test", http=api.client)
    try:
        yield client, api
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


# --------------------------------------------------------------- against the service


async def test_agents_can_be_discovered(rig: tuple[PoolClient, Api]) -> None:
    client, _ = rig
    agents = await client.agents()
    assert [a.name for a in agents] == ["chat", "pool", "inv"]
    inv = await client.agent("inv")
    assert inv.protocol == "invocations" and inv.limits.max_response_bytes is not None
    assert (await client.agent("chat")).supports_conversation_key is True


async def test_respond_and_chat_return_typed_results(rig: tuple[PoolClient, Api]) -> None:
    client, _ = rig
    result = await client.respond("pool", {"input": "hi"})
    assert result.request_id.startswith("req_") and result.result["echo"] == {"input": "hi"}
    assert result.correlation_id and result.replayed is False
    chat = await client.chat("pool", "hello")
    assert chat.result["echo"]["input"] == "hello"


async def test_a_repeated_idempotency_key_is_reported_as_a_replay(
    rig: tuple[PoolClient, Api],
) -> None:
    client, api = rig
    first = await client.respond("pool", {"input": "x"}, idempotency_key="k1")
    again = await client.respond("pool", {"input": "x"}, idempotency_key="k1")
    assert first.replayed is False and again.replayed is True
    assert len(api.fake.invocations) == 1


async def test_conversation_keys_are_sent(rig: tuple[PoolClient, Api]) -> None:
    client, api = rig
    for key in ("a", "b", "a"):
        await client.respond("chat", {"input": "x"}, conversation_key=key)
    assert len(api.fake.created) == 2


async def test_streaming_yields_events_in_order(rig: tuple[PoolClient, Api]) -> None:
    client, _ = rig
    events = [e async for e in client.stream_responses("pool", {"input": "x"})]
    assert [e.event for e in events] == ["a", "b"] and [e.data for e in events] == [1, 2]


async def test_a_stream_that_fails_raises_with_the_problem_details(
    rig: tuple[PoolClient, Api],
) -> None:
    client, api = rig
    api.fake.stream_error = FoundryUnavailable("reset")
    seen: list[SseEvent] = []

    async def consume() -> None:
        async for event in client.stream_responses("pool", {"input": "x"}):
            seen.append(event)

    with pytest.raises(PoolStreamError) as info:
        await consume()
    assert len(seen) == 2  # the frames before the failure were delivered
    error = info.value
    assert error.error_code == "FOUNDRY_UNAVAILABLE" and error.phase == "stream"
    assert error.retry_safe is False and error.request_id and error.correlation_id
    assert error.retry_after_seconds == api.settings.retry_after_seconds


async def test_a_failure_before_the_stream_starts_raises_a_normal_error(
    rig: tuple[PoolClient, Api],
) -> None:
    client, _ = rig

    async def consume() -> None:
        async for _ in client.stream_responses("ghost", {"input": "x"}):
            pass

    with pytest.raises(PoolError) as info:
        await consume()
    assert info.value.status == 404 and not isinstance(info.value, PoolStreamError)


async def test_invocations_send_any_body_and_return_the_raw_response(
    rig: tuple[PoolClient, Api],
) -> None:
    client, api = rig
    api.fake.invoke_handler = lambda _c: UpstreamResponse(
        status_code=201, raw=b'{"ok":true}', media_type="application/json"
    )
    result = await client.invoke("inv", [1, 2, {"a": 3}])
    assert result.status_code == 201 and result.json() == {"ok": True}
    assert result.content_type == "application/json" and result.request_id
    assert api.fake.invocations[-1].raw_body == b'[1, 2, {"a": 3}]'
    await client.invoke("inv", b"\x00\xff")
    assert api.fake.invocations[-1].content_type == "application/octet-stream"
    await client.invoke("inv", "plain", content_type="text/markdown")
    assert api.fake.invocations[-1].raw_body == b"plain"
    assert api.fake.invocations[-1].content_type == "text/markdown"
    await client.invoke("inv")
    assert api.fake.invocations[-1].raw_body == b""


async def test_an_invocations_agent_can_stream_events(rig: tuple[PoolClient, Api]) -> None:
    client, api = rig
    api.fake.invoke_handler = lambda _c: UpstreamResponse(
        stream=api.fake._stream(), media_type="text/event-stream"
    )
    events = [e async for e in client.stream_invocation("inv", {"go": 1})]
    assert [e.event for e in events] == ["a", "b"]


async def test_errors_carry_the_problem_details(rig: tuple[PoolClient, Api]) -> None:
    client, _ = rig
    with pytest.raises(PoolError) as info:
        await client.respond("ghost", {"input": "x"})
    error = info.value
    assert error.status == 404 and error.error_code == "AGENT_NOT_CONFIGURED"
    assert error.phase == "request" and error.retry_safe is True
    assert error.request_id and error.correlation_id and "AGENT_NOT_CONFIGURED" in str(error)
    with pytest.raises(PoolError) as wrong:
        await client.invoke("pool", {"x": 1})
    assert wrong.value.error_code == "VALIDATION_ERROR"


# ----------------------------------------------------------------- scripted responses


def problem(
    status: int, code: str, *, safe: bool, retry_after: int | None = None
) -> httpx.Response:
    body: dict[str, Any] = {
        "type": f"urn:hosted-agent-kit:error:{code}",
        "title": code,
        "status": status,
        "detail": "d",
        "error_code": code,
        "phase": "queue",
        "retry_safe": safe,
    }
    headers = {}
    if retry_after is not None:
        body["retry_after_seconds"] = retry_after
        headers["Retry-After"] = str(retry_after)
    return httpx.Response(status, json=body, headers=headers)


OK = httpx.Response(200, json={"request_id": "req_1", "result": {}})


def scripted(
    responses: list[httpx.Response | Exception], **kwargs: Any
) -> tuple[PoolClient, list[httpx.Request], list[float]]:
    seen: list[httpx.Request] = []
    sleeps: list[float] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        item = responses.pop(0)
        if isinstance(item, Exception):
            raise item
        return item

    async def record_sleep(seconds: float) -> None:
        sleeps.append(seconds)

    http = httpx.AsyncClient(transport=httpx.MockTransport(handler), base_url="http://svc")
    return PoolClient("http://svc", http=http, sleep=record_sleep, **kwargs), seen, sleeps


async def test_a_safe_error_with_a_delay_is_retried_after_at_least_that_delay() -> None:
    client, seen, sleeps = scripted([problem(429, "QUEUE_FULL", safe=True, retry_after=3), OK])
    result = await client.respond("a", {})
    assert result.request_id == "req_1" and len(seen) == 2
    assert sleeps and sleeps[0] >= 3
    ids = {r.headers["x-correlation-id"] for r in seen}
    assert len(ids) == 1  # one call, one correlation id, however many attempts


async def test_an_error_that_is_not_safe_to_repeat_is_not_retried() -> None:
    client, seen, _ = scripted([problem(504, "FOUNDRY_TIMEOUT", safe=False), OK])
    with pytest.raises(PoolError) as info:
        await client.respond("a", {})
    assert info.value.retry_safe is False and len(seen) == 1


async def test_a_delay_longer_than_the_limit_is_returned_not_waited_for() -> None:
    client, seen, sleeps = scripted(
        [problem(429, "UPSTREAM_THROTTLED", safe=True, retry_after=120), OK], max_retry_after=30
    )
    with pytest.raises(PoolError) as info:
        await client.respond("a", {})
    assert info.value.retry_after_seconds == 120 and len(seen) == 1 and not sleeps


async def test_retries_stop_at_the_limit() -> None:
    client, seen, _ = scripted(
        [problem(503, "FOUNDRY_CIRCUIT_OPEN", safe=True, retry_after=1)] * 5, max_retries=2
    )
    with pytest.raises(PoolError):
        await client.respond("a", {})
    assert len(seen) == 3  # the first attempt and two retries


async def test_client_errors_are_never_retried() -> None:
    client, seen, _ = scripted([problem(422, "VALIDATION_ERROR", safe=True), OK])
    with pytest.raises(PoolError):
        await client.respond("a", {})
    assert len(seen) == 1


async def test_a_connection_failure_is_retried_because_nothing_was_sent() -> None:
    client, seen, sleeps = scripted([httpx.ConnectError("refused"), OK])
    assert (await client.respond("a", {})).request_id == "req_1"
    assert len(seen) == 2 and len(sleeps) == 1
    exhausted, _, _ = scripted([httpx.ConnectError("x")] * 3, max_retries=1)
    with pytest.raises(httpx.ConnectError):
        await exhausted.respond("a", {})


async def test_the_token_is_fetched_for_every_attempt_and_may_be_async() -> None:
    tokens = iter(["t1", "t2"])

    async def provider() -> str:
        return next(tokens)

    client, seen, _ = scripted(
        [problem(429, "QUEUE_FULL", safe=True, retry_after=1), OK], token=provider
    )
    await client.respond("a", {})
    assert [r.headers["authorization"] for r in seen] == ["Bearer t1", "Bearer t2"]
    plain, seen2, _ = scripted([OK], token="static")
    await plain.respond("a", {})
    assert seen2[0].headers["authorization"] == "Bearer static"
    sync, seen3, _ = scripted([OK], token=lambda: "from-callable")
    await sync.respond("a", {})
    assert seen3[0].headers["authorization"] == "Bearer from-callable"
    anonymous, seen4, _ = scripted([OK])
    await anonymous.respond("a", {})
    assert "authorization" not in seen4[0].headers


async def test_headers_and_body_for_each_option() -> None:
    client, seen, _ = scripted([OK, OK])
    await client.respond(
        "a",
        {"input": "x"},
        conversation_key="c1",
        subject="alice",
        idempotency_key="key-1",
        timeout_seconds=9,
    )
    request = seen[0]
    assert request.headers["x-conversation-key"] == "c1"
    assert request.headers["x-pool-subject"] == "alice"
    assert request.headers["idempotency-key"] == "key-1"
    assert json.loads(request.content) == {"input": {"input": "x"}, "timeout_seconds": 9}
    await client.chat("a", "hi", previous_response_id="r1", conversation_id=None)
    assert json.loads(seen[1].content) == {"message": "hi", "previous_response_id": "r1"}


async def test_an_error_that_is_not_problem_json_still_raises_a_pool_error() -> None:
    client, _, _ = scripted([httpx.Response(502, text="bad gateway from a proxy")])
    with pytest.raises(PoolError) as info:
        await client.respond("a", {})
    assert info.value.status == 502 and info.value.error_code == "HTTP_ERROR"
    assert "bad gateway" in info.value.detail and info.value.retry_safe is False


async def test_the_client_can_be_used_as_a_context_manager() -> None:
    async with PoolClient("http://svc", token="t") as client:
        assert client is not None


# ------------------------------------------------------------------------ SSE parser


async def chunks(*parts: str) -> AsyncIterator[str]:
    for part in parts:
        yield part


async def test_the_parser_handles_split_chunks_comments_crlf_and_multiline_data() -> None:
    parts = (
        ': comment\r\nevent: a\r\ndata: {"n":',
        "1}\r\n\r\ndata: line1\n",
        "data: line2\n\nevent: last\ndata: tail",
    )
    events = [e async for e in parse_sse(chunks(*parts))]
    assert [(e.event, e.data) for e in events] == [
        ("a", {"n": 1}),
        ("message", "line1\nline2"),
        ("last", "tail"),
    ]
    assert events[1].raw == "line1\nline2"


async def test_the_parser_ignores_empty_events() -> None:
    assert [e async for e in parse_sse(chunks("\n\nevent: x\n\n"))] == []
