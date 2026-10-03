"""Active versus persisted capacity, the idle window, and eviction to free compute."""

from __future__ import annotations

import asyncio
from datetime import timedelta
from typing import Any

import pytest

from hosted_agent_kit.config.models import AgentConfig
from hosted_agent_kit.domain.enums import FoundrySessionStatus, LocalSessionState
from hosted_agent_kit.domain.errors import QueueWaitTimeoutError
from hosted_agent_kit.domain.models import SessionRecord
from hosted_agent_kit.services.quota import IDLE_MARGIN_SECONDS, is_counted
from tests.conftest import make_config, make_harness, make_request, settle
from tests.fakes.clock import FakeClock

AGENT = "stateless-agent"


def cfg(**overrides: Any) -> AgentConfig:
    values = {"mode": "stateless", "max_sessions": 10, **overrides}
    return make_config({"a": values}).agents["a"]


def record(clock: FakeClock, **overrides: Any) -> SessionRecord:
    now = clock.now()
    values: dict[str, Any] = {
        "session_id": "s1",
        "agent_name": "a",
        "platform_status": FoundrySessionStatus.ACTIVE,
        "local_state": LocalSessionState.AVAILABLE,
        "created_at": now,
        "last_seen_at": now,
    }
    values.update(overrides)
    return SessionRecord(**values)


# ------------------------------------------------------------------------- is_counted


def test_a_new_idle_session_counts_until_the_idle_window_passes() -> None:
    clock = FakeClock()
    agent = cfg(idle_timeout_seconds=600)
    r = record(clock)
    assert is_counted(r, agent, clock.now(), idle_status_deprovisions=False)
    clock.advance(600 + IDLE_MARGIN_SECONDS - 1)
    assert is_counted(r, agent, clock.now(), idle_status_deprovisions=False)
    clock.advance(2)
    assert not is_counted(r, agent, clock.now(), idle_status_deprovisions=False)


def test_the_window_runs_from_the_last_use() -> None:
    clock = FakeClock()
    agent = cfg(idle_timeout_seconds=600)
    r = record(clock, last_released_at=clock.now() + timedelta(seconds=1000))
    clock.advance(1100)
    assert is_counted(r, agent, clock.now(), idle_status_deprovisions=False)


@pytest.mark.parametrize(
    ("overrides", "expected"),
    [
        ({"lease_request_id": "r"}, True),
        ({"platform_status": FoundrySessionStatus.CREATING}, True),
        ({"platform_status": FoundrySessionStatus.UPDATING}, True),
        ({"platform_status": FoundrySessionStatus.FAILED}, False),
        ({"platform_status": FoundrySessionStatus.DELETING}, False),
        ({"platform_status": FoundrySessionStatus.EXPIRED}, False),
    ],
)
def test_status_decides_before_the_window(overrides: dict[str, Any], expected: bool) -> None:
    clock = FakeClock()
    clock.advance(100_000)  # far outside any idle window
    agent = cfg()
    r = record(clock, created_at=clock.now() - timedelta(days=2), **overrides)
    assert is_counted(r, agent, clock.now(), idle_status_deprovisions=False) is expected


def test_a_stopped_session_does_not_count_until_it_is_used_again() -> None:
    clock = FakeClock()
    agent = cfg()
    r = record(clock, compute_released_at=clock.now() + timedelta(seconds=5))
    assert not is_counted(r, agent, clock.now(), idle_status_deprovisions=False)
    r.last_released_at = clock.now() + timedelta(seconds=10)  # used after it was stopped
    assert is_counted(r, agent, clock.now(), idle_status_deprovisions=False)


def test_idle_status_can_be_declared_to_mean_deprovisioned() -> None:
    clock = FakeClock()
    agent = cfg()
    r = record(clock, platform_status=FoundrySessionStatus.IDLE)
    assert is_counted(r, agent, clock.now(), idle_status_deprovisions=False)
    assert not is_counted(r, agent, clock.now(), idle_status_deprovisions=True)


