"""Reconciliation: upsert, failure handling, verification and warm maintenance."""

from __future__ import annotations

import asyncio
import logging

import pytest

from hosted_agent_kit.domain.enums import FoundrySessionStatus as S
from hosted_agent_kit.domain.enums import LocalSessionState as L
from hosted_agent_kit.domain.errors import (
    FoundryUnavailable,
    SyncInProgressError,
    UpstreamError,
)
from hosted_agent_kit.domain.models import SessionAffinityKey
from hosted_agent_kit.services.reconciler import Reconciler
from tests.conftest import Harness, eventually, make_harness, make_request, settle

STATEFUL = {"coding-agent": {"mode": "stateful", "max_sessions": 3}}


def counter(h: Harness, agent: str, name: str) -> float:
    return sum(
        float(c["value"])
        for c in h.metrics.snapshot()["agents"].get(agent, {"counters": []})["counters"]
        if c["name"] == name
    )


async def test_restart_rebuilds_registry_from_agent_scoped_listing() -> None:
    """A10: the registry is rebuilt; user affinity is not recoverable in V1."""
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 5})
    h.fake.add_session("stateless-agent")
    h.fake.add_session("stateless-agent", S.IDLE)
    report = await h.reconciler.run_agent("stateless-agent")
    records = await h.registry.list("stateless-agent")
    assert report.outcome == "success" and report.discovered == 2
    assert [r.local_state for r in records] == [L.AVAILABLE, L.AVAILABLE]
    assert await h.affinity.count("stateless-agent") == 0
    assert h.metrics.snapshot()["agents"]["stateless-agent"]["histograms"]


async def test_stateful_agent_ignores_unknown_sessions_by_default() -> None:
    """A stateful session may hold another user's files, so it is never handed to a new user."""
    h = make_harness(STATEFUL, defaults={})
    foreign = h.fake.add_session("coding-agent")
    failed = h.fake.add_session("coding-agent", S.FAILED)
    report = await h.reconciler.run_agent("coding-agent")
    assert report.outcome == "success" and report.failed_sessions == 0
    assert await h.registry.count("coding-agent") == 0
    assert h.fake.deleted == []  # not ours to delete
    await h.pool.execute(make_request("coding-agent", "u1"))
    assert len(h.fake.created) == 1 and h.fake.created[0] not in {
        foreign.session_id,
        failed.session_id,
    }
    assert h.fake.invocations[0].session_id == h.fake.created[0]
    await h.reconciler.run_agent("coding-agent")  # still ignored on later syncs
    assert await h.registry.count("coding-agent") == 1


async def test_ignored_sessions_are_logged_with_a_hashed_identifier(
    caplog: pytest.LogCaptureFixture,
) -> None:
    caplog.set_level(logging.INFO, logger="hosted_agent_kit")
    h = make_harness(STATEFUL, defaults={})
    foreign = h.fake.add_session("coding-agent")
    await h.reconciler.run_agent("coding-agent")
    events = [r for r in caplog.records if r.getMessage() == "session_ignored"]
    assert len(events) == 1 and "session_id_hash" in events[0].fields  # type: ignore[attr-defined]
    assert foreign.session_id not in caplog.text


async def test_stateful_agent_adopts_unknown_sessions_when_enabled() -> None:
    h = make_harness(
        {"coding-agent": {"mode": "stateful", "max_sessions": 3, "adopt_unbound_sessions": True}},
        defaults={},
    )
    adopted = h.fake.add_session("coding-agent")
    await h.reconciler.run_agent("coding-agent")
    await h.pool.execute(make_request("coding-agent", "u1"))
    assert h.fake.created == []  # the adopted session was used
    entry = await h.affinity.get(SessionAffinityKey(user_id="u1", agent_name="coding-agent"))
    assert entry is not None and entry.session_id == adopted.session_id
    await h.pool.execute(make_request("coding-agent", "u2"))
    assert len(h.fake.created) == 1  # u1 owns the adopted session


