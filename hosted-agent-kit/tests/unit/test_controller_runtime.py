"""The controller manager: workers, retries, per-key exclusion, events and shutdown."""

from __future__ import annotations

import asyncio
from datetime import UTC, datetime

import pytest

from hosted_agent_kit.adapters.memory_registry import MemorySessionRegistry
from hosted_agent_kit.controllers.runtime import ControllerManager, ReconcileResult
from hosted_agent_kit.domain.enums import FoundrySessionStatus, LocalSessionState
from hosted_agent_kit.domain.models import SessionRecord
from hosted_agent_kit.domain.resources import ResourceKey, ResourceKind, session_key
from hosted_agent_kit.ports.registry import ChangeEvent, ChangeType
from tests.conftest import settle
from tests.fakes.clock import FakeClock

KEY = session_key("a", "s1")


class Probe:
    """A controller whose behaviour a test scripts."""

    name = "probe"

    def __init__(self, workers: int = 1) -> None:
        self.workers = workers
        self.seen: list[ResourceKey] = []
        self.results: list[ReconcileResult | Exception] = []
        self.gate: asyncio.Event | None = None
        self.active = 0
        self.peak = 0
        self.watch = True

    def map_event(self, event: ChangeEvent) -> list[ResourceKey]:
        return [session_key(event.key.agent_name, event.key.session_id)] if self.watch else []

    async def reconcile(self, key: ResourceKey) -> ReconcileResult:
        self.seen.append(key)
        self.active += 1
        self.peak = max(self.peak, self.active)
        try:
            if self.gate is not None:
                await self.gate.wait()
            if self.results:
                outcome = self.results.pop(0)
                if isinstance(outcome, Exception):
                    raise outcome
                return outcome
            return ReconcileResult.done()
        finally:
            self.active -= 1


def rig(workers: int = 1, **kw: float) -> tuple[ControllerManager, Probe, MemorySessionRegistry]:
    clock = FakeClock()
    registry = MemorySessionRegistry(clock)
    manager = ControllerManager(clock, registry, **kw)  # type: ignore[arg-type]
    probe = Probe(workers)
    manager.register(probe)
    return manager, probe, registry


def record(sid: str = "s1") -> SessionRecord:
    return SessionRecord(
        session_id=sid,
        agent_name="a",
        platform_status=FoundrySessionStatus.ACTIVE,
        local_state=LocalSessionState.AVAILABLE,
        created_at=datetime(2026, 1, 1, tzinfo=UTC),
        last_seen_at=datetime(2026, 1, 1, tzinfo=UTC),
    )


async def test_drain_reconciles_queued_keys_inline_when_no_workers_run() -> None:
    manager, probe, _ = rig()
    manager.enqueue("probe", KEY)
    manager.enqueue("probe", KEY)
    await manager.drain()
    assert probe.seen == [KEY]  # deduplicated


async def test_store_changes_queue_keys_for_interested_controllers() -> None:
    manager, probe, registry = rig()
    await registry.add(record("s1"))
    await registry.add(record("s2"))
    await manager.drain()
    assert {k.name for k in probe.seen} == {"s1", "s2"}
    probe.watch = False
    await registry.remove("a", "s1")
    await manager.drain()
    assert len(probe.seen) == 2  # an uninterested controller is not woken


async def test_an_error_is_retried_with_backoff_and_then_succeeds() -> None:
    manager, probe, _ = rig(base_delay=1)
    errors: list[tuple[str, str]] = []
    manager.on_error(lambda controller, key, exc: errors.append((controller, type(exc).__name__)))
    probe.results = [RuntimeError("boom"), ReconcileResult.done()]
    manager.enqueue("probe", KEY)
    await manager.drain()
    assert errors == [("probe", "RuntimeError")] and len(probe.seen) == 1
    clock = manager.queue("probe")._clock
    clock.advance(1)  # type: ignore[attr-defined]
    await manager.drain()  # the retry is due now
    assert len(probe.seen) == 2 and manager.queue("probe").num_requeues(KEY) == 0


async def test_a_controller_that_keeps_failing_is_given_up_on() -> None:
    manager, probe, _ = rig(base_delay=1, max_retries=2)
    probe.results = [RuntimeError("x")] * 10
    manager.enqueue("probe", KEY)
    clock = manager.queue("probe")._clock
    for _ in range(6):
        await manager.drain()
        clock.advance(10)  # type: ignore[attr-defined]
    assert len(probe.seen) == 3  # the first run and two retries, then it stopped
    assert manager.queue("probe").delayed_keys() == []


