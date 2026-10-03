"""Invocations-protocol agents, plus the admin and readiness view of restored sessions."""

from __future__ import annotations

import json
from collections.abc import AsyncIterator
from typing import Any

import pytest

from hosted_agent_kit.domain.enums import AgentProtocol
from hosted_agent_kit.domain.enums import FoundrySessionStatus as S
from hosted_agent_kit.domain.errors import FoundryUnavailable
from hosted_agent_kit.ports.foundry import UpstreamResponse
from hosted_agent_kit.services.session_ids import SessionIdDeriver
from tests.integration.conftest import Api, start_api

AGENTS: dict[str, dict[str, Any]] = {
    "inv": {"mode": "stateless", "protocol": "invocations"},
    "resp": {"mode": "stateless"},
}
INVOKE = "/v1/agents/inv/invoke"
PAYLOAD = {"input": {"task": "summarise", "stream": True, "items": [1, 2]}}


@pytest.fixture
async def inv() -> Any:
    api, manager = await start_api(agents=AGENTS, defaults={})
    yield api
    await api.client.aclose()
    await manager.__aexit__(None, None, None)


async def test_the_agents_response_is_returned_unchanged_with_its_own_content_type(
    inv: Api,
) -> None:
    inv.fake.invoke_handler = lambda ctx: UpstreamResponse(
        raw=b'{"answer":42}', media_type="application/json; charset=utf-8"
    )
    response = await inv.client.post(INVOKE, json=PAYLOAD, headers=inv.headers())
    assert response.status_code == 200 and response.content == b'{"answer":42}'
    assert response.headers["content-type"] == "application/json; charset=utf-8"
    assert (
        response.headers["x-request-id"].startswith("req_") and response.headers["x-correlation-id"]
    )
    context = inv.fake.invocations[-1]
    assert context.protocol is AgentProtocol.INVOCATIONS
    assert context.payload == PAYLOAD["input"]  # nothing removed, not even `stream`
    assert context.stream is False  # the stream flag is a Responses convention
    assert inv.fake.created[0] not in response.text


async def test_text_responses_and_upstream_status_codes_pass_through() -> None:
    api, manager = await start_api(agents=AGENTS, defaults={})
    try:
        api.fake.invoke_handler = lambda ctx: UpstreamResponse(
            status_code=202, raw=b"accepted", media_type="text/plain"
        )
        response = await api.client.post(INVOKE, json=PAYLOAD, headers=api.headers())
        assert response.status_code == 202 and response.text == "accepted"
        assert response.headers["content-type"].startswith("text/plain")
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_a_streaming_agent_is_relayed_even_without_a_stream_flag(inv: Api) -> None:
    async def frames() -> AsyncIterator[bytes]:
        yield b"data: one\n\n"
        yield b"data: two\n\n"

    inv.fake.invoke_handler = lambda ctx: UpstreamResponse(
        stream=frames(), media_type="text/event-stream"
    )
    async with inv.client.stream(
        "POST", INVOKE, json={"input": {"task": "go"}}, headers=inv.headers()
    ) as response:
        assert response.headers["content-type"].startswith("text/event-stream")
        body = "".join([chunk async for chunk in response.aiter_text()])
    assert body == "data: one\n\ndata: two\n\n"
    (record,) = await inv.container.pool.list_sessions("inv")
    assert record.lease_request_id is None


async def test_the_chat_endpoint_is_refused_for_an_invocations_agent(inv: Api) -> None:
    response = await inv.client.post(
        "/v1/agents/inv/chat", json={"message": "hi"}, headers=inv.headers()
    )
    problem = response.json()
    assert response.status_code == 422 and problem["error_code"] == "VALIDATION_ERROR"
    assert "invocations" in problem["detail"]
    assert inv.fake.created == [] and inv.fake.invocations == []


async def test_failures_map_to_the_usual_errors(inv: Api) -> None:
    inv.fake.invoke_errors = [FoundryUnavailable("x")]
    response = await inv.client.post(INVOKE, json=PAYLOAD, headers=inv.headers())
    assert response.status_code == 503 and response.json()["error_code"] == "FOUNDRY_UNAVAILABLE"


async def test_responses_agents_are_unaffected(inv: Api) -> None:
    response = await inv.client.post(
        "/v1/agents/resp/invoke", json={"input": {"input": "hi"}}, headers=inv.headers()
    )
    assert response.status_code == 200 and response.json()["result"]["status"] == "completed"
    assert inv.fake.invocations[-1].protocol is AgentProtocol.RESPONSES
    chat = await inv.client.post(
        "/v1/agents/resp/chat", json={"message": "hi"}, headers=inv.headers()
    )
    assert chat.status_code == 200
    async with inv.client.stream(
        "POST", "/v1/agents/resp/invoke", json={"input": {"stream": True}}, headers=inv.headers()
    ) as streamed:
        assert streamed.headers["content-type"].startswith("text/event-stream")
        _ = [c async for c in streamed.aiter_text()]
    assert inv.fake.invocations[-1].stream is True


# ------------------------------------------------------- restored sessions, admin


async def test_the_admin_view_marks_sessions_held_for_a_returning_user() -> None:
    key = "k" * 32
    deriver = SessionIdDeriver(key.encode())
    api, manager = await start_api(
        agents={"a": {"mode": "stateful", "max_sessions": 5}},
        defaults={},
        session_id_key=key,
    )
    try:
        mine = deriver.for_user("a", "alice")
        api.fake.add_session("a", S.ACTIVE, mine)
        api.fake.add_session("a", S.ACTIVE, "sess-foreign")
        await api.client.post("/v1/admin/agents/a/sync", headers=api.headers())
        sessions = (
            await api.client.get("/v1/admin/agents/a/sessions", headers=api.headers())
        ).json()
        assert [s["restorable"] for s in sessions] == [True] and sessions[0][
            "bound_to_user"
        ] is False
        reply = await api.client.post(
            "/v1/agents/a/invoke", json={"input": {"input": "hi"}}, headers=api.headers("alice")
        )
        assert reply.status_code == 200 and api.fake.created == []
        (session,) = (
            await api.client.get("/v1/admin/agents/a/sessions", headers=api.headers())
        ).json()
        assert session["restorable"] is False and session["bound_to_user"] is True
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_a_session_never_looks_restorable_without_a_key(inv: Api) -> None:
    await inv.client.post("/v1/agents/resp/invoke", json={"input": {}}, headers=inv.headers())
    sessions = (
        await inv.client.get("/v1/admin/agents/resp/sessions", headers=inv.headers())
    ).json()
    assert sessions and all(s["restorable"] is False for s in sessions)


async def test_readiness_waits_for_the_first_sync(inv: Api) -> None:
    assert (await inv.client.get("/health/ready")).json()["checks"]["initial_sync"] is True
    inv.container.reconciler._synced.clear()
    waiting = await inv.client.get("/health/ready")
    assert waiting.status_code == 503 and waiting.json()["checks"]["initial_sync"] is False
    await inv.container.reconciler.run_all()
    assert (await inv.client.get("/health/ready")).status_code == 200


async def test_the_openapi_description_names_both_protocols(inv: Api) -> None:
    spec = inv.app.openapi()
    description = spec["paths"]["/v1/agents/{agent_name}/invoke"]["post"]["description"]
    assert "Invocations" in description and "Responses" in description
    assert json.dumps(spec)  # serialisable
