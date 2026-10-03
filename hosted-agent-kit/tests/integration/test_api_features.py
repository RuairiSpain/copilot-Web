"""Protocol endpoints, discovery, conversations, app-only callers, idempotency and error detail."""

from __future__ import annotations

import asyncio
import base64
from collections.abc import AsyncIterator
from typing import Any

import pytest

from hosted_agent_kit.domain.errors import FoundryTimeout, FoundryUnavailable
from hosted_agent_kit.ports.foundry import UpstreamResponse
from tests.conftest import settle
from tests.fakes.clock import FakeClock
from tests.integration.conftest import Api, bearer, start_api

AGENTS: dict[str, dict[str, Any]] = {
    "chat": {"mode": "stateful", "max_sessions": 4},
    "pool": {"mode": "stateless", "max_sessions": 2},
    "inv": {"mode": "stateless", "protocol": "invocations", "max_sessions": 2},
    "inv-state": {"mode": "stateful", "protocol": "invocations", "max_sessions": 3},
}
INPUT = {"input": {"input": "hi"}}


@pytest.fixture
async def api() -> AsyncIterator[Api]:
    instance, manager = await start_api(agents=AGENTS, defaults={})
    try:
        yield instance
    finally:
        await instance.client.aclose()
        await manager.__aexit__(None, None, None)


def raw(body: bytes, media: str = "application/json", status: int = 200) -> Any:
    return lambda _ctx: UpstreamResponse(status_code=status, raw=body, media_type=media)


# ------------------------------------------------------------ protocol endpoints


async def test_responses_endpoint_returns_the_envelope(api: Api) -> None:
    response = await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=api.headers())
    body = response.json()
    assert response.status_code == 200 and body["request_id"].startswith("req_")
    assert body["result"]["echo"] == INPUT["input"]
    assert "deprecation" not in response.headers


async def test_responses_endpoint_streams_with_an_explicit_flag(api: Api) -> None:
    response = await api.client.post(
        "/v1/agents/pool/responses", json={**INPUT, "stream": True}, headers=api.headers()
    )
    assert response.headers["content-type"].startswith("text/event-stream")
    assert "event: a" in response.text and "event: b" in response.text
    assert api.fake.invocations[-1].stream is True


async def test_each_protocol_endpoint_refuses_the_other_protocol(api: Api) -> None:
    wrong_a = await api.client.post("/v1/agents/inv/responses", json=INPUT, headers=api.headers())
    wrong_b = await api.client.post(
        "/v1/agents/pool/invocations", content=b"x", headers=api.headers()
    )
    for response, expect in (
        (wrong_a, "/v1/agents/inv/invocations"),
        (wrong_b, "/v1/agents/pool/responses"),
    ):
        assert response.status_code == 422 and response.json()["error_code"] == "VALIDATION_ERROR"
        assert expect in response.json()["detail"]
    assert api.fake.created == []


async def test_invocations_accepts_any_content_type_and_sends_it_unchanged(api: Api) -> None:
    api.fake.invoke_handler = raw(b"ok", "text/plain")
    cases = [
        (b'[1,2,{"a":3}]', "application/json"),  # a JSON array, not an object
        (b'"just a string"', "application/json"),
        (b"plain text body", "text/plain; charset=utf-8"),
        (b"\x00\x01\x02\xff", "application/octet-stream"),
        (
            b"--b\r\nContent-Disposition: form-data; name=x\r\n\r\n1\r\n--b--\r\n",
            "multipart/form-data; boundary=b",
        ),
        (b"", "application/json"),
    ]
    for body, content_type in cases:
        response = await api.client.post(
            "/v1/agents/inv/invocations",
            content=body,
            headers=api.headers(**{"Content-Type": content_type}),
        )
        assert response.status_code == 200 and response.text == "ok"
        context = api.fake.invocations[-1]
        assert context.raw_body == body and context.content_type == content_type