async def test_stateless_agent_can_opt_out_of_adoption() -> None:
    h = make_harness({"a": {"mode": "stateless", "adopt_unbound_sessions": False}}, defaults={})
    h.fake.add_session("a")
    await h.reconciler.run_agent("a")
    assert await h.registry.count("a") == 0


async def test_sessions_this_process_created_are_still_refreshed_when_adoption_is_off() -> None:
    h = make_harness(STATEFUL, defaults={})
    await h.pool.execute(make_request("coding-agent", "u1"))
    sid = h.fake.created[0]
    h.fake.set_status(sid, S.IDLE)
    await h.reconciler.run_agent("coding-agent")
    record = await h.registry.get("coding-agent", sid)
    assert record is not None and record.platform_status is S.IDLE


async def test_provisioning_sessions_are_unavailable_until_active() -> None:
    h = make_harness()
    creating = h.fake.add_session("stateless-agent", S.CREATING)
    updating = h.fake.add_session("stateless-agent", S.UPDATING)
    unknown = h.fake.add_session("stateless-agent", S.UNKNOWN)
    await h.reconciler.run_agent("stateless-agent")
    for s in (creating, updating, unknown):
        assert (await h.registry.get("stateless-agent", s.session_id)).local_state is L.UNAVAILABLE  # type: ignore[union-attr]
    for s in (creating, updating, unknown):
        h.fake.set_status(s.session_id, S.ACTIVE)
    await h.reconciler.run_agent("stateless-agent")
    for s in (creating, updating, unknown):
        assert (await h.registry.get("stateless-agent", s.session_id)).local_state is L.AVAILABLE  # type: ignore[union-attr]


async def test_failed_session_is_logged_deleted_and_unmapped(
    caplog: pytest.LogCaptureFixture,
) -> None:
    """A8."""
    caplog.set_level(logging.INFO, logger="hosted_agent_kit")
    h = make_harness(STATEFUL, defaults={})
    await h.pool.execute(make_request("coding-agent", "u1"))
    sid = h.fake.created[0]
    h.fake.set_status(sid, S.FAILED)
    report = await h.reconciler.run_agent("coding-agent")
    assert report.failed_sessions == 1 and report.removed == 1
    assert h.fake.deleted == [sid]
    assert await h.registry.get("coding-agent", sid) is None
    assert await h.affinity.get(SessionAffinityKey(user_id="u1", agent_name="coding-agent")) is None
    assert counter(h, "coding-agent", "pool_failed_sessions_total") == 1
    events = [r for r in caplog.records if r.getMessage() == "session_failed"]
    assert events and events[0].fields["action"] == "delete"  # type: ignore[attr-defined]
    assert "session_id_hash" in events[0].fields  # type: ignore[attr-defined]
    assert sid not in caplog.text


async def test_failed_session_not_in_registry_is_adopted_then_deleted() -> None:
    h = make_harness()
    failed = h.fake.add_session("stateless-agent", S.FAILED)
    await h.reconciler.run_agent("stateless-agent")
    assert h.fake.deleted == [failed.session_id]
    assert await h.registry.count("stateless-agent") == 0


async def test_failed_delete_leaves_record_retiring_and_next_sync_retries() -> None:
    h = make_harness(delete_retries=0)
    failed = h.fake.add_session("stateless-agent", S.FAILED)
    h.fake.delete_errors = [FoundryUnavailable("x")]
    report = await h.reconciler.run_agent("stateless-agent")
    assert report.removed == 0
    record = await h.registry.get("stateless-agent", failed.session_id)
    assert record is not None and record.local_state is L.RETIRING
    report = await h.reconciler.run_agent("stateless-agent")
    assert await h.registry.get("stateless-agent", failed.session_id) is None
    assert h.fake.deleted == [failed.session_id]


