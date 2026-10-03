"""Concurrency edge cases: pending creation, cancel-after-grant races and stale state."""

from __future__ import annotations

import asyncio
from collections.abc import AsyncIterator

import pytest

from hosted_agent_kit.domain.enums import LocalSessionState
from hosted_agent_kit.domain.models import SessionAffinityKey
from hosted_agent_kit.ports.foundry import UpstreamResponse
from hosted_agent_kit.ports.queue import CreateGrant, QueueTicket, SessionGrant
from hosted_agent_kit.services.pool import PoolResult
from tests.conftest import make_harness, make_request, settle

STATEFUL = {"coding-agent": {"mode": "stateful", "max_sessions": 3}}


def key(user: str = "u1", agent: str = "coding-agent") -> SessionAffinityKey:
    return SessionAffinityKey(user_id=user, agent_name=agent)


async def test_requests_for_a_user_whose_session_is_being_created_wait_for_it() -> None:
    """The pending claim stops a second concurrent creation for the same (user, agent)."""
    h = make_harness(STATEFUL, defaults={})
    gate = asyncio.Event()
    original = h.fake.create_session

    async def slow(agent: str, session_id: str | None = None, version: str | None = None):  # type: ignore[no-untyped-def]
        await gate.wait()
        return await original(agent)

    h.fake.create_session = slow  # type: ignore[method-assign]
    tasks = [
        asyncio.create_task(h.pool.execute(make_request("coding-agent", "u1"))) for _ in range(3)
    ]
    await settle(10)
    assert await h.queue.depth("coding-agent") == 2
    entry = await h.affinity.get(key())
    assert entry is not None and entry.session_id is None  # pending claim
    gate.set()
    results = await asyncio.gather(*tasks)
    assert all(r.status_code == 200 for r in results)
    assert len(h.fake.created) == 1
    assert len({c.session_id for c in h.fake.invocations}) == 1


async def test_failed_pending_creation_lets_the_next_waiter_try() -> None:
    from hosted_agent_kit.domain.errors import FoundryUnavailable, FoundryUnavailableError

    h = make_harness({"coding-agent": {"mode": "stateful", "create_retries": 0}}, defaults={})
    gate = asyncio.Event()
    original = h.fake.create_session
    calls = 0

    async def flaky(agent: str, session_id: str | None = None, version: str | None = None):  # type: ignore[no-untyped-def]
        nonlocal calls
        calls += 1
        if calls == 1:
            await gate.wait()
            raise FoundryUnavailable("first creation fails")
        return await original(agent)

    h.fake.create_session = flaky  # type: ignore[method-assign]
    first = asyncio.create_task(h.pool.execute(make_request("coding-agent", "u1")))
    await settle()
    second = asyncio.create_task(h.pool.execute(make_request("coding-agent", "u1")))
    await settle()
    gate.set()
    with pytest.raises(FoundryUnavailableError):
        await first
    assert (await second).status_code == 200
    assert calls == 2


async def test_mapping_to_a_vanished_session_is_dropped_and_replaced() -> None:
    h = make_harness(STATEFUL, defaults={})
    await h.pool.execute(make_request("coding-agent", "u1"))
    old = h.fake.created[0]
    await h.registry.remove("coding-agent", old)  # the record vanished but the mapping remains
    await h.pool.execute(make_request("coding-agent", "u1"))
    entry = await h.affinity.get(key())
    assert entry is not None and entry.session_id == h.fake.created[1] != old


async def test_mapping_to_a_retiring_session_is_dropped_and_replaced() -> None:
    h = make_harness(STATEFUL, defaults={})
    await h.pool.execute(make_request("coding-agent", "u1"))
    old = h.fake.created[0]
    await h.registry.mark_retiring("coding-agent", old)
    await h.pool.execute(make_request("coding-agent", "u1"))
    entry = await h.affinity.get(key())
    assert entry is not None and entry.session_id == h.fake.created[1]
    retiring = await h.registry.get("coding-agent", old)
    assert retiring is not None and retiring.local_state is LocalSessionState.RETIRING


async def test_dispatcher_skips_waiters_that_are_already_resolved() -> None:
    h = make_harness()
    ticket = QueueTicket(
        "ghost",
        "stateless-agent",
        "u",
        h.clock.now(),
        False,
        asyncio.get_running_loop().create_future(),
    )
    ticket.future.set_result(SessionGrant("whatever"))
    await h.queue.enqueue(ticket, 5)
    await h.pool.notify("stateless-agent")
    assert await h.queue.depth("stateless-agent") == 1  # left for its owner to clean up


async def test_waiter_cancelled_after_a_session_was_granted_returns_the_lease() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    h.fake.invoke_gate = asyncio.Event()
    holder = asyncio.create_task(h.pool.execute(make_request(user="holder")))
    await settle()
    waiter = asyncio.create_task(h.pool.execute(make_request(user="waiter")))
    await settle()
    (ticket,) = await h.queue.pending("stateless-agent")
    # Cancel the waiter in the same loop turn in which its future is resolved.
    ticket.future.add_done_callback(lambda _: waiter.cancel())
    h.fake.invoke_gate.set()
    await holder
    with pytest.raises(asyncio.CancelledError):
        await waiter
    (record,) = await h.registry.list("stateless-agent")
    assert record.local_state is LocalSessionState.AVAILABLE and record.lease_request_id is None
    assert await h.queue.depth("stateless-agent") == 0
    h.fake.invoke_gate = None
    assert (await h.pool.execute(make_request(user="next"))).status_code == 200  # nothing stranded


