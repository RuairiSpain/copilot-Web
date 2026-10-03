"""Client API: envelopes, headers, validation, errors and streaming (A1 to A7, A12)."""

from __future__ import annotations

import asyncio
import json
from typing import Any

import httpx
import pytest

from hosted_agent_kit.domain.errors import (
    FoundryRejected,
    FoundrySessionFailed,
    FoundrySessionNotFound,
    FoundryThrottled,
    FoundryTimeout,
    FoundryUnavailable,
)
from tests.conftest import eventually, settle
from tests.integration.conftest import Api, start_api

INVOKE = "/v1/agents/research-agent/invoke"
BODY = {"input": {"input": "hello"}}


async def test_invoke_returns_envelope_without_session_identifiers(api: Api) -> None:
    response = await api.client.post(INVOKE, json=BODY, headers=api.headers())
    assert response.status_code == 200
    data = response.json()
    assert data["result"]["status"] == "completed" and data["request_id"].startswith("req_")
    session_id = api.fake.created[0]
    assert session_id not in response.text
    assert not any(session_id in v for v in response.headers.values())
    assert "x-pool-session-id" not in response.headers


async def test_correlation_id_is_echoed_or_generated(api: Api) -> None:
    echoed = await api.client.post(
        INVOKE, json=BODY, headers=api.headers(**{"X-Correlation-ID": "trace-123"})
    )
    assert echoed.headers["x-correlation-id"] == "trace-123"
    assert api.fake.invocations[-1].correlation_id == "trace-123"
    generated = await api.client.post(INVOKE, json=BODY, headers=api.headers())
    assert len(generated.headers["x-correlation-id"]) == 32
    unsafe = await api.client.post(
        INVOKE, json=BODY, headers=api.headers(**{"X-Correlation-ID": "bad value\t!"})
    )
    assert unsafe.headers["x-correlation-id"] != "bad value\t!"
    assert generated.headers["x-request-id"] != unsafe.headers["x-request-id"]


async def test_stateful_affinity_through_http(api: Api) -> None:
    """A1, A2 and A3 through the public API."""
    url = "/v1/agents/coding-agent/invoke"
    for user in ("u1", "u1", "u2"):
        assert (await api.client.post(url, json=BODY, headers=api.headers(user))).status_code == 200
    await api.client.post(INVOKE, json=BODY, headers=api.headers("u1"))
    sessions = [c.session_id for c in api.fake.invocations]
    assert sessions[0] == sessions[1] != sessions[2]
    assert len(api.fake.created) == 3  # u1/coding, u2/coding, u1/research


async def test_chat_builds_a_responses_payload(api: Api) -> None:
    response = await api.client.post(
        "/v1/agents/research-agent/chat",
        json={
            "message": "Create a script",
            "previous_response_id": "resp_1",
            "metadata": {"k": "v"},
        },
        headers=api.headers(),
    )
    assert response.status_code == 200
    assert api.fake.invocations[-1].payload == {
        "input": "Create a script",
        "metadata": {"k": "v"},
        "previous_response_id": "resp_1",
    }
    await api.client.post(
        "/v1/agents/research-agent/chat",
        json={"message": "x", "conversation_id": "conv_1"},
        headers=api.headers(),
    )
    assert api.fake.invocations[-1].payload["conversation"] == "conv_1"


@pytest.mark.parametrize(
    "body",
    [
        {"message": ""},
        {},
        {"message": "x", "previous_response_id": "a", "conversation_id": "b"},
        {"message": "x", "extra": 1},
        {"message": "x", "timeout_seconds": 0},
        {"message": "x", "metadata": {"k": 1}},
    ],
)
async def test_chat_validation_errors(api: Api, body: dict[str, Any]) -> None:
    response = await api.client.post(
        "/v1/agents/research-agent/chat", json=body, headers=api.headers()
    )
    assert response.status_code == 422 and response.json()["error_code"] == "VALIDATION_ERROR"
    assert api.fake.created == []


@pytest.mark.parametrize(
    "body",
    [
        {},
        {"input": "not-an-object"},
        {"input": {}, "extra": 1},
        {"input": {}, "timeout_seconds": -1},
    ],
)
async def test_invoke_validation_errors_are_sanitised(api: Api, body: dict[str, Any]) -> None:
    response = await api.client.post(INVOKE, json=body, headers=api.headers())
    problem = response.json()
    assert (
        response.status_code == 422
        and response.headers["content-type"] == "application/problem+json"
    )
    assert set(problem) >= {
        "type",
        "title",
        "status",
        "detail",
        "error_code",
        "correlation_id",
        "errors",
    }
    for error in problem["errors"]:
        assert set(error) == {"loc", "msg", "type"}  # never the submitted values