@pytest.mark.parametrize("status", [S.DELETED, S.EXPIRED])
async def test_gone_sessions_clear_registry_and_affinity(status: S) -> None:
    h = make_harness(STATEFUL, defaults={})
    await h.pool.execute(make_request("coding-agent", "u1"))
    sid = h.fake.created[0]
    h.fake.set_status(sid, status)
    await h.reconciler.run_agent("coding-agent")
    assert await h.registry.get("coding-agent", sid) is None
    assert await h.affinity.count("coding-agent") == 0
    assert h.fake.deleted == []  # no remote call for something already gone
    await h.pool.execute(make_request("coding-agent", "u1"))
    assert len(h.fake.created) == 2  # recreated when needed


async def test_gone_session_never_seen_locally_is_ignored() -> None:
    h = make_harness()
    h.fake.add_session("stateless-agent", S.EXPIRED)
    await h.reconciler.run_agent("stateless-agent")
    assert await h.registry.count("stateless-agent") == 0


async def test_deleting_session_is_marked_retiring_and_never_scheduled() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    await h.pool.execute(make_request())
    sid = h.fake.created[0]
    h.fake.set_status(sid, S.DELETING)
    await h.reconciler.run_agent("stateless-agent")
    assert (await h.registry.get("stateless-agent", sid)).local_state is L.RETIRING  # type: ignore[union-attr]
    h.fake.sessions.pop(sid)
    await h.reconciler.run_agent("stateless-agent")  # confirmed gone by get_session
    assert await h.registry.get("stateless-agent", sid) is None


async def test_missing_session_is_verified_once_before_removal() -> None:
    h = make_harness(STATEFUL, defaults={})
    await h.pool.execute(make_request("coding-agent", "u1"))
    sid = h.fake.created[0]
    h.fake.sessions.pop(sid)  # absent from the listing and from get_session
    await h.reconciler.run_agent("coding-agent")
    assert h.fake.get_calls.count(sid) == 1
    assert await h.registry.get("coding-agent", sid) is None
    assert await h.affinity.count("coding-agent") == 0


async def test_listing_lag_does_not_delete_a_session_that_get_can_see() -> None:
    h = make_harness()
    await h.pool.execute(make_request())
    sid = h.fake.created[0]
    hidden = h.fake.sessions[sid]

    original = h.fake.list_sessions

    async def lagging(agent: str):  # type: ignore[no-untyped-def]
        async for s in original(agent):
            if s.session_id != sid:
                yield s

    h.fake.list_sessions = lagging  # type: ignore[method-assign]
    await h.reconciler.run_agent("stateless-agent")
    record = await h.registry.get("stateless-agent", sid)
    assert record is not None and record.local_state is L.AVAILABLE
    assert hidden.session_id == sid


async def test_verification_error_marks_unavailable_instead_of_deleting() -> None:
    h = make_harness()
    await h.pool.execute(make_request())
    sid = h.fake.created[0]
    h.fake.sessions.pop(sid)
    h.fake.get_errors[sid] = FoundryUnavailable("x")
    await h.reconciler.run_agent("stateless-agent")
    record = await h.registry.get("stateless-agent", sid)
    assert record is not None and record.local_state is L.UNAVAILABLE


async def test_incomplete_listing_never_infers_deletion() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 5})
    a = h.fake.add_session("stateless-agent")
    b = h.fake.add_session("stateless-agent")
    await h.reconciler.run_agent("stateless-agent")
    h.fake.list_fail_after = 1
    h.fake.list_error = FoundryUnavailable("page 2 failed")
    report = await h.reconciler.run_agent("stateless-agent")
    assert report.outcome == "incomplete"
    assert (await h.registry.get("stateless-agent", a.session_id)).local_state is L.AVAILABLE  # type: ignore[union-attr]
    assert (await h.registry.get("stateless-agent", b.session_id)).local_state is L.UNAVAILABLE  # type: ignore[union-attr]
    assert h.fake.get_calls == []  # no verification, no deletion
    assert h.fake.deleted == []
    h.fake.list_fail_after = None
    report = await h.reconciler.run_agent("stateless-agent")
    assert report.outcome == "success"
    assert (await h.registry.get("stateless-agent", b.session_id)).local_state is L.AVAILABLE  # type: ignore[union-attr]
    outcomes = {
        c["labels"]["outcome"]
        for c in h.metrics.snapshot()["agents"]["stateless-agent"]["counters"]
        if c["name"] == "pool_reconcile_total"
    }
    assert outcomes == {"success", "incomplete"}


