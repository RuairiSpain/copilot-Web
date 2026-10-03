"""The controllers: session cleanup, garbage collection, pool capacity, status and health."""

from __future__ import annotations

import asyncio
from typing import Any

import pytest

from hosted_agent_kit.adapters.circuit_breaking import CircuitBreakingAdapter
from hosted_agent_kit.adapters.memory_affinity import MemoryAffinityStore
from hosted_agent_kit.adapters.memory_queue import MemoryQueueManager
from hosted_agent_kit.adapters.memory_registry import MemorySessionRegistry
from hosted_agent_kit.controllers.reports import ReconcileReport
from hosted_agent_kit.domain.enums import FoundrySessionStatus as P
from hosted_agent_kit.domain.enums import LocalSessionState as L
from hosted_agent_kit.domain.errors import FoundryRejected, FoundryUnavailable
from hosted_agent_kit.domain.models import SessionAffinityKey, SessionRecord
from hosted_agent_kit.domain.resources import (
    FINALIZER_PLATFORM_DELETION,
    FINALIZER_SESSION_CLEANUP,
    POOL_CAPACITY_AVAILABLE,
    POOL_CIRCUIT_OPEN,
    POOL_FOUNDRY_REACHABLE,
    POOL_INITIAL_SYNC_COMPLETE,
    POOL_READY,
    POOL_RECOVERY_BLOCKED,
    SESSION_DELETION_FAILED,
    ConditionStatus,
    get_condition,
    pool_key,
    session_key,
)
from hosted_agent_kit.services.circuit_breaker import CircuitBreakers
from hosted_agent_kit.services.events import EventType
from hosted_agent_kit.services.metrics import GatedMetrics, InMemoryMetrics
from hosted_agent_kit.services.pool import PoolService
from hosted_agent_kit.services.reconciler import Reconciler
from hosted_agent_kit.services.session_ids import SessionIdDeriver
from tests.conftest import Harness, make_config, make_harness, make_request, make_settings, settle
from tests.fakes.clock import FakeClock
from tests.fakes.foundry import FakeFoundry

AGENT = "stateless-agent"
TRUE, FALSE, UNKNOWN = ConditionStatus.TRUE, ConditionStatus.FALSE, ConditionStatus.UNKNOWN


def condition(h: Harness, agent: str, type_: str) -> Any:
    return h.plane.pools.condition(agent, type_)


def reasons(h: Harness, agent: str | None = None) -> list[str]:
    return [e.reason for e in h.plane.events.list(agent)]


# ============================================================= session controller


async def test_a_deleted_session_loses_its_remote_resource_before_its_record() -> None:
    h = make_harness()
    await h.pool.execute(make_request())
    sid = h.fake.created[0]
    assert await h.pool.retire_session(AGENT, sid, "admin", delete_remote=True) is True
    assert h.fake.deleted == [sid] and await h.registry.get(AGENT, sid) is None
    assert "Deleted" in reasons(h, AGENT)


async def test_a_failed_delete_keeps_the_finalizer_the_condition_and_an_event() -> None:
    h = make_harness(delete_retries=0)
    await h.pool.execute(make_request())
    sid = h.fake.created[0]
    h.fake.delete_errors = [FoundryRejected(403)]
    assert await h.pool.retire_session(AGENT, sid, "admin", delete_remote=True) is False
    record = await h.registry.get(AGENT, sid)
    assert record is not None
    assert record.finalizers == [FINALIZER_SESSION_CLEANUP] and record.deletion_timestamp
    failed = get_condition(record.conditions, SESSION_DELETION_FAILED)
    assert failed is not None and failed.status is TRUE and failed.reason == "RemoteDeleteFailed"
    assert "DeleteFailed" in reasons(h) and h.plane.events.list()[0].type is EventType.WARNING
    # The delete is retried (here by the next sync) and the record goes once Foundry confirms.
    await h.reconciler.run_agent(AGENT)
    assert await h.registry.get(AGENT, sid) is None and h.fake.deleted == [sid]