async def test_abandoned_create_grant_returns_slot_and_pending_claim() -> None:
    h = make_harness(STATEFUL, defaults={})
    cfg = h.pool.agent_config("coding-agent")
    assert await h.registry.reserve_slot("coding-agent", "tok", cfg.max_sessions)
    assert await h.affinity.reserve(key(), "req-x")
    ticket = QueueTicket(
        "req-x",
        "coding-agent",
        "u1",
        h.clock.now(),
        False,
        asyncio.get_running_loop().create_future(),
    )
    ticket.future.set_result(CreateGrant("tok", key()))
    await h.queue.enqueue(ticket, 5)
    await h.pool._abandon(cfg, ticket)
    assert await h.registry.reserved("coding-agent") == 0
    assert await h.affinity.get(key()) is None
    assert await h.queue.depth("coding-agent") == 0


async def test_abandoned_stateless_create_grant_only_returns_the_slot() -> None:
    h = make_harness()
    cfg = h.pool.agent_config("stateless-agent")
    assert await h.registry.reserve_slot("stateless-agent", "tok", cfg.max_sessions)
    ticket = QueueTicket(
        "r",
        "stateless-agent",
        "u",
        h.clock.now(),
        False,
        asyncio.get_running_loop().create_future(),
    )
    ticket.future.set_result(CreateGrant("tok", None))
    await h.queue.enqueue(ticket, 5)
    await h.pool._abandon(cfg, ticket)
    assert await h.registry.reserved("stateless-agent") == 0


async def test_registry_refusing_a_lease_falls_through_to_capacity() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 2})
    await h.pool.execute(make_request())
    original = h.registry.try_lease
    refused = 0

    async def refuse_once(agent_name: str, session_id: str, request_id: str, now):  # type: ignore[no-untyped-def]
        nonlocal refused
        if refused == 0:
            refused += 1
            return None
        return await original(agent_name, session_id, request_id, now)

    h.registry.try_lease = refuse_once  # type: ignore[method-assign]
    await h.pool.execute(make_request())
    assert refused == 1 and len(h.fake.created) == 2  # created instead of reusing


async def test_retire_unknown_session_is_a_noop_success() -> None:
    h = make_harness()
    assert await h.pool.retire_session("stateless-agent", "nope", "x", delete_remote=True) is True
    assert h.fake.deleted == []


async def test_pool_result_close_without_a_stream_is_harmless() -> None:
    result = PoolResult("r", 200, {}, None, "application/json", "s")
    await result.close()


async def test_upstream_stream_without_aclose_is_supported() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})

    class Plain:
        def __init__(self) -> None:
            self.items = [b"x"]

        def __aiter__(self) -> AsyncIterator[bytes]:
            return self

        async def __anext__(self) -> bytes:
            if not self.items:
                raise StopAsyncIteration
            return self.items.pop()

    h.fake.invoke_handler = lambda ctx: UpstreamResponse(
        stream=Plain(), media_type="text/event-stream"
    )
    result = await h.pool.execute(make_request(stream=True))
    assert result.stream is not None
    assert [f async for f in result.stream] == [b"x"]
    (record,) = await h.registry.list("stateless-agent")
    assert record.local_state is LocalSessionState.AVAILABLE


async def test_admin_delete_semantics() -> None:
    from hosted_agent_kit.domain.errors import SessionNotFoundAdminError

    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    with pytest.raises(SessionNotFoundAdminError):
        await h.pool.get_session_record("stateless-agent", "nope")
    with pytest.raises(SessionNotFoundAdminError):
        await h.pool.admin_delete("stateless-agent", "nope")
    await h.pool.execute(make_request())
    sid = h.fake.created[0]
    assert await h.pool.admin_delete("stateless-agent", sid) is True
    assert h.fake.deleted == [sid] and await h.registry.count("stateless-agent") == 0
    snapshot = await h.pool.snapshot("stateless-agent")
    assert snapshot.sessions_total == 0 and snapshot.queue_max_depth == 500


async def test_admin_delete_on_a_leased_session_is_deferred() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    h.fake.invoke_gate = asyncio.Event()
    task = asyncio.create_task(h.pool.execute(make_request()))
    await settle()
    sid = h.fake.created[0]
    assert await h.pool.admin_delete("stateless-agent", sid) is False
    assert h.fake.deleted == []
    h.fake.invoke_gate.set()
    await task
    assert h.fake.deleted == [sid] and await h.registry.count("stateless-agent") == 0


async def test_snapshot_reports_queue_limits_and_disabled_queue() -> None:
    h = make_harness({"a": {"mode": "stateless", "queue": {"enabled": False}}}, defaults={})
    snapshot = await h.pool.snapshot("a")
    assert snapshot.queue_max_depth == 0 and snapshot.telemetry_enabled is True
    assert h.pool.agent_names == ["a"]
