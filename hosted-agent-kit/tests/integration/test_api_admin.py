"""Admin API, health and metrics endpoints."""

from __future__ import annotations

import asyncio
from typing import Any

import pytest

from hosted_agent_kit.domain.enums import FoundrySessionStatus
from hosted_agent_kit.domain.errors import FoundryUnavailable
from hosted_agent_kit.domain.models import AgentSummary
from tests.conftest import eventually
from tests.integration.conftest import Api, start_api

INVOKE = "/v1/agents/coding-agent/invoke"
BODY = {"input": {"input": "hi"}}
SESSIONS = "/v1/admin/agents/coding-agent/sessions"


async def seed(api: Api, user: str = "u1") -> str:
    await api.client.post(INVOKE, json=BODY, headers=api.headers(user))
    return api.fake.created[-1]


async def test_list_agents_reports_counts(api: Api) -> None:
    await seed(api)
    response = await api.client.get("/v1/admin/agents", headers=api.headers())
    by_name = {a["agent_name"]: a for a in response.json()}
    coding = by_name["coding-agent"]
    assert coding["mode"] == "stateful" and coding["sessions_total"] == 1
    assert coding["sessions_available"] == 1 and coding["sessions_leased"] == 0
    assert coding["queue_depth"] == 0 and coding["max_sessions"] == 3
    assert by_name["research-agent"]["sessions_total"] == 0


async def test_session_listing_redacts_user_ids_by_default() -> None:
    api, manager = await start_api(entra=True)
    try:
        from tests.integration.conftest import bearer

        user = "aaaaaaaa-bbbb-cccc-dddd-000000000001"
        await api.client.post(INVOKE, json=BODY, headers=bearer(oid=user))
        plain = bearer(roles=["Pool.Admin"])
        sessions = (
            await api.client.get("/v1/admin/agents/coding-agent/sessions", headers=plain)
        ).json()
        assert sessions[0]["user"].startswith("u_") and user not in str(sessions)
        assert sessions[0]["bound_to_user"] is True and sessions[0]["leased"] is False
        privileged = bearer(roles=["Pool.Admin", "Pool.Diagnostics"])
        detail = (
            await api.client.get(
                f"/v1/admin/agents/coding-agent/sessions/{api.fake.created[0]}", headers=privileged
            )
        ).json()
        assert detail["user"] == user
        redacted = (
            await api.client.get(
                f"/v1/admin/agents/coding-agent/sessions/{api.fake.created[0]}", headers=plain
            )
        ).json()
        assert redacted["user"].startswith("u_")
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_unbound_session_has_no_user(api: Api) -> None:
    api.fake.add_session("research-agent")
    await api.container.reconciler.run_agent("research-agent")
    (session,) = (
        await api.client.get("/v1/admin/agents/research-agent/sessions", headers=api.headers())
    ).json()
    assert session["user"] is None and session["bound_to_user"] is False


async def test_get_session_detail_and_not_found(api: Api) -> None:
    sid = await seed(api)
    detail = await api.client.get(f"{SESSIONS}/{sid}", headers=api.headers())
    assert detail.status_code == 200 and detail.json()["session_id"] == sid
    assert (
        detail.json()["platform_status"] == "active" and detail.json()["local_state"] == "available"
    )
    missing = await api.client.get(f"{SESSIONS}/nope", headers=api.headers())
    assert missing.status_code == 404 and missing.json()["error_code"] == "SESSION_NOT_FOUND"


async def test_unknown_agent_sessions_is_404(api: Api) -> None:
    response = await api.client.get("/v1/admin/agents/ghost/sessions", headers=api.headers())
    assert response.status_code == 404 and response.json()["error_code"] == "AGENT_NOT_CONFIGURED"


@pytest.mark.parametrize("session_id", ["a%2Fb", "a%20b", "x" * 300])
async def test_session_id_path_injection_is_rejected(api: Api, session_id: str) -> None:
    response = await api.client.get(f"{SESSIONS}/{session_id}", headers=api.headers())
    assert response.status_code in (404, 422)