async def test_a_session_in_use_is_deleted_when_its_lease_ends_not_before() -> None:
    h = make_harness()
    h.fake.invoke_gate = asyncio.Event()
    task = asyncio.create_task(h.pool.execute(make_request()))
    await settle()
    sid = h.fake.created[0]
    assert await h.pool.retire_session(AGENT, sid, "admin", delete_remote=True) is None
    record = await h.registry.get(AGENT, sid)
    assert record is not None and record.deletion_timestamp and record.lease_request_id
    await h.reconciler.manager.drain()  # the controller sees it is leased and leaves it
    assert h.fake.deleted == []
    h.fake.invoke_gate.set()
    await task
    assert h.fake.deleted == [sid] and await h.registry.get(AGENT, sid) is None


async def test_nothing_is_deleted_remotely_for_a_session_that_is_already_gone() -> None:
    h = make_harness()
    await h.pool.execute(make_request())
    sid = h.fake.created[0]
    h.fake.sessions.pop(sid)
    await h.reconciler.run_agent(AGENT)  # a complete listing and get both say it is gone
    assert await h.registry.get(AGENT, sid) is None and h.fake.deleted == []


async def test_a_session_foundry_is_deleting_is_kept_until_it_is_gone() -> None:
    h = make_harness()
    session = h.fake.add_session(AGENT, P.DELETING)
    await h.reconciler.run_agent(AGENT)
    record = await h.registry.get(AGENT, session.session_id)
    assert record is not None and record.local_state is L.RETIRING
    assert record.finalizers == [FINALIZER_PLATFORM_DELETION]
    assert h.fake.deleted == []  # Foundry is doing the deleting
    await h.reconciler.run_agent(AGENT)  # still deleting: it is not adopted a second time
    assert await h.registry.count(AGENT) == 1
    h.fake.set_status(session.session_id, P.DELETED)
    await h.reconciler.run_agent(AGENT)
    assert await h.registry.get(AGENT, session.session_id) is None


async def test_the_session_controller_ignores_status_updates_and_other_kinds() -> None:
    h = make_harness()
    await h.pool.execute(make_request())
    sid = h.fake.created[0]
    controller = h.pool.plane.manager._controllers["session"]
    record = await h.registry.get(AGENT, sid)
    assert record is not None
    from hosted_agent_kit.ports.registry import ChangeEvent, ChangeType

    plain = ChangeEvent(ChangeType.MODIFIED, record.key, record)
    assert controller.map_event(plain) == []  # not marked for deletion
    marked = record.model_copy(update={"deletion_timestamp": h.clock.now()})
    assert controller.map_event(ChangeEvent(ChangeType.MODIFIED, record.key, marked))
    assert not controller.map_event(
        ChangeEvent(ChangeType.MODIFIED, record.key, marked, status_only=True)
    )
    assert not controller.map_event(ChangeEvent(ChangeType.DELETED, record.key, marked))
    result = await controller.reconcile(pool_key(AGENT))
    assert result.requeue is False and result.requeue_after is None
    assert (await controller.reconcile(session_key("ghost", "x"))).requeue is False


# =========================================================== garbage collection


def raw_record(sid: str, **kw: Any) -> SessionRecord:
    base: dict[str, Any] = {
        "session_id": sid,
        "agent_name": AGENT,
        "platform_status": P.CREATING,
        "local_state": L.UNAVAILABLE,
        "created_at": FakeClock().now(),
        "last_seen_at": FakeClock().now(),
    }
    base.update(kw)
    return SessionRecord(**base)


async def test_a_session_that_never_became_usable_is_given_up_after_a_while() -> None:
    h = make_harness(create_ready_timeout_seconds=10)
    session = h.fake.add_session(AGENT, P.CREATING)
    await h.registry.add(
        raw_record(session.session_id, provisioning_started_at=h.clock.now(), agent_version="1")
    )
    await h.reconciler.run_agent(AGENT)
    assert await h.registry.get(AGENT, session.session_id) is not None  # not yet
    h.clock.advance(21)  # longer than twice the ready timeout
    await h.reconciler.run_agent(AGENT)
    assert session.session_id in h.fake.deleted
    assert "ProvisioningStuck" in reasons(h, AGENT)