async def test_invocations_envelope_wraps_json_text_and_binary_responses(api: Api) -> None:
    url = "/v1/agents/inv/invocations?envelope=true"
    api.fake.invoke_handler = raw(b'{"answer":42}', "application/json")
    body = (await api.client.post(url, content=b"{}", headers=api.headers())).json()
    assert body["body_json"] == {"answer": 42} and body["status_code"] == 200
    assert body["request_id"].startswith("req_") and body["body_text"] is None

    api.fake.invoke_handler = raw("héllo".encode(), "text/plain; charset=utf-8", status=202)
    body = (await api.client.post(url, content=b"{}", headers=api.headers())).json()
    assert body["body_text"] == "héllo" and body["status_code"] == 202

    api.fake.invoke_handler = raw(b"\x00\xff", "application/octet-stream")
    body = (await api.client.post(url, content=b"{}", headers=api.headers())).json()
    assert base64.b64decode(body["body_base64"]) == b"\x00\xff" and body["body_json"] is None

    api.fake.invoke_handler = raw(b"{not json", "application/json")
    body = (await api.client.post(url, content=b"{}", headers=api.headers())).json()
    assert base64.b64decode(body["body_base64"]) == b"{not json"


async def test_invoke_still_works_and_is_marked_deprecated(api: Api) -> None:
    response = await api.client.post("/v1/agents/pool/invoke", json=INPUT, headers=api.headers())
    assert response.status_code == 200
    assert response.headers["deprecation"] == "true"
    assert response.headers["link"] == '</v1/agents/pool/responses>; rel="successor-version"'
    api.fake.invoke_handler = raw(b"{}")
    inv = await api.client.post("/v1/agents/inv/invoke", json=INPUT, headers=api.headers())
    assert "/v1/agents/inv/invocations" in inv.headers["link"]


# ---------------------------------------------------------------------- discovery


async def test_agents_can_be_listed_and_described_without_admin_access(api: Api) -> None:
    listing = (await api.client.get("/v1/agents", headers=api.headers())).json()
    assert [a["name"] for a in listing] == list(AGENTS)
    one = (await api.client.get("/v1/agents/inv-state", headers=api.headers())).json()
    assert one["protocol"] == "invocations" and one["stateful"] is True
    assert one["streaming"] == "agent" and one["supports_conversation_key"] is True
    assert one["endpoints"] == ["/v1/agents/inv-state/invocations"]
    assert one["limits"]["max_body_bytes"] == api.settings.max_body_bytes
    assert one["limits"]["max_response_bytes"] == api.settings.max_response_bytes
    chat = (await api.client.get("/v1/agents/chat", headers=api.headers())).json()
    assert chat["streaming"] == "request" and chat["limits"]["max_response_bytes"] is None
    assert chat["endpoints"] == ["/v1/agents/chat/responses", "/v1/agents/chat/chat"]
    assert chat["idempotency_ttl_seconds"] == 600 and chat["agent_version"] is None
    pool = (await api.client.get("/v1/agents/pool", headers=api.headers())).json()
    assert pool["supports_conversation_key"] is False and pool["queue_enabled"] is True
    missing = await api.client.get("/v1/agents/ghost", headers=api.headers())
    assert missing.status_code == 404 and missing.json()["error_code"] == "AGENT_NOT_CONFIGURED"


async def test_discovery_requires_the_invoke_role() -> None:
    api, manager = await start_api(agents=AGENTS, defaults={}, entra=True)
    try:
        assert (await api.client.get("/v1/agents")).status_code == 401
        denied = await api.client.get("/v1/agents", headers=bearer(roles=["Other"]))
        assert denied.status_code == 403
        assert (await api.client.get("/v1/agents", headers=bearer())).status_code == 200
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


# ----------------------------------------------------------------- conversations