async def test_delete_session_removes_it_in_foundry_and_locally(api: Api) -> None:
    sid = await seed(api)
    response = await api.client.delete(
        f"/v1/admin/agents/coding-agent/sessions/{sid}", headers=api.headers()
    )
    assert response.status_code == 204 and response.content == b""
    assert api.fake.deleted == [sid]
    assert await api.container.pool.list_sessions("coding-agent") == []
    assert await api.container.pool._affinity.count("coding-agent") == 0


async def test_delete_validates_agent_and_session(api: Api) -> None:
    sid = await seed(api)
    wrong_agent = await api.client.delete(
        f"/v1/admin/agents/research-agent/sessions/{sid}", headers=api.headers()
    )
    assert wrong_agent.status_code == 404
    unknown = await api.client.delete(
        "/v1/admin/agents/coding-agent/sessions/nope", headers=api.headers()
    )
    assert unknown.status_code == 404
    ghost = await api.client.delete(f"/v1/admin/agents/ghost/sessions/{sid}", headers=api.headers())
    assert ghost.status_code == 404 and api.fake.deleted == []


async def test_delete_of_a_leased_session_is_deferred_until_release(api: Api) -> None:
    api.fake.invoke_gate = asyncio.Event()
    request = asyncio.create_task(api.client.post(INVOKE, json=BODY, headers=api.headers()))

    async def leased() -> bool:
        records = await api.container.pool.list_sessions("coding-agent")
        return bool(records) and records[0].lease_request_id is not None

    await eventually(leased)
    sid = api.fake.created[0]
    response = await api.client.delete(
        f"/v1/admin/agents/coding-agent/sessions/{sid}", headers=api.headers()
    )
    assert response.status_code == 202 and response.json()["status"] == "deferred"
    assert api.fake.deleted == []
    api.fake.invoke_gate.set()
    assert (await request).status_code == 200

    async def gone() -> bool:
        return await api.container.pool.list_sessions("coding-agent") == []

    await eventually(gone)
    assert api.fake.deleted == [sid]


async def test_delete_failure_reports_502_and_keeps_the_record_retiring(api: Api) -> None:
    sid = await seed(api)
    api.fake.delete_errors = [FoundryUnavailable("x")] * 5
    response = await api.client.delete(
        f"/v1/admin/agents/coding-agent/sessions/{sid}", headers=api.headers()
    )
    assert response.status_code == 502 and response.json()["error_code"] == "UPSTREAM_ERROR"
    (record,) = await api.container.pool.list_sessions("coding-agent")
    assert record.local_state.value == "retiring"


async def test_manual_sync_returns_a_report_and_applies_changes(api: Api) -> None:
    sid = await seed(api)
    api.fake.set_status(sid, FoundrySessionStatus.FAILED)
    response = await api.client.post("/v1/admin/agents/coding-agent/sync", headers=api.headers())
    report = response.json()
    assert response.status_code == 200 and report["failed_sessions"] == 1 and report["removed"] == 1
    assert report["outcome"] == "success" and report["agent_name"] == "coding-agent"
    assert api.fake.deleted == [sid]


async def test_manual_sync_report_explains_an_incomplete_listing(api: Api) -> None:
    from hosted_agent_kit.domain.errors import FoundryUnavailable

    await seed(api)
    api.fake.list_fail_after = 0
    api.fake.list_error = FoundryUnavailable("page failed")
    report = (
        await api.client.post("/v1/admin/agents/coding-agent/sync", headers=api.headers())
    ).json()
    assert report["outcome"] == "incomplete" and report["errors"] == ["FoundryUnavailable"]


async def test_overlapping_manual_sync_returns_409(api: Api) -> None:
    gate = asyncio.Event()
    original = api.fake.list_sessions

    async def slow(agent: str) -> Any:
        await gate.wait()
        async for item in original(agent):
            yield item

    api.fake.list_sessions = slow  # type: ignore[method-assign]
    first = asyncio.create_task(
        api.client.post("/v1/admin/agents/coding-agent/sync", headers=api.headers())
    )
    await eventually(lambda: _locked(api))
    second = await api.client.post("/v1/admin/agents/coding-agent/sync", headers=api.headers())
    assert second.status_code == 409 and second.json()["error_code"] == "SYNC_IN_PROGRESS"
    gate.set()
    assert (await first).status_code == 200