async def test_incomplete_listing_skips_warm_maintenance() -> None:
    h = make_harness({"a": {"mode": "stateless", "min_warm_sessions": 2}}, defaults={})
    h.fake.list_fail_after = 0
    h.fake.list_error = FoundryUnavailable("x")
    report = await h.reconciler.run_agent("a")
    assert report.warm_created == 0 and h.fake.created == []


async def test_leased_session_absent_from_listing_is_retired_when_the_lease_ends() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    h.fake.invoke_gate = asyncio.Event()
    task = asyncio.create_task(h.pool.execute(make_request()))
    await settle()
    sid = h.fake.created[0]
    h.fake.sessions.pop(sid)
    await h.reconciler.run_agent("stateless-agent")
    record = await h.registry.get("stateless-agent", sid)
    assert record is not None and record.local_state is L.RETIRING  # lease holder is kept
    assert record.lease_request_id is not None
    h.fake.invoke_gate.set()
    await task
    assert await h.registry.get("stateless-agent", sid) is None  # never offered again


async def test_leased_session_is_left_alone_when_the_listing_is_incomplete() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    h.fake.invoke_gate = asyncio.Event()
    task = asyncio.create_task(h.pool.execute(make_request()))
    await settle()
    sid = h.fake.created[0]
    h.fake.list_error = FoundryUnavailable("page 2 failed")
    report = await h.reconciler.run_agent("stateless-agent")
    assert report.uncertain == 0
    assert (await h.registry.get("stateless-agent", sid)).local_state is L.LEASED  # type: ignore[union-attr]
    h.fake.invoke_gate.set()
    await task


async def test_leased_failed_session_is_retired_when_the_lease_ends() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    h.fake.invoke_gate = asyncio.Event()
    task = asyncio.create_task(h.pool.execute(make_request()))
    await settle()
    sid = h.fake.created[0]
    h.fake.set_status(sid, S.FAILED)
    report = await h.reconciler.run_agent("stateless-agent")
    assert report.removed == 0
    assert (await h.registry.get("stateless-agent", sid)).local_state is L.RETIRING  # type: ignore[union-attr]
    h.fake.invoke_gate.set()
    await task
    assert await h.registry.get("stateless-agent", sid) is None
    assert h.fake.deleted == [sid]


async def test_retiring_session_with_failed_remote_delete_after_release_is_retried_by_sync() -> (
    None
):
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1}, delete_retries=0)
    h.fake.invoke_gate = asyncio.Event()
    task = asyncio.create_task(h.pool.execute(make_request()))
    await settle()
    sid = h.fake.created[0]
    h.fake.set_status(sid, S.FAILED)
    await h.reconciler.run_agent("stateless-agent")
    h.fake.delete_errors = [FoundryUnavailable("x")]
    h.fake.invoke_gate.set()
    await task
    assert (await h.registry.get("stateless-agent", sid)).local_state is L.RETIRING  # type: ignore[union-attr]
    await h.reconciler.run_agent("stateless-agent")
    assert await h.registry.get("stateless-agent", sid) is None


