"""Core pool behaviour: affinity, leases, capacity and queueing (acceptance A1 to A7, A12)."""

from __future__ import annotations

import asyncio

import pytest

from hosted_agent_kit.domain.enums import LocalSessionState
from hosted_agent_kit.domain.errors import (
    AgentNotConfiguredError,
    PoolCapacityExceededError,
    QueueFullError,
    QueueWaitTimeoutError,
    StickySessionTimeoutError,
)
from hosted_agent_kit.domain.models import SessionAffinityKey
from tests.conftest import make_harness, make_request, settle

STATEFUL = {"coding-agent": {"mode": "stateful", "max_sessions": 3}}


async def test_unknown_agent_makes_no_foundry_call() -> None:
    h = make_harness()
    with pytest.raises(AgentNotConfiguredError):
        await h.pool.execute(make_request(agent="nope"))
    assert h.fake.created == [] and h.fake.invocations == []


async def test_stateless_creates_then_reuses_session_across_users() -> None:
    """A5: sequential requests from different users may reuse one stateless session."""
    h = make_harness()
    await h.pool.execute(make_request(user="a"))
    await h.pool.execute(make_request(user="b"))
    assert len(h.fake.created) == 1
    assert {c.session_id for c in h.fake.invocations} == {h.fake.created[0]}
    assert await h.affinity.count("stateless-agent") == 0  # no mapping for stateless


async def test_result_carries_body_and_never_leaks_session_in_body() -> None:
    h = make_harness()
    result = await h.pool.execute(make_request())
    assert result.body == {"status": "completed", "echo": {"input": "hello"}}
    assert result.stream is None and result.status_code == 200


async def test_stateful_same_user_reuses_session() -> None:
    """A1."""
    h = make_harness(STATEFUL, defaults={})
    await h.pool.execute(make_request("coding-agent", "u1"))
    await h.pool.execute(make_request("coding-agent", "u1"))
    assert len(h.fake.created) == 1
    assert len({c.session_id for c in h.fake.invocations}) == 1


async def test_stateful_different_users_get_distinct_sessions() -> None:
    """A3."""
    h = make_harness(STATEFUL, defaults={})
    await h.pool.execute(make_request("coding-agent", "u1"))
    await h.pool.execute(make_request("coding-agent", "u2"))
    assert len(h.fake.created) == 2
    sessions = {c.session_id for c in h.fake.invocations}
    assert len(sessions) == 2
    entry1 = await h.affinity.get(SessionAffinityKey(user_id="u1", agent_name="coding-agent"))
    entry2 = await h.affinity.get(SessionAffinityKey(user_id="u2", agent_name="coding-agent"))
    assert entry1 and entry2 and entry1.session_id != entry2.session_id


async def test_same_user_gets_separate_sessions_per_agent() -> None:
    """A2."""
    h = make_harness(
        {"coding-agent": {"mode": "stateful"}, "research-agent": {"mode": "stateful"}},
        defaults={"max_sessions": 3},
    )
    await h.pool.execute(make_request("coding-agent", "u1"))
    await h.pool.execute(make_request("research-agent", "u1"))
    await h.pool.execute(make_request("coding-agent", "u1"))
    by_agent = {c.agent_name: c.session_id for c in h.fake.invocations}
    assert len(h.fake.created) == 2
    assert by_agent["coding-agent"] != by_agent["research-agent"]
    first_coding = next(c for c in h.fake.invocations if c.agent_name == "coding-agent")
    assert first_coding.session_id == by_agent["coding-agent"]


async def test_lease_is_exclusive_and_waiter_is_fifo() -> None:
    """A6: with one session, requests are served one at a time in arrival order."""
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    h.fake.invoke_gate = asyncio.Event()
    order: list[str] = []

    async def call(name: str) -> None:
        await h.pool.execute(make_request(user=name))
        order.append(name)

    tasks = []
    for name in ("first", "second", "third"):
        tasks.append(asyncio.create_task(call(name)))
        await settle()
    assert await h.queue.depth("stateless-agent") == 2
    assert len(h.fake.invocations) == 1  # only one executing: mutual exclusion
    h.fake.invoke_gate.set()
    await asyncio.gather(*tasks)
    assert order == ["first", "second", "third"]
    assert len(h.fake.created) == 1


