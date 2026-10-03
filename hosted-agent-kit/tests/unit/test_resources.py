"""Resource metadata: conditions, finalizers, versions, change events and conflicts."""

from __future__ import annotations

from datetime import UTC, datetime, timedelta

import pytest

from hosted_agent_kit.adapters.memory_registry import MemorySessionRegistry
from hosted_agent_kit.domain.enums import FoundrySessionStatus as P
from hosted_agent_kit.domain.enums import LocalSessionState as L
from hosted_agent_kit.domain.models import SessionAffinityKey, SessionRecord
from hosted_agent_kit.domain.resources import (
    FINALIZER_PLATFORM_DELETION,
    FINALIZER_SESSION_CLEANUP,
    SESSION_DELETION_FAILED,
    SESSION_DELETION_PENDING,
    SESSION_LEASED,
    SESSION_PROVISIONED,
    SESSION_READY,
    ConditionStatus,
    ResourceKey,
    SessionKey,
    derive_session_conditions,
    drop_condition,
    get_condition,
    set_condition,
)
from hosted_agent_kit.ports.registry import ChangeEvent, ChangeType, ResourceConflictError
from hosted_agent_kit.services.store import mutate_session
from tests.fakes.clock import FakeClock

T0 = datetime(2026, 1, 1, tzinfo=UTC)
TRUE, FALSE = ConditionStatus.TRUE, ConditionStatus.FALSE


def rec(sid: str = "s1", agent: str = "a", **kw: object) -> SessionRecord:
    base: dict[str, object] = {
        "session_id": sid,
        "agent_name": agent,
        "platform_status": P.ACTIVE,
        "local_state": L.AVAILABLE,
        "created_at": T0,
        "last_seen_at": T0,
    }
    base.update(kw)
    return SessionRecord(**base)  # type: ignore[arg-type]


# -------------------------------------------------------------------- conditions


def test_a_condition_keeps_its_transition_time_until_the_status_changes() -> None:
    first = set_condition([], "Ready", TRUE, "Ok", now=T0)
    later = T0 + timedelta(seconds=30)
    same_status = set_condition(first, "Ready", TRUE, "StillOk", "detail", now=later)
    cond = get_condition(same_status, "Ready")
    assert cond is not None and cond.reason == "StillOk" and cond.last_transition_time == T0
    flipped = set_condition(same_status, "Ready", FALSE, "Broken", now=later)
    cond = get_condition(flipped, "Ready")
    assert cond is not None and cond.last_transition_time == later


def test_setting_an_identical_condition_returns_the_same_list() -> None:
    first = set_condition([], "Ready", TRUE, "Ok", "m", now=T0)
    assert set_condition(first, "Ready", TRUE, "Ok", "m", now=T0 + timedelta(hours=1)) is first


def test_conditions_can_be_dropped_and_looked_up() -> None:
    conditions = set_condition(set_condition([], "A", TRUE, "x", now=T0), "B", FALSE, "y", now=T0)
    assert get_condition(conditions, "B") is not None and get_condition(conditions, "C") is None
    assert [c.type for c in drop_condition(conditions, "A")] == ["B"]


def test_session_conditions_follow_the_state() -> None:
    conditions = {c.type: c for c in derive_session_conditions(rec(), T0)}
    assert (
        conditions[SESSION_READY].status is TRUE and conditions[SESSION_PROVISIONED].status is TRUE
    )
    assert conditions[SESSION_LEASED].status is FALSE
    busy = {
        c.type: c
        for c in derive_session_conditions(rec(local_state=L.LEASED, lease_request_id="r"), T0)
    }
    assert busy[SESSION_LEASED].status is TRUE and busy[SESSION_READY].status is TRUE
    starting = {c.type: c for c in derive_session_conditions(rec(platform_status=P.CREATING), T0)}
    assert starting[SESSION_PROVISIONED].reason == "Provisioning"
    assert starting[SESSION_READY].status is FALSE
    failed = {c.type: c for c in derive_session_conditions(rec(platform_status=P.FAILED), T0)}
    assert failed[SESSION_PROVISIONED].reason == "Failed"
    other = {c.type: c for c in derive_session_conditions(rec(platform_status=P.DELETING), T0)}
    assert other[SESSION_PROVISIONED].reason == "NotActive"
    deleting = {
        c.type: c
        for c in derive_session_conditions(rec(local_state=L.RETIRING, deletion_timestamp=T0), T0)
    }
    assert deleting[SESSION_DELETION_PENDING].status is TRUE
    assert deleting[SESSION_READY].reason == "Deleting"