async def test_sync_makes_updating_session_available_and_wakes_pinned_waiter() -> None:
    h = make_harness(STATEFUL, defaults={})
    await h.pool.execute(make_request("coding-agent", "u1"))
    sid = h.fake.created[0]
    h.fake.set_status(sid, S.UPDATING)
    await h.reconciler.run_agent("coding-agent")
    task = asyncio.create_task(h.pool.execute(make_request("coding-agent", "u1")))
    await settle()
    assert not task.done()
    h.fake.set_status(sid, S.ACTIVE)
    await h.reconciler.run_agent("coding-agent")
    result = await asyncio.wait_for(task, 2)
    assert result.status_code == 200 and len(h.fake.created) == 1


async def test_manual_sync_rejects_overlap_but_scheduled_sync_skips() -> None:
    h = make_harness()
    gate = asyncio.Event()
    original = h.fake.list_sessions

    async def slow(agent: str):  # type: ignore[no-untyped-def]
        await gate.wait()
        async for s in original(agent):
            yield s

    h.fake.list_sessions = slow  # type: ignore[method-assign]
    running = asyncio.create_task(h.reconciler.run_agent("stateless-agent"))
    await settle()
    with pytest.raises(SyncInProgressError):
        await h.reconciler.run_agent("stateless-agent", manual=True)
    skipped = await h.reconciler.run_agent("stateless-agent")
    assert skipped.skipped and skipped.outcome == "skipped"
    gate.set()
    assert (await running).outcome == "success"


async def test_unexpected_error_is_reported_and_does_not_escape() -> None:
    h = make_harness()

    async def broken(agent: str):  # type: ignore[no-untyped-def]
        raise RuntimeError("bug")
        yield

    h.fake.list_sessions = broken  # type: ignore[method-assign]
    report = await h.reconciler.run_agent("stateless-agent")
    assert report.outcome == "error" and report.errors == ["RuntimeError"]


async def test_warm_sessions_are_created_up_to_minimum_and_never_exceed_max() -> None:
    h = make_harness(
        {"a": {"mode": "stateless", "min_warm_sessions": 2, "max_sessions": 3}}, defaults={}
    )
    report = await h.reconciler.run_agent("a")
    assert report.warm_created == 2 and len(h.fake.created) == 2
    report = await h.reconciler.run_agent("a")
    assert report.warm_created == 0  # already warm


async def test_warm_stops_at_capacity() -> None:
    h = make_harness(
        {"a": {"mode": "stateless", "min_warm_sessions": 2, "max_sessions": 2}}, defaults={}
    )
    h.fake.invoke_gate = asyncio.Event()
    first = asyncio.create_task(h.pool.execute(make_request("a", "u1")))
    await settle()
    report = await h.reconciler.run_agent("a")
    assert report.warm_created == 1  # one session is leased; capacity for one warm
    h.fake.invoke_gate.set()
    await first


async def test_warm_creation_failure_is_reported_without_raising() -> None:
    h = make_harness(
        {"a": {"mode": "stateless", "min_warm_sessions": 1, "create_retries": 0}}, defaults={}
    )
    h.fake.create_errors = [FoundryUnavailable("x")]
    report = await h.reconciler.run_agent("a")
    assert report.warm_created == 0 and report.errors == ["FOUNDRY_UNAVAILABLE"]
    assert await h.registry.reserved("a") == 0


async def test_warm_session_is_used_by_first_request_without_creating() -> None:
    h = make_harness({"a": {"mode": "stateless", "min_warm_sessions": 1}}, defaults={})
    await h.reconciler.run_agent("a")
    await h.pool.execute(make_request("a"))
    assert len(h.fake.created) == 1


async def test_workers_start_sync_immediately_and_stop_cleanly() -> None:
    h = make_harness()
    h.fake.add_session("stateless-agent")
    assert not h.reconciler.workers_started
    await h.reconciler.start()
    assert h.reconciler.workers_started
    await settle(20)
    assert await h.registry.count("stateless-agent") == 1
    await h.reconciler.stop()
    assert not h.reconciler.workers_started


