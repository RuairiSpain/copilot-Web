"""The control plane through the admin API: resources, conditions, events, reload and shutdown."""

from __future__ import annotations

import asyncio
from collections.abc import AsyncIterator
from typing import Any

import pytest

from hosted_agent_kit.domain.errors import FoundryRejected
from hosted_agent_kit.services.session_ids import SessionIdDeriver
from tests.conftest import eventually, make_config, make_request, settle
from tests.integration.conftest import Api, bearer, start_api

AGENTS: dict[str, dict[str, Any]] = {
    "chat": {"mode": "stateful", "max_sessions": 3},
    "pool": {"mode": "stateless", "max_sessions": 2},
}
INPUT = {"input": {"input": "hi"}}
ADMIN = "/v1/admin"


@pytest.fixture
async def api() -> AsyncIterator[Api]:
    instance, manager = await start_api(agents=AGENTS, defaults={})
    try:
        yield instance
    finally:
        await instance.client.aclose()
        await manager.__aexit__(None, None, None)


def conditions(body: dict[str, Any]) -> dict[str, dict[str, Any]]:
    return {c["type"]: c for c in body["conditions"]}


async def settled(api: Api) -> None:
    """Let the controllers finish what the last change queued."""
    await api.container.reconciler.manager.drain()


# ------------------------------------------------------------------ pool resources


async def test_the_agent_list_shows_generation_and_conditions(api: Api) -> None:
    listing = (await api.client.get(f"{ADMIN}/agents", headers=api.headers())).json()
    by_name = {a["agent_name"]: a for a in listing}
    pool = by_name["pool"]
    assert pool["generation"] == 1 and pool["observed_generation"] == 1
    states = conditions(pool)
    assert states["Ready"]["status"] == "True" and states["InitialSyncComplete"]["status"] == "True"
    assert states["FoundryReachable"]["status"] == "True"
    assert states["CircuitOpen"]["status"] == "False"
    assert states["Ready"]["last_transition_time"]


async def test_one_pool_is_described_as_spec_and_status_with_recent_events(api: Api) -> None:
    await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=api.headers())
    await settled(api)
    body = (await api.client.get(f"{ADMIN}/agents/pool", headers=api.headers())).json()
    assert body["agent_name"] == "pool" and body["generation"] == body["observed_generation"] == 1
    assert body["spec"]["max_sessions"] == 2 and body["spec"]["mode"] == "stateless"
    assert body["ready_sessions"] == 1 and body["leased_sessions"] == 0
    assert body["queued_requests"] == 0 and body["resource_version"] >= 1
    assert any(
        e["reason"] == "Created" and e["object"].startswith("session/") for e in body["events"]
    )
    assert all("sess-" not in e["object"] for e in body["events"])  # ids are hashed
    missing = await api.client.get(f"{ADMIN}/agents/ghost", headers=api.headers())
    assert missing.status_code == 404 and missing.json()["error_code"] == "AGENT_NOT_CONFIGURED"


async def test_events_can_be_filtered_and_limited(api: Api) -> None:
    await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=api.headers())
    await api.client.post("/v1/agents/chat/responses", json=INPUT, headers=api.headers())
    everything = (await api.client.get(f"{ADMIN}/events", headers=api.headers())).json()
    assert {e["agent_name"] for e in everything} == {"pool", "chat"}
    only = (await api.client.get(f"{ADMIN}/events?agent_name=chat", headers=api.headers())).json()
    assert only and {e["agent_name"] for e in only} == {"chat"}
    limited = (await api.client.get(f"{ADMIN}/events?limit=1", headers=api.headers())).json()
    assert len(limited) == 1
    unknown = await api.client.get(f"{ADMIN}/events?agent_name=ghost", headers=api.headers())
    assert unknown.status_code == 404
    bad = await api.client.get(f"{ADMIN}/events?limit=0", headers=api.headers())
    assert bad.status_code == 422
    first = everything[0]
    assert {"count", "first_seen", "last_seen", "type", "message"} <= set(first)


async def test_admin_control_plane_endpoints_need_the_admin_role() -> None:
    instance, manager = await start_api(agents=AGENTS, defaults={}, entra=True)
    try:
        for method, path in (
            ("get", f"{ADMIN}/agents/pool"),
            ("get", f"{ADMIN}/events"),
            ("post", f"{ADMIN}/config/reload"),
        ):
            anonymous = await instance.client.request(method, path)
            plain = await instance.client.request(method, path, headers=bearer())
            assert anonymous.status_code == 401 and plain.status_code == 403
    finally:
        await instance.client.aclose()
        await manager.__aexit__(None, None, None)