async def test_a_session_someone_is_waiting_for_is_not_given_up() -> None:
    h = make_harness(create_ready_timeout_seconds=10)
    session = h.fake.add_session(AGENT, P.CREATING)
    await h.registry.add(
        raw_record(
            session.session_id,
            provisioning_started_at=h.clock.now(),
            local_state=L.LEASED,
            lease_request_id="r1",
        )
    )
    h.clock.advance(100)
    await h.reconciler.run_agent(AGENT)
    assert h.fake.deleted == []


async def test_an_affinity_entry_for_a_session_that_is_gone_is_removed() -> None:
    h = make_harness({"coding-agent": {"mode": "stateful", "max_sessions": 3}}, defaults={})
    key = SessionAffinityKey(user_id="u", agent_name="coding-agent")
    await h.affinity.bind(key, "ghost")
    pending = SessionAffinityKey(user_id="p", agent_name="coding-agent")
    await h.affinity.reserve(pending, "r1")
    await h.reconciler.run_agent("coding-agent")
    assert await h.affinity.get(key) is None
    assert await h.affinity.get(pending) is not None  # a claim in progress is not stale


async def test_capacity_reservations_nobody_used_are_expired() -> None:
    h = make_harness(reservation_ttl_seconds=60)
    assert await h.registry.reserve_slot(AGENT, "t1", 5)
    await h.reconciler.run_agent(AGENT)
    assert await h.registry.reserved(AGENT) == 1  # still fresh
    h.clock.advance(61)
    await h.reconciler.run_agent(AGENT)
    assert await h.registry.reserved(AGENT) == 0
    assert "ReservationsExpired" in reasons(h, AGENT)


# ================================================================ pool controller


async def test_the_pool_controller_does_nothing_before_the_first_observation() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 3, "min_warm_sessions": 2})
    await h.reconciler.manager.reconcile_now("pool", pool_key(AGENT))
    assert h.fake.created == []  # no view of Foundry yet, so nothing is created


async def test_a_lost_warm_session_is_replaced_without_waiting_for_the_next_sync() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 3, "min_warm_sessions": 2})
    await h.reconciler.run_agent(AGENT)
    assert len(h.fake.created) == 2
    victim = h.fake.created[0]
    await h.registry.remove(AGENT, victim)  # the store announces the loss
    h.fake.sessions.pop(victim)
    await h.reconciler.manager.drain()
    assert len(h.fake.created) == 3 and await h.registry.count(AGENT) == 2


async def test_a_failure_to_create_a_warm_session_is_reported_as_an_event() -> None:
    h = make_harness(
        defaults={
            "mode": "stateless",
            "min_warm_sessions": 1,
            "max_sessions": 2,
            "create_retries": 0,
        }
    )
    h.fake.create_errors = [FoundryUnavailable("down")]
    report = await h.reconciler.run_agent(AGENT)
    assert "FOUNDRY_UNAVAILABLE" in report.errors
    assert "WarmProvisioningFailed" in reasons(h, AGENT)


# ================================================================== observation


async def test_the_observer_records_what_it_found_and_why() -> None:
    h = make_harness({"coding-agent": {"mode": "stateful", "max_sessions": 3}}, defaults={})
    foreign = h.fake.add_session("coding-agent")
    await h.reconciler.run_agent("coding-agent")
    assert "Ignored" in reasons(
        h, "coding-agent"
    )  # unknown sessions of stateful agents are not adopted
    assert await h.registry.get("coding-agent", foreign.session_id) is None
    seen = h.reconciler.cache.get("coding-agent")
    assert seen is not None and seen.complete and seen.completed_at is not None
    assert seen.statuses == {foreign.session_id: P.ACTIVE} and seen.consecutive_failures == 0


async def test_an_adopted_session_and_a_status_change_are_recorded() -> None:
    h = make_harness()
    session = h.fake.add_session(AGENT, P.CREATING)
    await h.reconciler.run_agent(AGENT)
    assert "Adopted" in reasons(h, AGENT)
    h.fake.set_status(session.session_id, P.ACTIVE)
    await h.reconciler.run_agent(AGENT)
    changed = [e for e in h.plane.events.list(AGENT) if e.reason == "StatusChanged"]
    assert changed and "creating" in changed[0].message and "active" in changed[0].message