async def test_each_conversation_key_gets_its_own_session(api: Api) -> None:
    async def call(key: str | None) -> None:
        headers = api.headers("u1", **({"X-Conversation-Key": key} if key else {}))
        response = await api.client.post("/v1/agents/chat/responses", json=INPUT, headers=headers)
        assert response.status_code == 200

    await call("work")
    await call("home")
    await call("work")  # reuses the first
    await call(None)  # the user's default conversation is a third
    await call(None)
    assert len(api.fake.created) == 3
    used = [c.session_id for c in api.fake.invocations]
    assert used[0] == used[2] and used[3] == used[4] and len({used[0], used[1], used[3]}) == 3


async def test_the_conversation_key_can_be_in_the_body_and_must_agree_with_the_header(
    api: Api,
) -> None:
    body = {**INPUT, "conversation_key": "k1"}
    ok = await api.client.post(
        "/v1/agents/chat/responses", json=body, headers=api.headers(**{"X-Conversation-Key": "k1"})
    )
    assert ok.status_code == 200
    clash = await api.client.post(
        "/v1/agents/chat/responses", json=body, headers=api.headers(**{"X-Conversation-Key": "k2"})
    )
    assert clash.status_code == 422 and "differ" in clash.json()["detail"]
    chat = await api.client.post(
        "/v1/agents/chat/chat",
        json={"message": "hi", "conversation_key": "k1"},
        headers=api.headers(),
    )
    assert chat.status_code == 200 and len(api.fake.created) == 1


async def test_a_conversation_key_needs_a_stateful_agent_and_a_valid_format(api: Api) -> None:
    stateless = await api.client.post(
        "/v1/agents/pool/responses", json=INPUT, headers=api.headers(**{"X-Conversation-Key": "k"})
    )
    assert stateless.status_code == 422 and "stateful" in stateless.json()["detail"]
    for bad in ("has space", "x" * 129, "a/b"):
        response = await api.client.post(
            "/v1/agents/chat/responses",
            json={**INPUT, "conversation_key": bad},
            headers=api.headers(),
        )
        assert response.status_code == 422


async def test_conversations_are_scoped_to_the_user(api: Api) -> None:
    for user in ("u1", "u2"):
        headers = api.headers(user, **{"X-Conversation-Key": "shared-name"})
        await api.client.post("/v1/agents/chat/responses", json=INPUT, headers=headers)
    assert len(api.fake.created) == 2


async def test_invocations_agents_support_conversations_too(api: Api) -> None:
    api.fake.invoke_handler = raw(b"{}")
    for key in ("a", "b", "a"):
        await api.client.post(
            "/v1/agents/inv-state/invocations",
            content=b"{}",
            headers=api.headers(**{"X-Conversation-Key": key}),
        )
    assert len(api.fake.created) == 2


async def test_admin_views_show_the_conversation_redacted() -> None:
    api, manager = await start_api(agents=AGENTS, defaults={}, entra=True)
    try:
        await api.client.post(
            "/v1/agents/chat/responses",
            json={**INPUT, "conversation_key": "secret-topic"},
            headers=bearer(),
        )
        plain = bearer(roles=["Pool.Admin"])
        (view,) = (await api.client.get("/v1/admin/agents/chat/sessions", headers=plain)).json()
        assert view["conversation"].startswith("c_") and "secret-topic" not in str(view)
        diag = bearer(roles=["Pool.Admin", "Pool.Diagnostics"])
        (view,) = (await api.client.get("/v1/admin/agents/chat/sessions", headers=diag)).json()
        assert view["conversation"] == "secret-topic"
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


# ------------------------------------------------------------------ app-only callers


@pytest.fixture
async def entra() -> AsyncIterator[Api]:
    instance, manager = await start_api(agents=AGENTS, defaults={}, entra=True)
    try:
        yield instance
    finally:
        await instance.client.aclose()
        await manager.__aexit__(None, None, None)


def app_only(*roles: str, oid: str = "bbbbbbbb-0000-0000-0000-000000000001") -> dict[str, str]:
    return bearer(scp=None, oid=oid, roles=["Pool.Invoke", *roles])