async def _locked(api: Api) -> bool:
    return api.container.reconciler._mutexes["coding-agent"].locked()


async def test_sync_unknown_agent_is_404(api: Api) -> None:
    response = await api.client.post("/v1/admin/agents/ghost/sync", headers=api.headers())
    assert response.status_code == 404


async def test_metrics_snapshot_json(api: Api) -> None:
    await seed(api)
    response = await api.client.get("/v1/admin/metrics", headers=api.headers())
    names = {x["name"] for x in response.json()["agents"]["coding-agent"]["counters"]}
    assert {"pool_requests_total", "pool_sessions_created_total"} <= names


async def test_prometheus_endpoint_is_absent_unless_configured(api: Api) -> None:
    assert (await api.client.get("/metrics", headers=api.headers())).status_code == 404


async def test_prometheus_endpoint_when_enabled_requires_admin_role() -> None:
    api, manager = await start_api(entra=True, metrics_endpoint_enabled=True)
    try:
        from tests.integration.conftest import bearer

        assert (await api.client.get("/metrics")).status_code == 401
        assert (
            await api.client.get("/metrics", headers=bearer(roles=["Pool.Invoke"]))
        ).status_code == 403
        await api.client.post(INVOKE, json=BODY, headers=bearer())
        ok = await api.client.get("/metrics", headers=bearer(roles=["Pool.Admin"]))
        assert ok.status_code == 200 and ok.headers["content-type"].startswith("text/plain")
        assert 'pool_requests_total{agent="coding-agent",outcome="ok"} 1' in ok.text
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


# ------------------------------------------------------------------- health


async def test_liveness_and_readiness(api: Api) -> None:
    assert (await api.client.get("/health/live")).json() == {"status": "alive"}
    ready = await api.client.get("/health/ready")
    assert ready.status_code == 200
    assert ready.json() == {
        "status": "ready",
        "checks": {
            "configuration": True,
            "identity": True,
            "workers": True,
            "initial_sync": True,
        },
    }


async def test_not_ready_until_started(api: Api) -> None:
    api.container.ready = False
    response = await api.client.get("/health/ready")
    assert response.status_code == 503 and response.json()["status"] == "not_ready"
    assert response.json()["checks"]["identity"] is False


async def test_not_ready_without_workers(api: Api) -> None:
    await api.container.reconciler.stop()
    response = await api.client.get("/health/ready")
    assert response.status_code == 503 and response.json()["checks"]["workers"] is False
    await api.container.reconciler.start()


async def test_optional_foundry_probe() -> None:
    api, manager = await start_api(readiness_probe_foundry=True)
    try:
        assert (await api.client.get("/health/ready")).json()["checks"]["foundry"] is True
        api.fake.agents = [AgentSummary(name="coding-agent", state="enabled")]
        assert (await api.client.get("/health/ready")).json()["checks"]["foundry"] is True
        api.fake.list_agents_error = FoundryUnavailable("down")
        failing = await api.client.get("/health/ready")
        assert failing.status_code == 503 and failing.json()["checks"]["foundry"] is False
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_foundry_probe_timeout(monkeypatch: pytest.MonkeyPatch) -> None:
    from hosted_agent_kit.service.api import health

    monkeypatch.setattr(health, "PROBE_TIMEOUT_SECONDS", 0.05)
    api, manager = await start_api(readiness_probe_foundry=True)
    try:

        async def hang() -> Any:
            await asyncio.sleep(10)
            yield

        api.fake.list_agents = hang  # type: ignore[method-assign]
        response = await api.client.get("/health/ready")
        assert response.status_code == 503 and response.json()["checks"]["foundry"] is False
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)