def test_a_deletion_failure_is_forgotten_once_deletion_is_no_longer_pending() -> None:
    held = rec(deletion_timestamp=T0)
    held.conditions = set_condition([], SESSION_DELETION_FAILED, TRUE, "x", now=T0)
    assert get_condition(derive_session_conditions(held, T0), SESSION_DELETION_FAILED) is not None
    held.deletion_timestamp = None
    assert get_condition(derive_session_conditions(held, T0), SESSION_DELETION_FAILED) is None


def test_keys_have_readable_text() -> None:
    assert str(SessionKey(agent_name="a", session_id="s")) == "a/s"
    assert rec().key == SessionKey(agent_name="a", session_id="s1")
    assert str(ResourceKey.__mro__[0].__name__) == "ResourceKey"


# ---------------------------------------------------------------- store versioning


async def test_every_change_bumps_the_resource_version_and_refreshes_conditions() -> None:
    reg = MemorySessionRegistry(FakeClock())
    await reg.add(rec())
    stored = await reg.get("a", "s1")
    assert stored is not None and stored.resource_version == 1
    assert get_condition(stored.conditions, SESSION_READY) is not None
    leased = await reg.try_lease("a", "s1", "r1", T0)
    assert leased is not None and leased.resource_version == 2
    assert get_condition(leased.conditions, SESSION_LEASED).status is TRUE  # type: ignore[union-attr]
    released = await reg.release("a", "s1", "r1", T0)
    assert released is not None and released.resource_version == 3


async def test_a_heartbeat_is_not_a_change() -> None:
    reg = MemorySessionRegistry(FakeClock())
    await reg.add(rec())
    events: list[ChangeEvent] = []
    reg.subscribe(events.append)

    async def refresh(status: P, seen: int) -> None:
        await reg.refresh(
            "a",
            "s1",
            platform_status=status,
            agent_version=None,
            last_accessed_at=None,
            expires_at=None,
            last_seen_at=T0 + timedelta(seconds=seen),
        )

    await refresh(P.ACTIVE, 10)
    await refresh(P.ACTIVE, 20)
    assert events == []  # only last_seen_at moved
    stored = await reg.get("a", "s1")
    assert stored is not None and stored.resource_version == 1
    assert stored.last_seen_at == T0 + timedelta(seconds=20)
    await refresh(P.IDLE, 30)
    assert len(events) == 1 and events[0].type is ChangeType.MODIFIED


async def test_changes_are_announced_with_a_snapshot() -> None:
    reg = MemorySessionRegistry(FakeClock())
    events: list[ChangeEvent] = []
    reg.subscribe(events.append)
    await reg.add(rec())
    await reg.try_lease("a", "s1", "r", T0)
    await reg.remove("a", "s1")
    assert [e.type for e in events] == [ChangeType.ADDED, ChangeType.MODIFIED, ChangeType.DELETED]
    assert events[1].record.local_state is L.LEASED
    assert events[2].key == SessionKey(agent_name="a", session_id="s1")
    events[1].record.local_state = L.RETIRING  # a snapshot: it cannot change the store
    assert (await reg.get("a", "s1")) is None


async def test_generation_changes_only_when_the_desired_state_changes() -> None:
    reg = MemorySessionRegistry(FakeClock())
    await reg.add(rec())
    await reg.try_lease("a", "s1", "r", T0)  # status-like: not desired state
    assert (await reg.get("a", "s1")).generation == 1  # type: ignore[union-attr]
    key = SessionAffinityKey(user_id="u", agent_name="a")
    await reg.bind("a", "s1", key)
    assert (await reg.get("a", "s1")).generation == 2  # type: ignore[union-attr]
    await reg.bind("a", "s1", key)  # binding again changes nothing
    stored = await reg.get("a", "s1")
    assert stored is not None and stored.generation == 2 and stored.resource_version == 3
    await reg.mark_retiring("a", "s1")
    stored = await reg.get("a", "s1")
    assert stored is not None and stored.generation == 3


# ----------------------------------------------------------- finalizers and deletion


async def test_marking_for_deletion_sets_the_timestamp_reason_and_finalizers() -> None:
    clock = FakeClock()
    reg = MemorySessionRegistry(clock)
    await reg.add(rec())
    marked = await reg.mark_retiring("a", "s1", reason="failed")
    assert marked is not None
    assert marked.local_state is L.RETIRING and marked.deletion_timestamp == clock.now()
    assert marked.deletion_reason == "failed" and marked.finalizers == [FINALIZER_SESSION_CLEANUP]
    clock.advance(60)
    again = await reg.mark_retiring("a", "s1", reason="other")
    assert again is not None
    assert again.deletion_timestamp == marked.deletion_timestamp  # the first request stands
    assert again.deletion_reason == "failed" and again.resource_version == marked.resource_version
    assert await reg.mark_retiring("a", "missing") is None