async def test_a_listing_failure_is_remembered_and_nothing_is_deleted_because_of_it() -> None:
    h = make_harness()
    await h.pool.execute(make_request())
    sid = h.fake.created[0]
    h.fake.list_fail_after = 0
    h.fake.list_error = FoundryUnavailable("page 1 failed")
    report = await h.reconciler.run_agent(AGENT)
    seen = h.reconciler.cache.get(AGENT)
    assert report.outcome == "incomplete"
    assert seen is not None and not seen.complete and seen.consecutive_failures == 1
    assert "ObservationIncomplete" in reasons(h, AGENT)
    assert await h.registry.get(AGENT, sid) is not None
    await h.reconciler.run_agent(AGENT)
    assert h.reconciler.cache.get(AGENT).consecutive_failures == 2  # type: ignore[union-attr]
    h.fake.list_fail_after = None
    await h.reconciler.run_agent(AGENT)
    assert h.reconciler.cache.get(AGENT).consecutive_failures == 0  # type: ignore[union-attr]


async def test_an_unexpected_controller_error_is_reported_on_the_sync() -> None:
    h = make_harness()

    async def broken(agent: str) -> Any:
        raise RuntimeError("boom")
        yield  # pragma: no cover

    h.fake.list_sessions = broken  # type: ignore[method-assign]
    report = await h.reconciler.run_agent(AGENT)
    assert report.outcome == "error" and "RuntimeError" in report.errors


# =============================================================== status controller


async def test_a_pool_reports_its_counts_conditions_and_applied_generation() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 4, "min_warm_sessions": 2})
    before = h.plane.pools.get(AGENT)
    assert before.status.observed_generation == 0
    assert condition(h, AGENT, POOL_READY).status is UNKNOWN
    assert condition(h, AGENT, POOL_INITIAL_SYNC_COMPLETE).status is FALSE
    await h.reconciler.run_agent(AGENT)
    pool = h.plane.pools.get(AGENT)
    assert pool.status.observed_generation == pool.generation == 1
    assert pool.status.ready_sessions == 2 and pool.status.leased_sessions == 0
    assert condition(h, AGENT, POOL_READY).status is TRUE
    assert condition(h, AGENT, POOL_INITIAL_SYNC_COMPLETE).status is TRUE
    assert condition(h, AGENT, POOL_FOUNDRY_REACHABLE).status is TRUE
    assert condition(h, AGENT, POOL_CAPACITY_AVAILABLE).status is TRUE
    assert condition(h, AGENT, POOL_RECOVERY_BLOCKED).status is FALSE


async def test_a_pool_that_is_full_says_so() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    await h.reconciler.run_agent(AGENT)
    h.fake.invoke_gate = asyncio.Event()
    task = asyncio.create_task(h.pool.execute(make_request()))
    await settle()
    await h.reconciler.manager.drain()
    status = h.plane.pools.get(AGENT).status
    assert status.leased_sessions == 1
    full = condition(h, AGENT, POOL_CAPACITY_AVAILABLE)
    assert full.status is FALSE and full.reason == "AtCapacity"
    h.fake.invoke_gate.set()
    await task
    await h.reconciler.manager.drain()
    assert condition(h, AGENT, POOL_CAPACITY_AVAILABLE).status is TRUE


async def test_ready_names_the_first_thing_that_is_wrong() -> None:
    h = make_harness()
    await h.reconciler.manager.reconcile_now("status", pool_key(AGENT))
    ready = condition(h, AGENT, POOL_READY)
    assert ready.status is FALSE and ready.reason == "AwaitingSync"
    h.fake.list_fail_after = 0
    h.fake.list_error = FoundryUnavailable("x")
    await h.reconciler.run_agent(AGENT)
    ready = condition(h, AGENT, POOL_READY)
    assert ready.status is FALSE and ready.reason == "AwaitingSync"  # never listed completely
    h.fake.list_fail_after = None
    await h.reconciler.run_agent(AGENT)
    assert condition(h, AGENT, POOL_READY).status is TRUE
    assert any(e.reason == "ConditionChanged" for e in h.plane.events.list(AGENT))