# ------------------------------------------------------------------ session resources


async def test_a_session_shows_its_conditions_and_resource_metadata(api: Api) -> None:
    await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=api.headers())
    (view,) = (await api.client.get(f"{ADMIN}/agents/pool/sessions", headers=api.headers())).json()
    states = conditions(view)
    assert states["Ready"]["status"] == "True" and states["Leased"]["status"] == "False"
    assert states["Provisioned"]["status"] == "True"
    assert states["DeletionPending"]["status"] == "False"
    assert view["finalizers"] == [] and view["deletion_timestamp"] is None
    assert view["resource_version"] >= 1 and view["generation"] == 1


async def test_a_session_whose_delete_failed_shows_the_finalizer_and_the_failure(api: Api) -> None:
    await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=api.headers())
    sid = api.fake.created[0]
    api.fake.delete_errors = [FoundryRejected(403)] * 10
    refused = await api.client.delete(f"{ADMIN}/agents/pool/sessions/{sid}", headers=api.headers())
    assert refused.status_code == 502
    view = (
        await api.client.get(f"{ADMIN}/agents/pool/sessions/{sid}", headers=api.headers())
    ).json()
    assert view["finalizers"] == ["foundry-session-cleanup"]
    assert view["deletion_timestamp"] and view["deletion_reason"] == "admin"
    states = conditions(view)
    assert states["DeletionPending"]["status"] == "True"
    assert states["DeletionFailed"]["reason"] == "RemoteDeleteFailed"
    assert states["Ready"]["reason"] == "Deleting"
    api.fake.delete_errors = []
    await api.container.reconciler.run_agent("pool")  # the next sync finishes the job
    gone = await api.client.get(f"{ADMIN}/agents/pool/sessions/{sid}", headers=api.headers())
    assert gone.status_code == 404


async def test_a_restorable_session_carries_a_computed_condition() -> None:
    deriver = SessionIdDeriver(b"k" * 32)
    instance, manager = await start_api(
        agents=AGENTS, defaults={}, session_id_key=deriver._key.decode()
    )
    try:
        sid = deriver.for_user("chat", "u1")
        instance.fake.add_session("chat", session_id=sid)
        await instance.container.reconciler.run_agent("chat")
        (view,) = (
            await instance.client.get(f"{ADMIN}/agents/chat/sessions", headers=instance.headers())
        ).json()
        assert view["restorable"] is True
        held = conditions(view)["Recoverable"]
        assert held["status"] == "True" and held["reason"] == "HeldForOwner"
    finally:
        await instance.client.aclose()
        await manager.__aexit__(None, None, None)


# ----------------------------------------------------------------------- reload


def doc(**pool: Any) -> Any:
    return make_config({"chat": AGENTS["chat"], "pool": {**AGENTS["pool"], **pool}}, {})