async def test_validation_errors_do_not_echo_secrets(api: Api) -> None:
    response = await api.client.post(INVOKE, json={"input": "S3CRET-VALUE"}, headers=api.headers())
    assert response.status_code == 422 and "S3CRET-VALUE" not in response.text


async def test_malformed_json_is_a_validation_error(api: Api) -> None:
    response = await api.client.post(
        INVOKE, content=b"{not json", headers={**api.headers(), "Content-Type": "application/json"}
    )
    assert response.status_code == 422 and response.json()["error_code"] == "VALIDATION_ERROR"


async def test_unknown_agent_is_404_and_makes_no_foundry_call(api: Api) -> None:
    response = await api.client.post("/v1/agents/ghost/invoke", json=BODY, headers=api.headers())
    assert response.status_code == 404 and response.json()["error_code"] == "AGENT_NOT_CONFIGURED"
    assert api.fake.created == [] and api.fake.invocations == []


@pytest.mark.parametrize(
    "name", ["..%2F..%2Fetc", "a%20b", "a%2Fb", "-bad", "x" * 200, "a;b", "%24%7Bjndi%7D"]
)
async def test_path_injection_is_rejected_before_any_work(api: Api, name: str) -> None:
    response = await api.client.post(f"/v1/agents/{name}/invoke", json=BODY, headers=api.headers())
    assert response.status_code in (404, 422)
    assert api.fake.created == [] and api.fake.invocations == []


async def test_unknown_route_and_method_use_problem_json(api: Api) -> None:
    missing = await api.client.get("/nope")
    assert missing.status_code == 404 and missing.json()["error_code"] == "NOT_FOUND"
    wrong = await api.client.get(INVOKE)
    assert wrong.status_code == 405 and wrong.json()["error_code"] == "METHOD_NOT_ALLOWED"


async def test_timeout_is_clamped_to_the_configured_maximum(api: Api) -> None:
    await api.client.post(INVOKE, json={**BODY, "timeout_seconds": 10**6}, headers=api.headers())
    assert api.fake.invocations[-1].timeout_seconds == api.settings.max_timeout_seconds
    await api.client.post(INVOKE, json={**BODY, "timeout_seconds": 5}, headers=api.headers())
    assert api.fake.invocations[-1].timeout_seconds == 5
    await api.client.post(INVOKE, json=BODY, headers=api.headers())
    assert api.fake.invocations[-1].timeout_seconds == api.settings.default_timeout_seconds


async def test_idempotency_key_makes_failed_session_retry_safe(api: Api) -> None:
    api.fake.invoke_errors = [FoundrySessionFailed()]
    ok = await api.client.post(
        INVOKE, json=BODY, headers=api.headers(**{"Idempotency-Key": "abc-123"})
    )
    assert ok.status_code == 200
    api.fake.invoke_errors = [FoundrySessionFailed()]
    refused = await api.client.post(INVOKE, json=BODY, headers=api.headers())
    assert refused.status_code == 502 and refused.json()["error_code"] == "SESSION_FAILED"


@pytest.mark.parametrize("key", ["has space", "x" * 300])
async def test_invalid_idempotency_keys_are_rejected(api: Api, key: str) -> None:
    response = await api.client.post(
        INVOKE, json=BODY, headers=api.headers(**{"Idempotency-Key": key})
    )
    assert response.status_code == 422


# ------------------------------------------------------------ error mapping


@pytest.mark.parametrize(
    ("error", "status", "code", "retry"),
    [
        (FoundrySessionNotFound(), 502, "SESSION_NOT_FOUND", False),
        (FoundrySessionFailed(), 502, "SESSION_FAILED", False),
        (FoundryThrottled(2.5), 429, "UPSTREAM_THROTTLED", True),
        (FoundryUnavailable("x"), 503, "FOUNDRY_UNAVAILABLE", False),
        (FoundryTimeout(), 504, "FOUNDRY_TIMEOUT", False),
        (FoundryRejected(403), 502, "UPSTREAM_ERROR", False),
        (FoundryRejected(400), 422, "VALIDATION_ERROR", False),
    ],
)
async def test_upstream_failures_map_to_the_documented_errors(
    api: Api, error: Exception, status: int, code: str, retry: bool
) -> None:
    api.fake.invoke_errors = [error, error, error, error]
    response = await api.client.post(INVOKE, json=BODY, headers=api.headers())
    problem = response.json()
    assert response.status_code == status and problem["error_code"] == code
    assert problem["correlation_id"] == response.headers["x-correlation-id"]
    assert response.headers["content-type"] == "application/problem+json"
    if retry:
        assert response.headers["retry-after"] == str(problem["retry_after_seconds"]) == "3"