async def test_an_app_only_caller_cannot_use_a_stateful_agent_without_naming_the_user(
    entra: Api,
) -> None:
    response = await entra.client.post("/v1/agents/chat/responses", json=INPUT, headers=app_only())
    problem = response.json()
    assert response.status_code == 403 and problem["error_code"] == "END_USER_REQUIRED"
    assert problem["phase"] == "auth" and entra.fake.created == []


async def test_an_app_only_caller_may_use_a_stateless_agent(entra: Api) -> None:
    response = await entra.client.post("/v1/agents/pool/responses", json=INPUT, headers=app_only())
    assert response.status_code == 200


async def test_a_delegate_names_the_end_user_and_each_user_gets_a_session(entra: Api) -> None:
    for subject in ("alice", "bob", "alice"):
        response = await entra.client.post(
            "/v1/agents/chat/responses",
            json=INPUT,
            headers={**app_only("Pool.Delegate"), "X-Pool-Subject": subject},
        )
        assert response.status_code == 200
    assert len(entra.fake.created) == 2
    used = [c.session_id for c in entra.fake.invocations]
    assert used[0] == used[2] and used[0] != used[1]


async def test_subjects_are_scoped_to_the_calling_service(entra: Api) -> None:
    headers = {"X-Pool-Subject": "alice"}
    for oid in ("bbbbbbbb-0000-0000-0000-000000000001", "bbbbbbbb-0000-0000-0000-000000000002"):
        await entra.client.post(
            "/v1/agents/chat/responses",
            json=INPUT,
            headers={**app_only("Pool.Delegate", oid=oid), **headers},
        )
    assert len(entra.fake.created) == 2  # one service cannot reach another's "alice"


async def test_naming_a_subject_needs_the_delegate_role_and_an_app_only_token(entra: Api) -> None:
    no_role = await entra.client.post(
        "/v1/agents/chat/responses", json=INPUT, headers={**app_only(), "X-Pool-Subject": "alice"}
    )
    assert no_role.status_code == 403 and no_role.json()["error_code"] == "INSUFFICIENT_SCOPE"
    delegated = await entra.client.post(
        "/v1/agents/chat/responses", json=INPUT, headers={**bearer(), "X-Pool-Subject": "alice"}
    )
    assert delegated.status_code == 422 and "app-only" in delegated.json()["detail"]
    bad = await entra.client.post(
        "/v1/agents/chat/responses",
        json=INPUT,
        headers={**app_only("Pool.Delegate"), "X-Pool-Subject": "not valid!"},
    )
    assert bad.status_code == 422
    assert entra.fake.created == []


async def test_a_delegated_user_token_needs_no_subject(entra: Api) -> None:
    response = await entra.client.post("/v1/agents/chat/responses", json=INPUT, headers=bearer())
    assert response.status_code == 200


async def test_the_allow_policy_keeps_the_service_principal_as_the_user() -> None:
    api, manager = await start_api(agents=AGENTS, defaults={}, entra=True, app_only_policy="allow")
    try:
        response = await api.client.post(
            "/v1/agents/chat/responses", json=INPUT, headers=app_only()
        )
        assert response.status_code == 200
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


# --------------------------------------------------------------------- idempotency


async def test_a_repeated_key_replays_the_first_response_without_calling_the_agent(
    api: Api,
) -> None:
    headers = api.headers(**{"Idempotency-Key": "abc"})
    first = await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=headers)
    again = await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=headers)
    assert first.status_code == again.status_code == 200
    assert again.content == first.content and again.headers["idempotent-replayed"] == "true"
    assert "idempotent-replayed" not in first.headers
    assert len(api.fake.invocations) == 1


