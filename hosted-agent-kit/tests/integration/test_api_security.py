"""Authentication, RBAC, claim spoofing and log hygiene over HTTP."""

from __future__ import annotations

import logging
import time

import pytest

from tests.integration.conftest import OID, Api, bearer, start_api

INVOKE = "/v1/agents/coding-agent/invoke"
BODY = {"input": {"input": "hi"}}


async def test_missing_token_gets_401_with_challenge(entra_api: Api) -> None:
    response = await entra_api.client.post(INVOKE, json=BODY)
    assert response.status_code == 401
    assert response.json()["error_code"] == "AUTHENTICATION_REQUIRED"
    assert response.headers["www-authenticate"].startswith("Bearer")
    assert entra_api.fake.created == []


async def test_authentication_runs_before_body_validation(entra_api: Api) -> None:
    response = await entra_api.client.post(INVOKE, json={"wrong": 1})
    assert response.status_code == 401


@pytest.mark.parametrize(
    "headers",
    [
        {"Authorization": "Bearer not-a-token"},
        bearer(exp=int(time.time()) - 600),
        bearer(aud="api://other"),
        bearer(tid="00000000-0000-0000-0000-000000000000"),
    ],
)
async def test_invalid_tokens_get_401(entra_api: Api, headers: dict[str, str]) -> None:
    response = await entra_api.client.post(INVOKE, json=BODY, headers=headers)
    assert (
        response.status_code == 401
        and 'error="invalid_token"' in response.headers["www-authenticate"]
    )


async def test_invoke_role_cannot_use_admin_api_and_vice_versa(entra_api: Api) -> None:
    invoke_only = bearer(roles=["Pool.Invoke"])
    admin_only = bearer(roles=["Pool.Admin"])
    denied_admin = await entra_api.client.get("/v1/admin/agents", headers=invoke_only)
    assert (
        denied_admin.status_code == 403
        and denied_admin.json()["error_code"] == "INSUFFICIENT_SCOPE"
    )
    denied_invoke = await entra_api.client.post(INVOKE, json=BODY, headers=admin_only)
    assert denied_invoke.status_code == 403
    assert (await entra_api.client.get("/v1/admin/agents", headers=admin_only)).status_code == 200
    assert entra_api.fake.created == []


async def test_forbidden_response_reveals_nothing_about_resources(entra_api: Api) -> None:
    response = await entra_api.client.post(
        "/v1/agents/ghost/invoke", json=BODY, headers=bearer(roles=["Pool.Admin"])
    )
    assert response.status_code == 403  # no 404 oracle for unauthorised callers


async def test_user_id_is_taken_from_the_oid_claim(entra_api: Api) -> None:
    await entra_api.client.post(INVOKE, json=BODY, headers=bearer())
    await entra_api.client.post(INVOKE, json=BODY, headers=bearer())
    await entra_api.client.post(
        INVOKE, json=BODY, headers=bearer(oid="aaaaaaaa-0000-0000-0000-000000000002")
    )
    sessions = [c.session_id for c in entra_api.fake.invocations]
    assert sessions[0] == sessions[1] != sessions[2]
    record = (await entra_api.container.pool.list_sessions("coding-agent"))[0]
    assert record.affinity_key is not None and record.affinity_key.user_id == OID


async def test_body_user_id_spoofing_is_rejected_in_entra_mode(entra_api: Api) -> None:
    response = await entra_api.client.post(
        INVOKE, json={**BODY, "user_id": "victim"}, headers=bearer()
    )
    assert response.status_code == 422 and "user_id" in response.json()["detail"]
    chat = await entra_api.client.post(
        "/v1/agents/coding-agent/chat", json={"message": "x", "user_id": "victim"}, headers=bearer()
    )
    assert chat.status_code == 422
    assert entra_api.fake.created == []


async def test_dev_header_is_ignored_in_entra_mode(entra_api: Api) -> None:
    response = await entra_api.client.post(INVOKE, json=BODY, headers={"X-Dev-User-Id": "attacker"})
    assert response.status_code == 401


async def test_development_mode_accepts_body_user_id_only_when_well_formed(api: Api) -> None:
    ok = await api.client.post(INVOKE, json={**BODY, "user_id": "alice"})
    assert ok.status_code == 200
    record = (await api.container.pool.list_sessions("coding-agent"))[0]
    assert record.affinity_key is not None and record.affinity_key.user_id == "alice"
    bad = await api.client.post(INVOKE, json={**BODY, "user_id": "../x"})
    assert bad.status_code == 422


async def test_tokens_and_content_never_appear_in_logs(
    entra_api: Api, caplog: pytest.LogCaptureFixture
) -> None:
    caplog.set_level(logging.DEBUG, logger="hosted_agent_kit")
    headers = bearer()
    secret_token = headers["Authorization"].split(" ", 1)[1]
    await entra_api.client.post(
        INVOKE, json={"input": {"input": "TOP-SECRET-PROMPT"}}, headers=headers
    )
    await entra_api.client.post(
        INVOKE, json=BODY, headers={"Authorization": "Bearer garbage.token.value"}
    )
    logged = caplog.text + "".join(str(getattr(r, "fields", "")) for r in caplog.records)
    assert secret_token not in logged and "TOP-SECRET-PROMPT" not in logged
    assert entra_api.fake.created[0] not in logged  # session ids appear only hashed
    assert OID not in logged


async def test_diagnostic_session_header_requires_option_and_role() -> None:
    api, manager = await start_api(entra=True, diagnostic_session_id_header=True)
    try:
        plain = await api.client.post(INVOKE, json=BODY, headers=bearer())
        assert "x-pool-session-id" not in plain.headers
        diag = await api.client.post(
            INVOKE, json=BODY, headers=bearer(roles=["Pool.Invoke", "Pool.Diagnostics"])
        )
        assert (
            diag.headers["x-pool-session-id"] == api.fake.created[-1]
            or diag.headers["x-pool-session-id"] in api.fake.created
        )
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_health_endpoints_need_no_credentials(entra_api: Api) -> None:
    assert (await entra_api.client.get("/health/live")).status_code == 200
    assert (await entra_api.client.get("/health/ready")).status_code == 200