async def test_worker_repeats_on_interval_and_survives_errors() -> None:
    h = make_harness()
    fast = h.config.agents["stateless-agent"].model_copy(update={"sync_interval_seconds": 0.01})
    config = h.config.model_copy(update={"agents": {"stateless-agent": fast}})
    reconciler = Reconciler(
        config=config,
        adapter=h.fake,
        registry=h.registry,
        pool=h.pool,
        metrics=h.gated,
        clock=h.clock,
    )
    calls = 0
    real_notify = h.pool.notify

    async def flaky(agent: str) -> None:
        nonlocal calls
        calls += 1
        if calls == 1:
            raise RuntimeError("notify failed")
        await real_notify(agent)

    h.pool.notify = flaky  # type: ignore[method-assign]
    await reconciler.start()
    for _ in range(100):
        await asyncio.sleep(0.01)
        if calls >= 3:
            break
    await reconciler.stop()
    assert calls >= 3


async def test_run_all_covers_every_agent() -> None:
    h = make_harness({"a": {}, "b": {}}, defaults={"mode": "stateless"})
    reports = await h.reconciler.run_all()
    assert [r.agent_name for r in reports] == ["a", "b"]


async def test_stuck_retiring_session_that_still_exists_upstream_is_retried_by_the_next_sync() -> (
    None
):
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1}, delete_retries=0)
    await h.pool.execute(make_request())
    sid = h.fake.created[0]
    h.fake.delete_errors = [FoundryUnavailable("x")]
    with pytest.raises(UpstreamError):
        await h.pool.admin_delete("stateless-agent", sid)
    assert (await h.registry.get("stateless-agent", sid)).local_state is L.RETIRING  # type: ignore[union-attr]
    report = await h.reconciler.run_agent("stateless-agent")
    assert report.removed == 1 and await h.registry.get("stateless-agent", sid) is None
    assert h.fake.deleted == [sid]


async def test_stop_before_start_is_harmless() -> None:
    h = make_harness()
    await h.reconciler.stop()
    assert not h.reconciler.workers_started


async def test_stop_cancels_a_worker_that_overruns_the_grace_period() -> None:
    h = make_harness()

    async def hang(agent: str):  # type: ignore[no-untyped-def]
        await asyncio.sleep(3600)
        yield

    h.fake.list_sessions = hang  # type: ignore[method-assign]
    await h.reconciler.start()
    await settle(10)
    await asyncio.wait_for(h.reconciler.stop(grace_seconds=0.05), 2)
    assert not h.reconciler.workers_started


async def test_clean_sync_report_has_the_expected_defaults() -> None:
    h = make_harness()
    report = await h.reconciler.run_agent("stateless-agent")
    assert report.outcome == "success" and report.skipped is False
    assert (report.discovered, report.removed, report.failed_sessions) == (0, 0, 0)
    assert (report.uncertain, report.warm_created) == (0, 0)
    assert report.errors == [] and report.duration_seconds == 0.0


async def test_skipped_report_records_no_work() -> None:
    h = make_harness()
    async with h.reconciler._mutexes["stateless-agent"]:
        report = await h.reconciler.run_agent("stateless-agent")
    assert report.skipped is True and report.duration_seconds == 0.0
    assert (report.discovered, report.uncertain) == (0, 0)


async def test_duration_is_measured_and_logged_rounded_to_milliseconds(
    caplog: pytest.LogCaptureFixture,
) -> None:
    caplog.set_level(logging.INFO, logger="hosted_agent_kit")
    h = make_harness()
    original = h.fake.list_sessions

    async def slow(agent: str):  # type: ignore[no-untyped-def]
        h.clock.advance(0.123456)
        async for item in original(agent):
            yield item

    h.fake.list_sessions = slow  # type: ignore[method-assign]
    report = await h.reconciler.run_agent("stateless-agent")
    assert report.duration_seconds == pytest.approx(0.123456)
    done = [r for r in caplog.records if r.getMessage() == "reconcile_completed"]
    assert done[0].fields["duration_seconds"] == 0.123  # type: ignore[attr-defined]