async def test_internal_errors_hide_details_and_keep_correlation() -> None:
    api, manager = await start_api()
    try:
        api.fake.invoke_errors = [RuntimeError("db password is hunter2")]
        response = await api.client.post(
            INVOKE, json=BODY, headers=api.headers(**{"X-Correlation-ID": "c-500"})
        )
        assert response.status_code == 500
        problem = response.json()
        assert problem["error_code"] == "INTERNAL_ERROR" and problem["correlation_id"] == "c-500"
        assert "hunter2" not in response.text and "Traceback" not in response.text
        assert response.headers["x-correlation-id"] == "c-500"
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_queue_errors_over_http() -> None:
    api, manager = await start_api(
        agents={
            "a": {
                "mode": "stateless",
                "max_sessions": 1,
                "queue": {"max_depth": 1, "max_wait_seconds": 0.1},
            },
            "off": {"mode": "stateless", "max_sessions": 1, "queue": {"enabled": False}},
            "s": {"mode": "stateful", "max_sessions": 2, "queue": {"max_wait_seconds": 0.1}},
        }
    )
    try:
        api.fake.invoke_gate = asyncio.Event()

        async def post(agent: str, user: str = "u") -> httpx.Response:
            return await api.client.post(
                f"/v1/agents/{agent}/invoke", json=BODY, headers=api.headers(user)
            )

        first = asyncio.create_task(post("a", "1"))
        await settle()
        second = asyncio.create_task(post("a", "2"))
        await settle()
        full = await post("a", "3")  # A7
        assert full.status_code == 429 and full.json()["error_code"] == "QUEUE_FULL"
        assert full.headers["retry-after"] == "7" and full.json()["retry_after_seconds"] == 7
        timed_out = await second
        assert (
            timed_out.status_code == 504 and timed_out.json()["error_code"] == "QUEUE_WAIT_TIMEOUT"
        )
        disabled_first = asyncio.create_task(post("off", "1"))
        await settle()
        disabled = await post("off", "2")
        assert (
            disabled.status_code == 429
            and disabled.json()["error_code"] == "POOL_CAPACITY_EXCEEDED"
        )
        sticky_first = asyncio.create_task(post("s", "1"))
        await settle()
        sticky = await post("s", "1")
        assert sticky.status_code == 504 and sticky.json()["error_code"] == "STICKY_SESSION_TIMEOUT"
        api.fake.invoke_gate.set()
        for task in (first, disabled_first, sticky_first):
            assert (await task).status_code == 200
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


# ----------------------------------------------------------------- body size


async def test_declared_oversized_body_is_rejected_with_413() -> None:
    api, manager = await start_api(max_body_bytes=200)
    try:
        response = await api.client.post(
            INVOKE, json={"input": {"blob": "x" * 500}}, headers=api.headers()
        )
        assert response.status_code == 413 and response.json()["error_code"] == "PAYLOAD_TOO_LARGE"
        assert response.headers["x-correlation-id"]
        assert api.fake.created == []
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_streamed_oversized_body_without_content_length_is_rejected() -> None:
    api, manager = await start_api(max_body_bytes=200)
    try:

        async def chunks() -> Any:
            yield b'{"input": {"blob": "'
            for _ in range(10):
                yield b"x" * 100
            yield b'"}}'

        response = await api.client.post(
            INVOKE, content=chunks(), headers={**api.headers(), "Content-Type": "application/json"}
        )
        assert response.status_code == 413 and response.json()["error_code"] == "PAYLOAD_TOO_LARGE"
        assert api.fake.created == []
        small = await api.client.post(INVOKE, json={"input": {}}, headers=api.headers())
        assert small.status_code == 200
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


# ------------------------------------------------------------------ streaming


async def test_streaming_invoke_relays_frames_and_releases_the_lease(api: Api) -> None:
    async with api.client.stream(
        "POST", INVOKE, json={"input": {"input": "x", "stream": True}}, headers=api.headers()
    ) as response:
        assert response.status_code == 200
        assert response.headers["content-type"].startswith("text/event-stream")
        assert response.headers["cache-control"] == "no-cache"
        assert response.headers["x-correlation-id"]
        body = "".join([chunk async for chunk in response.aiter_text()])
    assert body == "event: a\ndata: 1\n\nevent: b\ndata: 2\n\n"
    (record,) = await api.container.pool.list_sessions("research-agent")
    assert record.lease_request_id is None