async def test_invocations_responses_are_replayed_with_their_content_type(api: Api) -> None:
    api.fake.invoke_handler = raw(b"\x01\x02", "application/octet-stream", status=201)
    headers = api.headers(**{"Idempotency-Key": "k", "Content-Type": "application/x-bin"})
    first = await api.client.post("/v1/agents/inv/invocations", content=b"payload", headers=headers)
    again = await api.client.post("/v1/agents/inv/invocations", content=b"payload", headers=headers)
    assert again.status_code == 201 and again.content == b"\x01\x02"
    assert again.headers["content-type"] == "application/octet-stream"
    assert first.content == again.content and len(api.fake.invocations) == 1


async def test_the_same_key_with_a_different_request_is_refused(api: Api) -> None:
    headers = api.headers(**{"Idempotency-Key": "abc"})
    await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=headers)
    other = await api.client.post(
        "/v1/agents/pool/responses", json={"input": {"input": "different"}}, headers=headers
    )
    problem = other.json()
    assert other.status_code == 422 and problem["error_code"] == "IDEMPOTENCY_KEY_REUSED"
    assert problem["retry_safe"] is True and len(api.fake.invocations) == 1
    # A different endpoint with the same payload is a different request too.
    chat = await api.client.post("/v1/agents/pool/chat", json={"message": "hi"}, headers=headers)
    assert chat.status_code == 422


async def test_a_duplicate_while_the_first_is_running_gets_409_and_retry_after(api: Api) -> None:
    api.fake.invoke_gate = asyncio.Event()
    headers = api.headers(**{"Idempotency-Key": "slow"})
    first = asyncio.create_task(
        api.client.post("/v1/agents/pool/responses", json=INPUT, headers=headers)
    )
    await settle(30)
    duplicate = await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=headers)
    assert duplicate.status_code == 409
    assert duplicate.json()["error_code"] == "IDEMPOTENCY_IN_PROGRESS"
    assert duplicate.headers["retry-after"] == str(api.settings.retry_after_seconds)
    api.fake.invoke_gate.set()
    assert (await first).status_code == 200
    replay = await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=headers)
    assert replay.headers["idempotent-replayed"] == "true" and len(api.fake.invocations) == 1


async def test_a_failed_request_is_not_remembered_so_the_retry_runs(api: Api) -> None:
    headers = api.headers(**{"Idempotency-Key": "flaky"})
    api.fake.invoke_errors = [FoundryUnavailable("down")]
    failed = await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=headers)
    assert failed.status_code == 503
    retry = await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=headers)
    assert retry.status_code == 200 and "idempotent-replayed" not in retry.headers
    assert len(api.fake.invocations) == 2


async def test_keys_are_scoped_to_the_user_and_the_agent(api: Api) -> None:
    for user in ("u1", "u2"):
        headers = api.headers(user, **{"Idempotency-Key": "same"})
        response = await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=headers)
        assert "idempotent-replayed" not in response.headers
    assert len(api.fake.invocations) == 2


async def test_streams_are_not_deduplicated(api: Api) -> None:
    headers = api.headers(**{"Idempotency-Key": "s"})
    body = {**INPUT, "stream": True}
    for _ in range(2):
        response = await api.client.post("/v1/agents/pool/responses", json=body, headers=headers)
        assert response.headers["content-type"].startswith("text/event-stream")
        assert "idempotent-replayed" not in response.headers
    assert len(api.fake.invocations) == 2


async def test_a_stored_response_expires() -> None:
    clock = FakeClock()
    api, manager = await start_api(
        agents=AGENTS, defaults={}, clock=clock, idempotency_ttl_seconds=60
    )
    try:
        headers = api.headers(**{"Idempotency-Key": "t"})
        await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=headers)
        clock.advance(61)
        again = await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=headers)
        assert "idempotent-replayed" not in again.headers and len(api.fake.invocations) == 2
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_deduplication_can_be_turned_off() -> None:
    api, manager = await start_api(agents=AGENTS, defaults={}, idempotency_ttl_seconds=0)
    try:
        headers = api.headers(**{"Idempotency-Key": "k"})
        for _ in range(2):
            await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=headers)
        assert len(api.fake.invocations) == 2
        info = (await api.client.get("/v1/agents/pool", headers=api.headers())).json()
        assert info["idempotency_ttl_seconds"] == 0
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