async def test_a_reload_applies_changes_and_reports_the_generations() -> None:
    current = {"config": doc()}
    instance, manager = await start_api(
        agents=AGENTS, defaults={}, config_loader=lambda: current["config"]
    )
    try:
        unchanged = await instance.client.post(f"{ADMIN}/config/reload", headers=instance.headers())
        assert unchanged.status_code == 200 and unchanged.json() == {"changed": {}}
        current["config"] = doc(max_sessions=6)
        reloaded = await instance.client.post(f"{ADMIN}/config/reload", headers=instance.headers())
        assert reloaded.json() == {"changed": {"pool": 2}}

        async def applied() -> bool:
            body = (
                await instance.client.get(f"{ADMIN}/agents/pool", headers=instance.headers())
            ).json()
            return bool(body["observed_generation"] == 2 and body["spec"]["max_sessions"] == 6)

        await eventually(applied)
        events = (
            await instance.client.get(f"{ADMIN}/events?agent_name=pool", headers=instance.headers())
        ).json()
        assert any(e["reason"] == "SpecChanged" for e in events)
        listing = (await instance.client.get(f"{ADMIN}/agents", headers=instance.headers())).json()
        by_name = {a["agent_name"]: a for a in listing}
        assert by_name["chat"]["generation"] == 1 and by_name["pool"]["generation"] == 2
        assert by_name["pool"]["max_sessions"] == 6
    finally:
        await instance.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_a_reload_changes_how_requests_are_scheduled() -> None:
    current = {"config": doc(max_sessions=1, queue={"enabled": False})}
    instance, manager = await start_api(
        agents={
            **AGENTS,
            "pool": {"mode": "stateless", "max_sessions": 1, "queue": {"enabled": False}},
        },
        defaults={},
        config_loader=lambda: current["config"],
    )
    try:
        instance.fake.invoke_gate = asyncio.Event()
        first = asyncio.create_task(
            instance.client.post(
                "/v1/agents/pool/responses", json=INPUT, headers=instance.headers()
            )
        )
        await settle(30)
        full = await instance.client.post(
            "/v1/agents/pool/responses", json=INPUT, headers=instance.headers("u2")
        )
        assert full.json()["error_code"] == "POOL_CAPACITY_EXCEEDED"
        current["config"] = doc(max_sessions=2, queue={"enabled": False})
        await instance.client.post(f"{ADMIN}/config/reload", headers=instance.headers())
        roomy = asyncio.create_task(
            instance.client.post(
                "/v1/agents/pool/responses", json=INPUT, headers=instance.headers("u2")
            )
        )
        await settle(30)
        instance.fake.invoke_gate.set()
        assert (await roomy).status_code == 200 and (await first).status_code == 200
        assert len(instance.fake.created) == 2  # the new limit allowed a second session
    finally:
        await instance.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_a_rejected_reload_changes_nothing_and_says_why() -> None:
    current = {"config": doc()}
    instance, manager = await start_api(
        agents=AGENTS, defaults={}, config_loader=lambda: current["config"]
    )
    try:
        current["config"] = make_config({"pool": AGENTS["pool"]}, {})  # an agent removed
        rejected = await instance.client.post(f"{ADMIN}/config/reload", headers=instance.headers())
        problem = rejected.json()
        assert rejected.status_code == 422 and problem["error_code"] == "CONFIG_INVALID"
        assert "restart" in problem["detail"] and problem["retry_safe"] is True
        await settled(instance)
        body = (
            await instance.client.get(f"{ADMIN}/agents/pool", headers=instance.headers())
        ).json()
        valid = conditions(body)["ConfigurationValid"]
        assert valid["status"] == "False" and valid["reason"] == "ReloadRejected"
        assert conditions(body)["Ready"]["status"] == "False"
        assert conditions(body)["Ready"]["reason"] == "ConfigurationInvalid"
        assert body["generation"] == 1 and body["spec"]["max_sessions"] == 2
        assert any(
            e["reason"] == "ReloadRejected"
            for e in body["events"]
            + (await instance.client.get(f"{ADMIN}/events", headers=instance.headers())).json()
        )
        current["config"] = doc()  # a good document clears it
        ok = await instance.client.post(f"{ADMIN}/config/reload", headers=instance.headers())
        assert ok.status_code == 200
        await settled(instance)
        body = (
            await instance.client.get(f"{ADMIN}/agents/pool", headers=instance.headers())
        ).json()
        assert conditions(body)["ConfigurationValid"]["status"] == "True"
        assert conditions(body)["Ready"]["status"] == "True"
    finally:
        await instance.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_a_reload_that_breaks_a_rule_across_settings_is_rejected() -> None:
    current = {"config": doc()}
    instance, manager = await start_api(
        agents=AGENTS,
        defaults={},
        config_loader=lambda: current["config"],
        session_id_key="x" * 40,
    )
    try:
        # With a session id key set, a stateful agent may not keep warm sessions.
        current["config"] = make_config(
            {"chat": {**AGENTS["chat"], "min_warm_sessions": 1}, "pool": AGENTS["pool"]}, {}
        )
        rejected = await instance.client.post(f"{ADMIN}/config/reload", headers=instance.headers())
        assert rejected.status_code == 422 and "warm" in rejected.json()["detail"]
    finally:
        await instance.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_reload_is_unavailable_without_a_configuration_file(api: Api) -> None:
    response = await api.client.post(f"{ADMIN}/config/reload", headers=api.headers())
    assert response.status_code == 409 and response.json()["error_code"] == "RELOAD_UNAVAILABLE"


# ---------------------------------------------------------------------- shutdown