async def test_streaming_chat(api: Api) -> None:
    async with api.client.stream(
        "POST",
        "/v1/agents/research-agent/chat",
        json={"message": "x", "stream": True},
        headers=api.headers(),
    ) as response:
        text = "".join([chunk async for chunk in response.aiter_text()])
    assert text.startswith("event: a") and api.fake.invocations[-1].stream is True


async def test_stream_error_after_start_is_in_band(api: Api) -> None:
    api.fake.stream_error = FoundryUnavailable("x")
    async with api.client.stream(
        "POST", INVOKE, json={"input": {"input": "x", "stream": True}}, headers=api.headers()
    ) as response:
        text = "".join([chunk async for chunk in response.aiter_text()])
    assert response.status_code == 200 and "event: error" in text and "FOUNDRY_UNAVAILABLE" in text


async def test_pre_stream_failure_is_a_normal_problem_response(api: Api) -> None:
    api.fake.invoke_errors = [FoundryTimeout()]
    response = await api.client.post(
        INVOKE, json={"input": {"stream": True}}, headers=api.headers()
    )
    assert response.status_code == 504 and response.json()["error_code"] == "FOUNDRY_TIMEOUT"


# ----------------------------------------------------------------- disconnect


async def raw_call(
    app: Any, path: str, body: dict[str, Any], headers: dict[str, str], disconnect: asyncio.Event
) -> list[dict[str, Any]]:
    """Drive the ASGI app directly so the client can vanish mid-request."""
    payload = json.dumps(body).encode()
    sent = False
    messages: list[dict[str, Any]] = []

    async def receive() -> dict[str, Any]:
        nonlocal sent
        if not sent:
            sent = True
            return {"type": "http.request", "body": payload, "more_body": False}
        await disconnect.wait()
        return {"type": "http.disconnect"}

    async def send(message: dict[str, Any]) -> None:
        messages.append(message)

    scope = {
        "type": "http",
        "asgi": {"version": "3.0"},
        "http_version": "1.1",
        "method": "POST",
        "path": path,
        "raw_path": path.encode(),
        "query_string": b"",
        "headers": [
            (b"content-type", b"application/json"),
            (b"content-length", str(len(payload)).encode()),
            *[(k.lower().encode(), v.encode()) for k, v in headers.items()],
        ],
        "client": ("127.0.0.1", 1),
        "server": ("test", 80),
        "scheme": "http",
        "state": {},
    }
    await app(scope, receive, send)
    return messages


async def test_client_disconnect_cancels_work_and_releases_the_lease(api: Api) -> None:
    """A12."""
    api.fake.invoke_gate = asyncio.Event()
    disconnect = asyncio.Event()
    call = asyncio.create_task(raw_call(api.app, INVOKE, BODY, api.headers(), disconnect))

    async def leased() -> bool:
        records = await api.container.pool.list_sessions("research-agent")
        return bool(records) and records[0].lease_request_id is not None

    await eventually(leased)
    disconnect.set()
    messages = await asyncio.wait_for(call, 2)
    assert messages[0]["status"] == 499
    (record,) = await api.container.pool.list_sessions("research-agent")
    assert record.lease_request_id is None
    assert (await api.container.pool.snapshot("research-agent")).queue_depth == 0


async def test_client_disconnect_removes_queue_waiter(api: Api) -> None:
    """A12 (queue)."""
    api.fake.invoke_gate = asyncio.Event()
    holders = [
        asyncio.create_task(api.client.post(INVOKE, json=BODY, headers=api.headers(f"h{i}")))
        for i in range(2)
    ]
    await settle(20)
    disconnect = asyncio.Event()
    waiter = asyncio.create_task(raw_call(api.app, INVOKE, BODY, api.headers("w"), disconnect))

    async def queued() -> bool:
        return (await api.container.pool.snapshot("research-agent")).queue_depth == 1

    await eventually(queued)
    disconnect.set()
    await asyncio.wait_for(waiter, 2)
    assert (await api.container.pool.snapshot("research-agent")).queue_depth == 0
    api.fake.invoke_gate.set()
    for holder in holders:
        assert (await holder).status_code == 200


async def test_completed_request_beats_the_disconnect_watcher(api: Api) -> None:
    disconnect = asyncio.Event()
    messages = await asyncio.wait_for(raw_call(api.app, INVOKE, BODY, api.headers(), disconnect), 2)
    assert messages[0]["status"] == 200
    assert isinstance(api, Api) and httpx is not None