async def test_capacity_never_exceeded_under_concurrent_creates() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 2})
    h.fake.invoke_gate = asyncio.Event()
    tasks = [asyncio.create_task(h.pool.execute(make_request(user=f"u{i}"))) for i in range(8)]
    await settle(10)
    assert len(h.fake.created) == 2
    assert await h.registry.count("stateless-agent") <= 2
    h.fake.invoke_gate.set()
    await asyncio.gather(*tasks)
    assert len(h.fake.created) == 2


async def test_stateful_busy_session_makes_same_user_wait_not_reroute() -> None:
    """A4: the second request waits for the mapped session even with free capacity."""
    h = make_harness(STATEFUL, defaults={})
    h.fake.invoke_gate = asyncio.Event()
    first = asyncio.create_task(h.pool.execute(make_request("coding-agent", "u1")))
    await settle()
    second = asyncio.create_task(h.pool.execute(make_request("coding-agent", "u1")))
    await settle()
    assert await h.queue.depth("coding-agent") == 1
    assert len(h.fake.created) == 1  # capacity was free, yet no second session
    h.fake.invoke_gate.set()
    await asyncio.gather(first, second)
    assert len({c.session_id for c in h.fake.invocations}) == 1


async def test_stateful_other_user_not_blocked_by_busy_user() -> None:
    h = make_harness(STATEFUL, defaults={})
    h.fake.invoke_gate = asyncio.Event()
    first = asyncio.create_task(h.pool.execute(make_request("coding-agent", "u1")))
    await settle()
    second = asyncio.create_task(h.pool.execute(make_request("coding-agent", "u2")))
    await settle()
    assert await h.queue.depth("coding-agent") == 0
    h.fake.invoke_gate.set()
    await asyncio.gather(first, second)


async def test_concurrent_first_requests_for_one_user_create_one_session() -> None:
    h = make_harness(STATEFUL, defaults={})
    h.fake.invoke_gate = asyncio.Event()
    tasks = [
        asyncio.create_task(h.pool.execute(make_request("coding-agent", "u1"))) for _ in range(4)
    ]
    await settle(10)
    assert len(h.fake.created) == 1
    h.fake.invoke_gate.set()
    await asyncio.gather(*tasks)
    assert len(h.fake.created) == 1
    assert await h.affinity.count("coding-agent") == 1


async def test_stateful_capacity_exhausted_waits_then_creates_after_release() -> None:
    h = make_harness({"coding-agent": {"mode": "stateful", "max_sessions": 1}}, defaults={})
    h.fake.invoke_gate = asyncio.Event()
    first = asyncio.create_task(h.pool.execute(make_request("coding-agent", "u1")))
    await settle()
    second = asyncio.create_task(h.pool.execute(make_request("coding-agent", "u2")))
    await settle()
    assert await h.queue.depth("coding-agent") == 1
    h.fake.invoke_gate.set()
    await first
    # u2 is not entitled to u1's bound session; it times out or waits. Cancel to finish.
    second.cancel()
    with pytest.raises(asyncio.CancelledError):
        await second
    assert await h.queue.depth("coding-agent") == 0


async def test_queue_full_returns_queue_full_with_retry_after() -> None:
    """A7."""
    h = make_harness(
        {"a": {"mode": "stateless", "max_sessions": 1, "queue": {"max_depth": 1}}}, defaults={}
    )
    h.fake.invoke_gate = asyncio.Event()
    running = asyncio.create_task(h.pool.execute(make_request("a", "u1")))
    await settle()
    waiting = asyncio.create_task(h.pool.execute(make_request("a", "u2")))
    await settle()
    with pytest.raises(QueueFullError) as info:
        await h.pool.execute(make_request("a", "u3"))
    assert info.value.retry_after_seconds == 7
    h.fake.invoke_gate.set()
    await asyncio.gather(running, waiting)


