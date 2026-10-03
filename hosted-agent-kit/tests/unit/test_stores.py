"""In-memory registry, affinity store and queue manager contracts."""

from __future__ import annotations

import asyncio
from datetime import UTC, datetime, timedelta

import pytest

from hosted_agent_kit.adapters.memory_affinity import MemoryAffinityStore
from hosted_agent_kit.adapters.memory_queue import MemoryQueueManager
from hosted_agent_kit.adapters.memory_registry import MemorySessionRegistry
from hosted_agent_kit.domain.enums import FoundrySessionStatus, LocalSessionState
from hosted_agent_kit.domain.errors import QueueFullError
from hosted_agent_kit.domain.models import SessionAffinityKey, SessionRecord
from hosted_agent_kit.ports.queue import QueueTicket, SessionGrant

T0 = datetime(2026, 1, 1, tzinfo=UTC)


def record(sid: str, agent: str = "a", offset: int = 0, **kwargs: object) -> SessionRecord:
    base = {
        "session_id": sid,
        "agent_name": agent,
        "platform_status": FoundrySessionStatus.ACTIVE,
        "local_state": LocalSessionState.AVAILABLE,
        "created_at": T0 + timedelta(seconds=offset),
        "last_seen_at": T0,
    }
    base.update(kwargs)
    return SessionRecord(**base)  # type: ignore[arg-type]


async def test_registry_add_get_list_count_remove() -> None:
    reg = MemorySessionRegistry()
    await reg.add(record("s2", offset=2))
    await reg.add(record("s1", offset=1))
    await reg.add(record("x", agent="b"))
    assert [r.session_id for r in await reg.list("a")] == ["s1", "s2"]  # stable order
    assert await reg.count("a") == 2 and await reg.count("zz") == 0
    with pytest.raises(ValueError, match="already registered"):
        await reg.add(record("s1"))
    assert (await reg.remove("a", "s1")) is not None
    assert await reg.remove("a", "s1") is None
    assert await reg.get("a", "s1") is None
    assert (await reg.get("a", "s2")) is not None


async def test_same_session_id_under_two_agents_is_two_records() -> None:
    reg = MemorySessionRegistry()
    await reg.add(record("shared", agent="a"))
    await reg.add(record("shared", agent="b"))  # no collision across agents
    assert await reg.try_lease("a", "shared", "r1", T0) is not None
    other = await reg.get("b", "shared")
    assert other is not None and other.local_state is LocalSessionState.AVAILABLE
    assert await reg.remove("a", "shared") is not None
    assert await reg.get("a", "shared") is None and await reg.get("b", "shared") is not None
    assert await reg.get("c", "shared") is None


async def test_registry_returns_copies() -> None:
    reg = MemorySessionRegistry()
    await reg.add(record("s1"))
    snapshot = await reg.get("a", "s1")
    assert snapshot is not None
    snapshot.local_state = LocalSessionState.RETIRING
    again = await reg.get("a", "s1")
    assert again is not None and again.local_state is LocalSessionState.AVAILABLE


async def test_lease_is_exclusive_and_release_is_compare_and_release() -> None:
    reg = MemorySessionRegistry()
    await reg.add(record("s1"))
    assert await reg.try_lease("a", "s1", "r1", T0) is not None
    assert await reg.try_lease("a", "s1", "r2", T0) is None  # mutual exclusion
    assert await reg.try_lease("a", "missing", "r2", T0) is None
    assert await reg.release("a", "s1", "r2", T0) is None  # not the holder
    assert await reg.release("a", "missing", "r1", T0) is None
    released = await reg.release("a", "s1", "r1", T0 + timedelta(seconds=5))
    assert released is not None
    assert released.local_state is LocalSessionState.AVAILABLE
    assert released.last_released_at == T0 + timedelta(seconds=5)
    assert await reg.release("a", "s1", "r1", T0) is None  # double release


async def test_release_keeps_retiring_state() -> None:
    reg = MemorySessionRegistry()
    await reg.add(record("s1"))
    await reg.try_lease("a", "s1", "r1", T0)
    marked = await reg.mark_retiring("a", "s1")
    assert marked is not None and marked.lease_request_id == "r1"
    released = await reg.release("a", "s1", "r1", T0)
    assert released is not None and released.local_state is LocalSessionState.RETIRING
    assert await reg.try_lease("a", "s1", "r2", T0) is None
    assert await reg.mark_retiring("a", "missing") is None