async def test_requeue_after_waits_and_requeue_retries_with_backoff() -> None:
    manager, probe, _ = rig(base_delay=1)
    probe.results = [ReconcileResult.after(5), ReconcileResult.retry(), ReconcileResult.done()]
    clock = manager.queue("probe")._clock
    manager.enqueue("probe", KEY)
    await manager.drain()
    assert len(probe.seen) == 1
    clock.advance(4)  # type: ignore[attr-defined]
    await manager.drain()
    assert len(probe.seen) == 1  # not yet
    clock.advance(1)  # type: ignore[attr-defined]
    await manager.drain()
    assert len(probe.seen) == 2  # retry asked for backoff
    clock.advance(1)  # type: ignore[attr-defined]
    await manager.drain()
    assert len(probe.seen) == 3


async def test_reconcile_now_runs_in_the_callers_task_and_applies_the_result() -> None:
    manager, probe, _ = rig()
    probe.results = [ReconcileResult.after(30)]
    result = await manager.reconcile_now("probe", KEY)
    assert result.requeue_after == 30 and probe.seen == [KEY]
    assert manager.queue("probe").delayed_keys() == [KEY]


async def test_one_key_is_never_reconciled_twice_at_once() -> None:
    manager, probe, _ = rig()
    probe.gate = asyncio.Event()
    first = asyncio.create_task(manager.reconcile_now("probe", KEY))
    await settle()
    second = asyncio.create_task(manager.reconcile_now("probe", KEY))
    await settle()
    assert probe.peak == 1 and len(probe.seen) == 1  # the second waits for the first
    probe.gate.set()
    await asyncio.gather(first, second)
    assert probe.peak == 1 and len(probe.seen) == 2


async def test_different_keys_run_in_parallel_up_to_the_worker_count() -> None:
    manager, probe, _ = rig(workers=3)
    probe.gate = asyncio.Event()
    await manager.start()
    for index in range(5):
        manager.enqueue("probe", session_key("a", f"s{index}"))
    await settle(20)
    assert probe.peak == 3
    probe.gate.set()
    await manager.drain()  # waits for the workers
    assert len(probe.seen) == 5
    await manager.stop()


async def test_a_key_changed_while_it_runs_is_reconciled_again() -> None:
    manager, probe, _ = rig()
    probe.gate = asyncio.Event()
    await manager.start()
    manager.enqueue("probe", KEY)
    await settle(10)
    manager.enqueue("probe", KEY)  # changed during the run
    probe.gate.set()
    await manager.drain()
    assert len(probe.seen) == 2
    await manager.stop()


async def test_stop_lets_a_running_reconcile_finish_and_cancels_an_overrunning_one() -> None:
    manager, probe, _ = rig()
    probe.gate = asyncio.Event()
    await manager.start()
    manager.enqueue("probe", KEY)
    await settle(10)
    stopping = asyncio.create_task(manager.stop(grace_seconds=5))
    await settle(10)
    assert not stopping.done()  # waiting for the reconcile that is running
    probe.gate.set()
    await asyncio.wait_for(stopping, 2)
    assert not manager.running

    slow, probe2, _ = rig()
    probe2.gate = asyncio.Event()  # never opens
    await slow.start()
    slow.enqueue("probe", KEY)
    await settle(10)
    await slow.stop(grace_seconds=0.05)  # overruns, so it is cancelled
    assert probe2.active == 0 and not slow.running


async def test_a_manager_can_be_started_again_after_it_stopped() -> None:
    manager, probe, _ = rig()
    await manager.start()
    await manager.stop()
    await manager.start()
    manager.enqueue("probe", KEY)
    await manager.drain()
    assert probe.seen == [KEY]
    await manager.stop()
    await manager.start()  # starting twice is harmless
    await manager.start()
    await manager.stop()


async def test_the_manager_works_as_an_async_context_manager() -> None:
    manager, probe, _ = rig()
    async with manager:
        assert manager.running
        manager.enqueue("probe", KEY)
        await manager.drain()
    assert not manager.running and probe.seen == [KEY]


async def test_cancellation_is_not_swallowed_as_a_controller_error() -> None:
    manager, probe, _ = rig()
    probe.gate = asyncio.Event()
    task = asyncio.create_task(manager.reconcile_now("probe", KEY))
    await settle()
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task


def test_result_helpers() -> None:
    assert ReconcileResult.done() == ReconcileResult()
    assert ReconcileResult.retry().requeue and ReconcileResult.after(3).requeue_after == 3


def test_resource_key_text() -> None:
    assert str(KEY) == "AgentSession/a/s1"
    assert str(ResourceKey(ResourceKind.AGENT_POOL, "a")) == "AgentPool/a"


def test_change_events_carry_a_type() -> None:
    assert ChangeType.ADDED.value == "Added"