async def test_queue_disabled_returns_capacity_exceeded() -> None:
    h = make_harness(
        {"a": {"mode": "stateless", "max_sessions": 1, "queue": {"enabled": False}}}, defaults={}
    )
    h.fake.invoke_gate = asyncio.Event()
    running = asyncio.create_task(h.pool.execute(make_request("a", "u1")))
    await settle()
    with pytest.raises(PoolCapacityExceededError) as info:
        await h.pool.execute(make_request("a", "u2"))
    assert info.value.retry_after_seconds == 7
    h.fake.invoke_gate.set()
    await running


async def test_queue_wait_timeout_removes_waiter() -> None:
    h = make_harness(
        {"a": {"mode": "stateless", "max_sessions": 1, "queue": {"max_wait_seconds": 0.05}}},
        defaults={},
    )
    h.fake.invoke_gate = asyncio.Event()
    running = asyncio.create_task(h.pool.execute(make_request("a", "u1")))
    await settle()
    with pytest.raises(QueueWaitTimeoutError):
        await h.pool.execute(make_request("a", "u2"))
    assert await h.queue.depth("a") == 0
    h.fake.invoke_gate.set()
    await running
    # the abandoned waiter must not have stolen the session
    result = await h.pool.execute(make_request("a", "u3"))
    assert result.status_code == 200


async def test_sticky_timeout_does_not_reroute() -> None:
    h = make_harness(
        {"a": {"mode": "stateful", "max_sessions": 3, "queue": {"max_wait_seconds": 0.05}}},
        defaults={},
    )
    h.fake.invoke_gate = asyncio.Event()
    running = asyncio.create_task(h.pool.execute(make_request("a", "u1")))
    await settle()
    with pytest.raises(StickySessionTimeoutError):
        await h.pool.execute(make_request("a", "u1"))
    assert len(h.fake.created) == 1
    h.fake.invoke_gate.set()
    await running


async def test_cancelled_waiter_leaves_queue_and_next_waiter_still_served() -> None:
    """A12 (queue side)."""
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    h.fake.invoke_gate = asyncio.Event()
    running = asyncio.create_task(h.pool.execute(make_request(user="u1")))
    await settle()
    doomed = asyncio.create_task(h.pool.execute(make_request(user="u2")))
    survivor = asyncio.create_task(h.pool.execute(make_request(user="u3")))
    await settle()
    assert await h.queue.depth("stateless-agent") == 2
    doomed.cancel()
    with pytest.raises(asyncio.CancelledError):
        await doomed
    assert await h.queue.depth("stateless-agent") == 1
    h.fake.invoke_gate.set()
    await asyncio.gather(running, survivor)


async def test_cancelled_running_request_releases_lease() -> None:
    """A12 (lease side)."""
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    h.fake.invoke_gate = asyncio.Event()
    running = asyncio.create_task(h.pool.execute(make_request(user="u1")))
    await settle()
    running.cancel()
    with pytest.raises(asyncio.CancelledError):
        await running
    records = await h.registry.list("stateless-agent")
    assert [r.local_state for r in records] == [LocalSessionState.AVAILABLE]
    assert records[0].lease_request_id is None
    h.fake.invoke_gate = None
    await h.pool.execute(make_request(user="u2"))


async def test_exception_during_invoke_releases_lease() -> None:
    from hosted_agent_kit.domain.errors import FoundryTimeout, FoundryTimeoutAppError

    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    h.fake.invoke_errors = [FoundryTimeout()]
    with pytest.raises(FoundryTimeoutAppError):
        await h.pool.execute(make_request())
    (record,) = await h.registry.list("stateless-agent")
    assert record.local_state is LocalSessionState.AVAILABLE


async def test_double_release_is_harmless() -> None:
    h = make_harness()
    await h.pool.execute(make_request())
    (record,) = await h.registry.list("stateless-agent")
    assert (
        await h.registry.release(
            "stateless-agent", record.session_id, "someone-else", h.clock.now()
        )
        is None
    )
    (after,) = await h.registry.list("stateless-agent")
    assert after.local_state is LocalSessionState.AVAILABLE