async def test_recovery_is_blocked_until_foundry_has_been_listed() -> None:
    deriver = SessionIdDeriver(b"k" * 32)
    h = make_harness(
        {"coding-agent": {"mode": "stateful", "max_sessions": 3}},
        defaults={},
        session_ids=deriver,
    )
    await h.reconciler.manager.reconcile_now("status", pool_key("coding-agent"))
    blocked = condition(h, "coding-agent", POOL_RECOVERY_BLOCKED)
    assert blocked.status is TRUE and blocked.reason == "AwaitingObservation"
    await h.reconciler.run_agent("coding-agent")
    assert condition(h, "coding-agent", POOL_RECOVERY_BLOCKED).status is FALSE


async def test_a_configuration_change_shows_as_a_newer_generation_until_it_is_applied() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 4})
    await h.reconciler.run_agent(AGENT)
    new = make_config({AGENT: {"mode": "stateless", "max_sessions": 7}}, {})
    changed = h.pool.holder.replace(new)
    assert changed == [AGENT] and h.pool.holder.generation(AGENT) == 2
    assert h.plane.pools.get(AGENT).generation == 1  # not noticed yet
    await h.reconciler.manager.reconcile_now("status", pool_key(AGENT))
    pool = h.plane.pools.get(AGENT)
    assert pool.generation == pool.status.observed_generation == 2
    assert pool.spec.max_sessions == 7 and "SpecChanged" in reasons(h, AGENT)
    assert h.plane.pools.sync_spec(AGENT) is False  # nothing further to apply


# ================================================================ dependency health


def with_breaker(threshold: int = 2) -> tuple[Harness, CircuitBreakers, Any]:
    """A harness whose Foundry calls go through real circuit breakers."""
    config = make_config(
        {AGENT: {"mode": "stateless", "circuit_breaker": {"failure_threshold": threshold}}}
    )
    fake = FakeFoundry(FakeClock())
    clock = fake.clock
    inner = InMemoryMetrics()
    metrics = GatedMetrics([inner], lambda agent: True)
    circuits = CircuitBreakers(config, clock, metrics)
    guarded = CircuitBreakingAdapter(fake, circuits)
    registry = MemorySessionRegistry(clock)
    affinity = MemoryAffinityStore()
    queue = MemoryQueueManager()
    settings = make_settings()
    pool = PoolService(
        config=config,
        adapter=guarded,
        registry=registry,
        affinity=affinity,
        queue=queue,
        metrics=metrics,
        clock=clock,
        settings=settings,
    )
    reconciler = Reconciler(
        config=config,
        adapter=guarded,
        registry=registry,
        pool=pool,
        metrics=metrics,
        clock=clock,
        circuits=circuits,
    )
    harness = Harness(
        pool=pool,
        reconciler=reconciler,
        fake=fake,
        registry=registry,
        affinity=affinity,
        queue=queue,
        metrics=inner,
        gated=metrics,
        clock=clock,
        config=config,
        settings=settings,
        plane=pool.plane,
    )
    return harness, circuits, guarded


async def test_an_open_circuit_is_recorded_as_a_condition_and_closes_again() -> None:
    h, circuits, _ = with_breaker(threshold=2)
    await h.reconciler.run_agent(AGENT)
    assert condition(h, AGENT, POOL_FOUNDRY_REACHABLE).status is TRUE
    assert condition(h, AGENT, POOL_CIRCUIT_OPEN).status is FALSE
    h.fake.list_error = FoundryUnavailable("down")
    h.fake.list_fail_after = 0
    for _ in range(2):
        await h.reconciler.run_agent(AGENT)
    assert circuits.state(AGENT) == "open"
    await h.reconciler.manager.drain()
    assert condition(h, AGENT, POOL_CIRCUIT_OPEN).status is TRUE
    reachable = condition(h, AGENT, POOL_FOUNDRY_REACHABLE)
    assert reachable.status is FALSE and reachable.reason == "CircuitOpen"
    assert condition(h, AGENT, POOL_READY).status is FALSE
    assert any(
        e.type is EventType.WARNING and "CircuitOpen" in e.message
        for e in h.plane.events.list(AGENT)
    )
    # Recovery: the circuit lets a probe through after its open period, and the listing works.
    h.clock.advance(60)
    h.fake.list_fail_after = None
    await h.reconciler.run_agent(AGENT)
    await h.reconciler.manager.drain()
    assert circuits.state(AGENT) == "closed"
    assert condition(h, AGENT, POOL_CIRCUIT_OPEN).status is FALSE
    assert condition(h, AGENT, POOL_FOUNDRY_REACHABLE).status is TRUE
    assert condition(h, AGENT, POOL_READY).status is TRUE


