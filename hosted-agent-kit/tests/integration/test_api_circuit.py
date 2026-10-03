"""The circuit breaker as callers and operators see it."""

from __future__ import annotations

import pytest

from hosted_agent_kit.domain.errors import FoundryTimeout, FoundryUnavailable
from tests.fakes.clock import FakeClock
from tests.integration.conftest import start_api

URL = "/v1/agents/a/invoke"
BODY = {"input": {"input": "hi"}}
BREAKER = {"failure_threshold": 2, "open_seconds": 30}


async def post(api, user: str = "u"):  # type: ignore[no-untyped-def]
    return await api.client.post(URL, json=BODY, headers=api.headers(user))


async def agent_state(api) -> str:  # type: ignore[no-untyped-def]
    listing = await api.client.get("/v1/admin/agents", headers=api.headers())
    return str(next(a for a in listing.json() if a["agent_name"] == "a")["circuit_state"])


async def test_repeated_foundry_failures_open_the_circuit_and_callers_fail_fast() -> None:
    clock = FakeClock()
    api, manager = await start_api(
        agents={"a": {"mode": "stateless", "circuit_breaker": BREAKER}}, defaults={}, clock=clock
    )
    try:
        assert (await post(api)).status_code == 200
        api.fake.invoke_errors = [FoundryUnavailable("x"), FoundryTimeout()]
        assert (await post(api)).status_code == 503
        assert (await post(api)).status_code == 504
        calls = len(api.fake.invocations)
        blocked = await post(api)
        problem = blocked.json()
        assert blocked.status_code == 503 and problem["error_code"] == "FOUNDRY_CIRCUIT_OPEN"
        assert blocked.headers["retry-after"] == "30" and problem["retry_after_seconds"] == 30
        assert len(api.fake.invocations) == calls  # nothing reached Foundry
        assert await agent_state(api) == "open"
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_the_circuit_recovers_through_a_probe_after_the_cool_down() -> None:
    clock = FakeClock()
    api, manager = await start_api(
        agents={"a": {"mode": "stateless", "circuit_breaker": BREAKER}}, defaults={}, clock=clock
    )
    try:
        api.fake.invoke_errors = [FoundryUnavailable("x")] * 2
        await post(api)
        await post(api)
        assert (await post(api)).json()["error_code"] == "FOUNDRY_CIRCUIT_OPEN"
        clock.advance(31)
        assert (await post(api)).status_code == 200
        assert await agent_state(api) == "closed"
        assert (await post(api)).status_code == 200
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_a_failed_probe_keeps_it_open_and_one_agent_does_not_affect_another() -> None:
    clock = FakeClock()
    api, manager = await start_api(
        agents={
            "a": {"mode": "stateless", "circuit_breaker": BREAKER},
            "b": {"mode": "stateless", "circuit_breaker": BREAKER},
        },
        defaults={},
        clock=clock,
    )
    try:
        api.fake.invoke_errors = [FoundryUnavailable("x")] * 3
        await post(api)
        await post(api)
        clock.advance(31)
        assert (await post(api)).status_code == 503  # the probe failed
        assert (await post(api)).json()["error_code"] == "FOUNDRY_CIRCUIT_OPEN"
        other = await api.client.post("/v1/agents/b/invoke", json=BODY, headers=api.headers())
        assert other.status_code == 200
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_an_open_circuit_makes_a_sync_incomplete_and_deletes_nothing() -> None:
    clock = FakeClock()
    api, manager = await start_api(
        agents={"a": {"mode": "stateless", "circuit_breaker": BREAKER}}, defaults={}, clock=clock
    )
    try:
        await post(api)
        api.fake.invoke_errors = [FoundryUnavailable("x")] * 2
        await post(api)
        await post(api)
        report = await api.client.post("/v1/admin/agents/a/sync", headers=api.headers())
        assert report.json()["outcome"] == "incomplete" and api.fake.deleted == []
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_metrics_show_the_circuit_state_and_transitions() -> None:
    api, manager = await start_api(
        agents={"a": {"mode": "stateless", "circuit_breaker": BREAKER}},
        defaults={},
        clock=FakeClock(),
        metrics_endpoint_enabled=True,
    )
    try:
        api.fake.invoke_errors = [FoundryUnavailable("x")] * 2
        await post(api)
        await post(api)
        text = (await api.client.get("/metrics", headers=api.headers())).text
        assert 'pool_circuit_state{agent="a"} 2' in text
        assert 'pool_circuit_transitions_total{agent="a",to="open"} 1' in text
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_a_disabled_breaker_never_trips() -> None:
    api, manager = await start_api(
        agents={"a": {"mode": "stateless", "circuit_breaker": {"enabled": False}}}, defaults={}
    )
    try:
        api.fake.invoke_errors = [FoundryUnavailable("x")] * 8
        statuses = {(await post(api)).status_code for _ in range(8)}
        assert statuses == {503}
        assert await agent_state(api) == "disabled"
        assert (await post(api)).status_code == 200
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


@pytest.mark.parametrize("failures", [1, 4])
async def test_the_default_threshold_is_five_consecutive_failures(failures: int) -> None:
    api, manager = await start_api(agents={"a": {"mode": "stateless"}}, defaults={})
    try:
        api.fake.invoke_errors = [FoundryUnavailable("x")] * failures
        for _ in range(failures):
            await post(api)
        assert await agent_state(api) == "closed"
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)