# ------------------------------------------------------------------------- config


def test_max_active_sessions_defaults_to_max_sessions_and_cannot_exceed_it() -> None:
    assert cfg(max_sessions=4).active_limit == 4
    assert cfg(max_sessions=4, max_active_sessions=2).active_limit == 2
    with pytest.raises(Exception, match="max_active_sessions cannot exceed max_sessions"):
        cfg(max_sessions=2, max_active_sessions=3)


# ------------------------------------------------------------------------- stateless


async def test_the_active_limit_holds_while_persisted_sessions_remain() -> None:
    h = make_harness(
        defaults={
            "mode": "stateless",
            "max_sessions": 5,
            "max_active_sessions": 2,
            "idle_timeout_seconds": 60,
            "queue": {"max_wait_seconds": 5},
        }
    )
    h.fake.invoke_gate = asyncio.Event()
    calls = [asyncio.create_task(h.pool.execute(make_request(user=f"u{i}"))) for i in range(3)]
    await settle()
    assert len(h.fake.created) == 2  # two sessions hold compute; the third call waits
    snapshot = await h.pool.snapshot(AGENT)
    assert snapshot.sessions_counted == 2 and snapshot.max_active_sessions == 2
    h.fake.invoke_gate.set()
    results = await asyncio.gather(*calls)
    assert all(r.status_code == 200 for r in results)
    assert len(h.fake.created) == 2  # the waiter reused a freed session


async def test_sessions_past_the_idle_window_are_resumed_before_new_ones_are_made() -> None:
    h = make_harness(
        defaults={
            "mode": "stateless",
            "max_sessions": 5,
            "max_active_sessions": 2,
            "idle_timeout_seconds": 60,
        }
    )
    await h.pool.execute(make_request())
    h.clock.advance(60 + IDLE_MARGIN_SECONDS + 1)  # Foundry has deprovisioned it
    snapshot = await h.pool.snapshot(AGENT)
    assert snapshot.sessions_total == 1 and snapshot.sessions_counted == 0
    await h.pool.execute(make_request())
    assert len(h.fake.created) == 1  # the same session was resumed
    assert (await h.pool.snapshot(AGENT)).sessions_counted == 1


async def test_a_counted_session_is_preferred_over_one_that_must_resume() -> None:
    h = make_harness(
        defaults={
            "mode": "stateless",
            "max_sessions": 5,
            "max_active_sessions": 5,
            "idle_timeout_seconds": 60,
            "scheduler": "oldest_idle",
        }
    )
    old = h.fake.add_session(AGENT)
    new = h.fake.add_session(AGENT)
    await h.reconciler.run_agent(AGENT)
    h.clock.advance(1000)
    await h.registry.refresh(
        AGENT,
        new.session_id,
        platform_status=FoundrySessionStatus.ACTIVE,
        agent_version="1",
        last_accessed_at=h.clock.now(),
        expires_at=None,
        last_seen_at=h.clock.now(),
    )
    await h.pool.execute(make_request())
    assert h.fake.invocations[-1].session_id == new.session_id  # the warm one, not `old`
    assert old.session_id != new.session_id


# -------------------------------------------------------------------------- stateful


def stateful(**overrides: Any) -> dict[str, Any]:
    return {
        "mode": "stateful",
        "max_sessions": 5,
        "max_active_sessions": 2,
        "idle_timeout_seconds": 600,
        "queue": {"max_wait_seconds": 2},
        **overrides,
    }


async def test_a_new_user_stops_the_least_recently_used_idle_session() -> None:
    h = make_harness({"memo": {}}, defaults=stateful())
    sessions = {}
    for user in ("u1", "u2"):
        await h.pool.execute(make_request("memo", user))
        sessions[user] = h.fake.invocations[-1].session_id
        h.clock.advance(5)
    await h.pool.execute(make_request("memo", "u3"))  # no room: u1 is the least recently used
    assert h.fake.stopped == [sessions["u1"]]
    assert len(h.fake.created) == 3
    assert h.fake.deleted == []  # nothing was deleted: u1's state is kept
    record_u1 = await h.registry.get("memo", sessions["u1"])
    assert record_u1 is not None and record_u1.compute_released_at is not None
    assert record_u1.local_state is LocalSessionState.AVAILABLE