# ------------------------------------------------------------------- error detail


async def test_problem_responses_say_where_the_request_failed_and_whether_to_retry(
    api: Api,
) -> None:
    api.fake.invoke_errors = [FoundryTimeout()]
    timeout = (
        await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=api.headers())
    ).json()
    assert timeout["error_code"] == "FOUNDRY_TIMEOUT"
    assert timeout["phase"] == "invoke" and timeout["retry_safe"] is False
    assert timeout["request_id"].startswith("req_") and timeout["correlation_id"]

    api.fake.create_errors = [FoundryUnavailable("x")] * 10
    created = (
        await api.client.post("/v1/agents/chat/responses", json=INPUT, headers=api.headers("u2"))
    ).json()
    assert created["phase"] == "create" and created["retry_safe"] is True

    missing = (
        await api.client.post("/v1/agents/ghost/responses", json=INPUT, headers=api.headers())
    ).json()
    assert missing["phase"] == "request" and missing["retry_safe"] is True
    invalid = (
        await api.client.post("/v1/agents/pool/responses", json={}, headers=api.headers())
    ).json()
    assert invalid["error_code"] == "VALIDATION_ERROR" and invalid["retry_safe"] is True
    assert invalid["request_id"].startswith("req_")


async def test_a_full_queue_is_a_queue_phase_error_that_is_safe_to_retry() -> None:
    api, manager = await start_api(
        agents={"a": {"mode": "stateless", "max_sessions": 1, "queue": {"enabled": False}}},
        defaults={},
    )
    try:
        api.fake.invoke_gate = asyncio.Event()
        first = asyncio.create_task(
            api.client.post("/v1/agents/a/responses", json=INPUT, headers=api.headers())
        )
        await settle(30)
        full = (
            await api.client.post("/v1/agents/a/responses", json=INPUT, headers=api.headers("u2"))
        ).json()
        assert full["error_code"] == "POOL_CAPACITY_EXCEEDED"
        assert full["phase"] == "queue" and full["retry_safe"] is True
        assert full["retry_after_seconds"] == api.settings.retry_after_seconds
        api.fake.invoke_gate.set()
        await first
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_a_stream_error_event_carries_the_phase_and_request_id(api: Api) -> None:
    api.fake.stream_error = FoundryUnavailable("reset")
    response = await api.client.post(
        "/v1/agents/pool/responses", json={**INPUT, "stream": True}, headers=api.headers()
    )
    import json as _json

    last = response.text.strip().split("event: error\ndata: ")[-1]
    event = _json.loads(last)
    assert event["phase"] == "stream" and event["retry_safe"] is False
    assert event["request_id"].startswith("req_") and event["correlation_id"]


async def test_an_oversized_subject_is_refused(entra: Api) -> None:
    long_oid = "b" * 100  # a valid oid, but oid + subject would not fit in a user id
    response = await entra.client.post(
        "/v1/agents/chat/responses",
        json=INPUT,
        headers={**app_only("Pool.Delegate", oid=long_oid), "X-Pool-Subject": "s" * 60},
    )
    assert response.status_code == 422 and "X-Pool-Subject" in response.json()["detail"]


async def test_an_envelope_for_text_that_is_not_valid_utf8_falls_back_to_base64(api: Api) -> None:
    api.fake.invoke_handler = raw(b"\xff\xfe", "text/plain")
    body = (
        await api.client.post(
            "/v1/agents/inv/invocations?envelope=true", content=b"{}", headers=api.headers()
        )
    ).json()
    assert base64.b64decode(body["body_base64"]) == b"\xff\xfe" and body["body_text"] is None