async def test_without_cleanup_there_is_no_cleanup_finalizer_and_others_can_be_added() -> None:
    reg = MemorySessionRegistry(FakeClock())
    await reg.add(rec())
    marked = await reg.mark_retiring(
        "a", "s1", cleanup=False, finalizers=(FINALIZER_PLATFORM_DELETION,)
    )
    assert marked is not None and marked.finalizers == [FINALIZER_PLATFORM_DELETION]
    removed = await reg.remove_finalizer("a", "s1", FINALIZER_PLATFORM_DELETION)
    assert removed is not None and removed.finalizers == []
    assert (await reg.remove_finalizer("a", "s1", "never-set")) is not None
    assert await reg.remove_finalizer("a", "missing", "x") is None


async def test_conditions_can_be_set_without_a_controller_reacting() -> None:
    reg = MemorySessionRegistry(FakeClock())
    await reg.add(rec())
    await reg.mark_retiring("a", "s1")
    events: list[ChangeEvent] = []
    reg.subscribe(events.append)
    await reg.set_condition("a", "s1", SESSION_DELETION_FAILED, TRUE, "RemoteDeleteFailed", "m")
    await reg.set_condition("a", "s1", SESSION_DELETION_FAILED, TRUE, "RemoteDeleteFailed", "m")
    assert len(events) == 1 and events[0].status_only  # a status update, and only once
    stored = await reg.get("a", "s1")
    assert stored is not None and get_condition(stored.conditions, SESSION_DELETION_FAILED)
    assert await reg.set_condition("a", "nope", "X", TRUE, "r") is None


# ------------------------------------------------------ optimistic concurrency


async def test_an_update_with_a_stale_version_is_refused() -> None:
    reg = MemorySessionRegistry(FakeClock())
    await reg.add(rec())
    mine = await reg.get("a", "s1")
    assert mine is not None
    await reg.try_lease("a", "s1", "someone-else", T0)  # the store moves on
    mine.affinity_key = SessionAffinityKey(user_id="u", agent_name="a")
    with pytest.raises(ResourceConflictError):
        await reg.update(mine, expected_version=mine.resource_version)
    current = await reg.get("a", "s1")
    assert current is not None and current.affinity_key is None  # nothing was overwritten


async def test_a_current_update_is_stored_and_versioned() -> None:
    reg = MemorySessionRegistry(FakeClock())
    await reg.add(rec())
    mine = await reg.get("a", "s1")
    assert mine is not None
    mine.agent_version = "9"
    mine.affinity_key = SessionAffinityKey(user_id="u", agent_name="a")
    stored = await reg.update(mine, expected_version=1)
    assert stored.resource_version == 2 and stored.agent_version == "9"
    assert stored.generation == 2  # the binding is desired state
    ghost = rec("ghost")
    with pytest.raises(ResourceConflictError):
        await reg.update(ghost, expected_version=1)


async def test_mutate_session_retries_when_another_writer_gets_there_first() -> None:
    reg = MemorySessionRegistry(FakeClock())
    await reg.add(rec())
    calls = 0

    def change(record: SessionRecord) -> None:
        nonlocal calls
        calls += 1
        record.agent_version = f"v{calls}"
        if calls == 1:  # a competing writer lands between our read and our write
            reg._records[("a", "s1")].resource_version += 1

    stored = await mutate_session(reg, "a", "s1", change)
    assert stored is not None and calls == 2 and stored.agent_version == "v2"
    assert await mutate_session(reg, "a", "missing", lambda r: None) is None


async def test_mutate_session_gives_up_when_the_record_never_settles() -> None:
    reg = MemorySessionRegistry(FakeClock())
    await reg.add(rec())

    def always_loses(record: SessionRecord) -> None:
        reg._records[("a", "s1")].resource_version += 1

    with pytest.raises(ResourceConflictError):
        await mutate_session(reg, "a", "s1", always_loses, attempts=3)


# ---------------------------------------------------------------- capacity slots


async def test_slots_can_be_forced_and_unused_ones_expire() -> None:
    clock = FakeClock()
    reg = MemorySessionRegistry(clock)
    assert await reg.reserve_slot("a", "t1", 1)
    assert not await reg.reserve_slot("a", "t2", 1)  # full
    assert await reg.reserve_slot("a", "t2", 1, force=True)  # a replacement takes its place
    assert await reg.reserved("a") == 2
    clock.advance(100)
    assert await reg.reserve_slot("a", "t3", 5)
    assert await reg.release_expired_slots("a", 50) == 2  # t1 and t2 are old, t3 is fresh
    assert await reg.reserved("a") == 1
    assert await reg.release_expired_slots("a", 50) == 0
    assert await reg.release_expired_slots("unknown-agent", 50) == 0