async def test_refresh_set_local_state_and_bind() -> None:
    reg = MemorySessionRegistry()
    await reg.add(record("s1"))
    refreshed = await reg.refresh(
        "a",
        "s1",
        platform_status=FoundrySessionStatus.IDLE,
        agent_version="9",
        last_accessed_at=T0,
        expires_at=T0,
        last_seen_at=T0 + timedelta(seconds=1),
    )
    assert refreshed is not None and refreshed.platform_status is FoundrySessionStatus.IDLE
    assert refreshed.agent_version == "9"
    assert (
        await reg.refresh(
            "a",
            "nope",
            platform_status=FoundrySessionStatus.IDLE,
            agent_version=None,
            last_accessed_at=None,
            expires_at=None,
            last_seen_at=T0,
        )
        is None
    )
    only = frozenset({LocalSessionState.AVAILABLE})
    assert (
        await reg.set_local_state("a", "s1", LocalSessionState.UNAVAILABLE, only_from=only)
    ) is True
    assert (
        await reg.set_local_state("a", "s1", LocalSessionState.AVAILABLE, only_from=only)
    ) is False
    assert (
        await reg.set_local_state("a", "nope", LocalSessionState.AVAILABLE, only_from=only)
    ) is False
    key = SessionAffinityKey(user_id="u", agent_name="a")
    assert (await reg.bind("a", "s1", key)) is True
    assert (await reg.bind("a", "nope", key)) is False
    stored = await reg.get("a", "s1")
    assert stored is not None and stored.affinity_key == key


async def test_slot_reservations_count_against_the_limit() -> None:
    reg = MemorySessionRegistry()
    await reg.add(record("s1"))
    assert (await reg.reserve_slot("a", "t1", 3)) is True
    assert (await reg.reserve_slot("a", "t2", 3)) is True
    assert (await reg.reserve_slot("a", "t3", 3)) is False  # 1 session + 2 reservations
    assert await reg.reserved("a") == 2
    await reg.release_slot("a", "t1")
    await reg.release_slot("a", "unknown")
    await reg.release_slot("never-seen-agent", "t")
    assert (await reg.reserve_slot("a", "t3", 3)) is True
    assert (await reg.reserve_slot("b", "t4", 1)) is True  # other agents are independent


async def test_affinity_remove_by_session_is_scoped_to_the_agent() -> None:
    store = MemoryAffinityStore()
    key_a = SessionAffinityKey(user_id="u", agent_name="a")
    key_b = SessionAffinityKey(user_id="u", agent_name="b")
    await store.bind(key_a, "shared")
    await store.bind(key_b, "shared")
    assert await store.remove_by_session("a", "shared") == [key_a]
    entry = await store.get(key_b)
    assert entry is not None and entry.session_id == "shared"


async def test_affinity_reserve_bind_cancel_remove() -> None:
    store = MemoryAffinityStore()
    key = SessionAffinityKey(user_id="u", agent_name="a")
    other = SessionAffinityKey(user_id="v", agent_name="a")
    assert await store.get(key) is None
    assert (await store.reserve(key, "r1")) is True
    assert (await store.reserve(key, "r2")) is False  # already claimed
    entry = await store.get(key)
    assert entry is not None and entry.pending_request_id == "r1" and entry.session_id is None
    assert (await store.cancel_reservation(key, "r2")) is False
    assert (await store.cancel_reservation(other, "r1")) is False
    assert (await store.cancel_reservation(key, "r1")) is True
    assert await store.get(key) is None
    await store.bind(key, "s1")
    await store.bind(other, "s2")
    assert (await store.reserve(key, "r3")) is False  # bound keys cannot be claimed
    assert (await store.cancel_reservation(key, "r3")) is False
    assert await store.count("a") == 2 and await store.count("b") == 0
    assert await store.remove_by_session("a", "s1") == [key]
    assert await store.remove_by_session("a", "s1") == []
    await store.remove(other)
    await store.remove(other)
    assert await store.count("a") == 0


async def test_pending_claims_are_not_counted_as_bound() -> None:
    store = MemoryAffinityStore()
    await store.reserve(SessionAffinityKey(user_id="u", agent_name="a"), "r")
    assert await store.count("a") == 0


def ticket(request_id: str, agent: str = "a") -> QueueTicket:
    return QueueTicket(
        request_id=request_id,
        agent_name=agent,
        user_id="u",
        enqueued_at=T0,
        pinned=False,
        future=asyncio.get_running_loop().create_future(),
    )


async def test_queue_is_bounded_fifo_and_per_agent() -> None:
    queue = MemoryQueueManager()
    await queue.enqueue(ticket("1"), 2)
    await queue.enqueue(ticket("2"), 2)
    with pytest.raises(QueueFullError):
        await queue.enqueue(ticket("3"), 2)
    await queue.enqueue(ticket("x", "other"), 1)  # independent queue
    assert [t.request_id for t in await queue.pending("a")] == ["1", "2"]
    assert await queue.depth("a") == 2 and await queue.depth("other") == 1
    assert await queue.depth("unknown") == 0


async def test_queue_remove_preserves_order() -> None:
    queue = MemoryQueueManager()
    for rid in ("1", "2", "3"):
        await queue.enqueue(ticket(rid), 10)
    removed = await queue.remove("a", "2")
    assert removed is not None and removed.request_id == "2"
    assert await queue.remove("a", "2") is None
    assert await queue.remove("nope", "1") is None
    assert [t.request_id for t in await queue.pending("a")] == ["1", "3"]


def test_grants_are_value_objects() -> None:
    assert SessionGrant("s") == SessionGrant("s")