async def test_incomplete_listing_records_the_error_type() -> None:
    h = make_harness()
    h.fake.list_fail_after = 0
    h.fake.list_error = FoundryUnavailable("x")
    report = await h.reconciler.run_agent("stateless-agent")
    assert report.outcome == "incomplete" and report.errors == ["FoundryUnavailable"]


async def test_workers_can_be_restarted_after_a_stop() -> None:
    h = make_harness()
    await h.reconciler.start()
    await settle(20)
    await h.reconciler.stop()
    h.fake.add_session("stateless-agent")
    await h.reconciler.start()
    assert h.reconciler.workers_started

    async def adopted() -> bool:
        return await h.registry.count("stateless-agent") == 1

    await eventually(adopted)  # the new workers ran a sync, so the stop flag was cleared
    await h.reconciler.stop()


async def test_cancelled_workers_are_awaited_before_stop_returns() -> None:
    h = make_harness()

    async def hang(agent: str):  # type: ignore[no-untyped-def]
        await asyncio.sleep(3600)
        yield

    h.fake.list_sessions = hang  # type: ignore[method-assign]
    await h.reconciler.start()
    await settle(10)
    tasks = list(h.reconciler._tasks)
    await h.reconciler.stop(grace_seconds=0.05)
    assert tasks and all(task.done() for task in tasks)


async def test_stop_returns_promptly_because_workers_see_the_stop_signal() -> None:
    h = make_harness()
    await h.reconciler.start()
    await settle(20)
    await asyncio.wait_for(h.reconciler.stop(), 2)  # default grace is 5 seconds
    assert not h.reconciler.workers_started


async def test_missing_session_is_removed_locally_without_a_remote_delete() -> None:
    h = make_harness()
    await h.pool.execute(make_request())
    sid = h.fake.created[0]
    h.fake.sessions.pop(sid)
    await h.reconciler.run_agent("stateless-agent")
    assert await h.registry.get("stateless-agent", sid) is None and h.fake.deleted == []


async def test_session_seen_only_by_get_is_refreshed_from_what_get_returns() -> None:
    h = make_harness()
    await h.pool.execute(make_request())
    sid = h.fake.created[0]
    h.fake.set_status(sid, S.IDLE)
    original = h.fake.list_sessions

    async def lagging(agent: str):  # type: ignore[no-untyped-def]
        async for s in original(agent):
            if s.session_id != sid:
                yield s

    h.fake.list_sessions = lagging  # type: ignore[method-assign]
    await h.reconciler.run_agent("stateless-agent")
    record = await h.registry.get("stateless-agent", sid)
    assert record is not None and record.platform_status is S.IDLE


async def test_failed_verification_is_counted_as_uncertain() -> None:
    h = make_harness()
    await h.pool.execute(make_request())
    sid = h.fake.created[0]
    h.fake.sessions.pop(sid)
    h.fake.get_errors[sid] = FoundryUnavailable("x")
    report = await h.reconciler.run_agent("stateless-agent")
    assert report.uncertain == 1 and report.removed == 0


async def test_existing_warm_sessions_count_once_each() -> None:
    h = make_harness(
        {"a": {"mode": "stateless", "min_warm_sessions": 2, "max_sessions": 5}}, defaults={}
    )
    h.fake.add_session("a")
    report = await h.reconciler.run_agent("a")
    assert report.warm_created == 1 and len(h.fake.created) == 1


async def test_user_bound_sessions_are_not_warm() -> None:
    h = make_harness(
        {"a": {"mode": "stateful", "min_warm_sessions": 1, "max_sessions": 5}}, defaults={}
    )
    await h.pool.execute(make_request("a", "u1"))
    assert len(h.fake.created) == 1
    report = await h.reconciler.run_agent("a")
    assert report.warm_created == 1 and len(h.fake.created) == 2