async def test_failing_listings_make_foundry_unreachable_before_the_circuit_opens() -> None:
    h, _, _ = with_breaker(threshold=10)
    h.fake.list_error = FoundryUnavailable("down")
    h.fake.list_fail_after = 0
    await h.reconciler.run_agent(AGENT)
    reachable = condition(h, AGENT, POOL_FOUNDRY_REACHABLE)
    assert reachable.status is FALSE and reachable.reason == "ListingFailed"
    assert "1 listing" in reachable.message


async def test_without_a_breaker_health_still_reports_from_the_observer() -> None:
    h = make_harness()
    await h.reconciler.manager.reconcile_now("health", pool_key(AGENT))
    assert condition(h, AGENT, POOL_FOUNDRY_REACHABLE).status is UNKNOWN
    assert condition(h, AGENT, POOL_CIRCUIT_OPEN).status is FALSE
    result = await h.reconciler.health.reconcile(session_key(AGENT, "x"))
    assert result.requeue is False  # another kind of key is not its business


# ================================================================== the report


def test_a_report_starts_empty() -> None:
    report = ReconcileReport(agent_name="a")
    assert report.outcome == "success" and report.drained == 0 and report.attempted == set()


@pytest.mark.parametrize("controller", ["observation", "gc", "pool", "status", "health"])
async def test_controllers_ignore_keys_for_unknown_agents(controller: str) -> None:
    h = make_harness()
    result = await h.reconciler.manager.reconcile_now(controller, pool_key("ghost"))
    assert result.requeue is False and result.requeue_after is None


# ============================================ retries and triggers between syncs


async def test_a_failed_delete_is_scheduled_for_a_retry_without_waiting_for_a_sync() -> None:
    h = make_harness(delete_retries=0)
    await h.pool.execute(make_request())
    sid = h.fake.created[0]
    h.fake.delete_errors = [FoundryRejected(403), FoundryRejected(403)]
    assert await h.pool.retire_session(AGENT, sid, "admin", delete_remote=True) is False
    queue = h.reconciler.manager.queue("session")
    key = session_key(AGENT, sid)
    assert key in queue.delayed_keys()  # backoff: the controller asked to be called again
    # The store change also queued the key, so the controller tries once more at once and fails.
    await h.reconciler.manager.drain()
    assert await h.registry.get(AGENT, sid) is not None and h.fake.deleted == []
    h.clock.advance(10)  # past the backoff
    await h.reconciler.manager.drain()
    assert await h.registry.get(AGENT, sid) is None and h.fake.deleted == [sid]


async def test_a_circuit_change_refreshes_readiness_without_waiting_for_a_sync() -> None:
    h, circuits, _ = with_breaker(threshold=2)
    await h.reconciler.run_agent(AGENT)
    assert condition(h, AGENT, POOL_READY).status is TRUE
    breaker = circuits.get(AGENT)
    assert breaker is not None
    breaker.failure(False)
    breaker.failure(False)  # the circuit opens, and tells the health controller
    await h.reconciler.manager.drain()
    assert condition(h, AGENT, POOL_CIRCUIT_OPEN).status is TRUE
    assert condition(h, AGENT, POOL_READY).status is FALSE  # status followed health
    assert condition(h, AGENT, POOL_READY).reason == "FoundryUnreachable"