async def test_an_evicted_user_resumes_the_same_session_and_evicts_another() -> None:
    h = make_harness({"memo": {}}, defaults=stateful())
    first: dict[str, str] = {}
    for user in ("u1", "u2", "u3"):
        await h.pool.execute(make_request("memo", user))
        first[user] = h.fake.invocations[-1].session_id
        h.clock.advance(5)
    assert h.fake.stopped == [first["u1"]]
    await h.pool.execute(make_request("memo", "u1"))
    assert h.fake.invocations[-1].session_id == first["u1"]  # state and identity kept
    assert h.fake.stopped == [first["u1"], first["u2"]]  # u2 was the next least recently used
    assert len(h.fake.created) == 3  # no new session for u1


async def test_a_leased_session_is_never_evicted() -> None:
    h = make_harness({"memo": {}}, defaults=stateful(queue={"max_wait_seconds": 1}))
    h.fake.invoke_gate = asyncio.Event()
    busy = [
        asyncio.create_task(h.pool.execute(make_request("memo", user))) for user in ("u1", "u2")
    ]
    await settle()
    with pytest.raises(QueueWaitTimeoutError):
        await h.pool.execute(make_request("memo", "u3"))
    assert h.fake.stopped == []
    h.fake.invoke_gate.set()
    await asyncio.gather(*busy)


async def test_the_persisted_limit_never_deletes_a_users_state() -> None:
    h = make_harness({"memo": {}}, defaults=stateful(max_sessions=2, max_active_sessions=2))
    for user in ("u1", "u2"):
        await h.pool.execute(make_request("memo", user))
    with pytest.raises(QueueWaitTimeoutError):
        await h.pool.execute(make_request("memo", "u3"))
    assert h.fake.stopped == [] and h.fake.deleted == []


async def test_eviction_can_be_turned_off() -> None:
    h = make_harness({"memo": {}}, defaults=stateful(), evict_idle_for_quota=False)
    for user in ("u1", "u2"):
        await h.pool.execute(make_request("memo", user))
    with pytest.raises(QueueWaitTimeoutError):
        await h.pool.execute(make_request("memo", "u3"))
    assert h.fake.stopped == []


async def test_a_failed_stop_leaves_the_session_usable_and_counted() -> None:
    from hosted_agent_kit.domain.errors import FoundryUnavailable

    h = make_harness({"memo": {}}, defaults=stateful())
    for user in ("u1", "u2"):
        await h.pool.execute(make_request("memo", user))

    async def refuse(agent: str, session_id: str) -> None:
        raise FoundryUnavailable()

    h.fake.stop_session = refuse  # type: ignore[method-assign]
    with pytest.raises(QueueWaitTimeoutError):
        await h.pool.execute(make_request("memo", "u3"))
    for r in await h.registry.list("memo"):
        assert r.local_state is LocalSessionState.AVAILABLE and r.compute_released_at is None


async def test_the_waiter_is_served_when_the_idle_window_passes() -> None:
    h = make_harness({"memo": {}}, defaults=stateful(), evict_idle_for_quota=False)
    for user in ("u1", "u2"):
        await h.pool.execute(make_request("memo", user))
    waiter = asyncio.create_task(h.pool.execute(make_request("memo", "u3")))
    await settle()
    assert not waiter.done()
    h.clock.advance(600 + IDLE_MARGIN_SECONDS + 1)
    await h.pool.quota.tick()
    await h.pool.dispatch_waiting()
    result = await asyncio.wait_for(waiter, timeout=2)
    assert result.status_code == 200