async def test_a_service_that_is_shutting_down_refuses_new_requests_and_waits_for_running_ones(
    api: Api,
) -> None:
    api.fake.invoke_gate = asyncio.Event()
    running = asyncio.create_task(
        api.client.post("/v1/agents/pool/responses", json=INPUT, headers=api.headers())
    )
    await settle(30)
    pool = api.container.pool
    assert pool.in_flight == 1 and not pool.draining
    pool.begin_shutdown()
    assert pool.draining
    refused = await api.client.post(
        "/v1/agents/pool/responses", json=INPUT, headers=api.headers("u2")
    )
    problem = refused.json()
    assert refused.status_code == 503 and problem["error_code"] == "SERVICE_SHUTTING_DOWN"
    assert refused.headers["retry-after"] == str(api.settings.retry_after_seconds)
    assert problem["retry_safe"] is True and problem["phase"] == "request"
    assert not await pool.wait_idle(0.05)  # the running request has not finished
    api.fake.invoke_gate.set()
    assert (await running).status_code == 200  # it was allowed to finish
    assert await pool.wait_idle(1) and pool.in_flight == 0


async def test_a_stream_counts_as_running_until_it_has_ended(api: Api) -> None:
    result = await api.container.pool.execute(make_request("pool", stream=True))
    assert api.container.pool.in_flight == 1
    assert result.stream is not None
    _ = [frame async for frame in result.stream]
    assert api.container.pool.in_flight == 0


async def test_a_failed_request_is_not_left_counted(api: Api) -> None:
    api.fake.invoke_errors = [FoundryRejected(500)]
    await api.client.post("/v1/agents/pool/responses", json=INPUT, headers=api.headers())
    assert api.container.pool.in_flight == 0


async def test_stopping_the_app_waits_for_requests_in_flight_and_marks_it_not_ready() -> None:
    instance, manager = await start_api(agents=AGENTS, defaults={}, shutdown_grace_seconds=5)
    instance.fake.invoke_gate = asyncio.Event()
    running = asyncio.create_task(
        instance.client.post("/v1/agents/pool/responses", json=INPUT, headers=instance.headers())
    )
    await settle(30)
    stopping = asyncio.create_task(manager.__aexit__(None, None, None))
    await settle(30)
    assert instance.container.ready is False and instance.container.pool.draining
    assert not stopping.done()  # waiting for the request
    instance.fake.invoke_gate.set()
    assert (await running).status_code == 200
    await asyncio.wait_for(stopping, 5)
    assert instance.fake.closed
    await instance.client.aclose()


async def test_stopping_the_app_gives_up_after_the_grace_period() -> None:
    instance, manager = await start_api(agents=AGENTS, defaults={}, shutdown_grace_seconds=0.05)
    instance.fake.invoke_gate = asyncio.Event()  # never opens
    running = asyncio.create_task(
        instance.client.post("/v1/agents/pool/responses", json=INPUT, headers=instance.headers())
    )
    await settle(30)
    await asyncio.wait_for(manager.__aexit__(None, None, None), 5)
    assert instance.fake.closed
    running.cancel()
    await asyncio.gather(running, return_exceptions=True)
    await instance.client.aclose()


async def test_a_reload_with_an_unknown_scheduler_plugin_is_rejected_before_anything_changes() -> (
    None
):
    current = {"config": doc()}
    instance, manager = await start_api(
        agents=AGENTS, defaults={}, config_loader=lambda: current["config"]
    )
    try:
        current["config"] = doc(scheduler_profile={"scores": {"NoSuchScore": 5}})
        rejected = await instance.client.post(f"{ADMIN}/config/reload", headers=instance.headers())
        assert rejected.status_code == 422 and "NoSuchScore" in rejected.json()["detail"]
        assert instance.container.config.generation("pool") == 1  # nothing was applied
        assert instance.container.config.get("pool").scheduler_profile.scores == {}  # type: ignore[union-attr]
        ok = await instance.client.post(
            "/v1/agents/pool/responses", json=INPUT, headers=instance.headers()
        )
        assert ok.status_code == 200  # still serving with the old settings
    finally:
        await instance.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_a_reload_rebuilds_the_scheduling_profiles() -> None:
    current = {"config": doc()}
    instance, manager = await start_api(
        agents=AGENTS, defaults={}, config_loader=lambda: current["config"]
    )
    try:
        framework = instance.container.pool._framework
        assert [s.name for s, _ in framework.profile("pool").scores] == ["FirstAvailable"]
        current["config"] = doc(scheduler_profile={"scores": {"ExpiryRisk": 7}})
        reloaded = await instance.client.post(f"{ADMIN}/config/reload", headers=instance.headers())
        assert reloaded.status_code == 200
        assert [(s.name, w) for s, w in framework.profile("pool").scores] == [("ExpiryRisk", 7)]
    finally:
        await instance.client.aclose()
        await manager.__aexit__(None, None, None)
