"""Version pinning and draining, conversation-scoped session ids and the idempotency store."""

from __future__ import annotations

import asyncio

import pytest

from hosted_agent_kit.config.models import ConfigError
from hosted_agent_kit.domain.enums import LocalSessionState as L
from hosted_agent_kit.domain.errors import FoundryUnavailable
from hosted_agent_kit.services.idempotency import (
    IdempotencyStore,
    Outcome,
    StoredResponse,
    fingerprint,
)
from hosted_agent_kit.services.session_ids import SessionIdDeriver
from tests.conftest import make_config, make_harness, make_request, settle
from tests.fakes.clock import FakeClock
from tests.fakes.foundry import FakeFoundry

POOL = "stateless-agent"


# ------------------------------------------------------------------- version pinning


async def test_new_sessions_are_pinned_to_the_configured_version() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 2, "agent_version": "5"})
    await h.pool.execute(make_request())
    assert h.fake.create_versions == ["5"]
    (record,) = await h.registry.list(POOL)
    assert record.agent_version == "5"


async def test_without_a_pin_new_sessions_use_the_latest_version() -> None:
    h = make_harness()
    h.fake.latest_version = "9"
    await h.pool.execute(make_request())
    assert h.fake.create_versions == [None]
    (record,) = await h.registry.list(POOL)
    assert record.agent_version == "9"


def test_an_empty_version_pin_is_refused() -> None:
    with pytest.raises(ConfigError):
        make_config({"a": {"mode": "stateless", "agent_version": ""}})


def test_an_unknown_drain_policy_is_refused() -> None:
    with pytest.raises(ConfigError):
        make_config({"a": {"mode": "stateless", "version_drain": "everything"}})


# ------------------------------------------------------------------ version draining


def drain_harness(policy: str, **agent: object) -> object:
    return make_harness(
        defaults={"mode": "stateless", "max_sessions": 4, "version_drain": policy, **agent}
    )


async def test_nothing_is_drained_by_default() -> None:
    h = make_harness()
    for _ in range(2):
        h.fake.add_session(POOL)  # version "1"
    h.fake.latest_version = "2"
    report = await h.reconciler.run_agent(POOL)
    assert report.drained == 0 and h.fake.deleted == []


async def test_idle_pooled_sessions_on_an_old_version_are_retired_and_replaced() -> None:
    h = make_harness(
        defaults={
            "mode": "stateless",
            "max_sessions": 4,
            "min_warm_sessions": 2,
            "version_drain": "unbound",
        }
    )
    old = [h.fake.add_session(POOL).session_id for _ in range(2)]
    h.fake.latest_version = "2"
    report = await h.reconciler.run_agent(POOL)
    assert report.drained == 2 and sorted(h.fake.deleted) == sorted(old)
    versions = {r.agent_version for r in await h.registry.list(POOL)}
    assert versions == {"2"} and report.warm_created == 2  # the warm pool is on the new version


async def test_a_leased_session_is_never_drained() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 2, "version_drain": "unbound"})
    h.fake.invoke_gate = asyncio.Event()
    task = asyncio.create_task(h.pool.execute(make_request()))
    await settle()
    sid = h.fake.created[0]
    h.fake.latest_version = "2"
    report = await h.reconciler.run_agent(POOL)
    assert report.drained == 0 and sid not in h.fake.deleted
    h.fake.invoke_gate.set()
    await task


async def test_a_pinned_version_is_the_drain_target() -> None:
    h = make_harness(
        defaults={
            "mode": "stateless",
            "max_sessions": 4,
            "version_drain": "unbound",
            "agent_version": "1",
        }
    )
    h.fake.add_session(POOL)  # version 1 matches the pin
    h.fake.latest_version = "2"  # the latest is irrelevant while pinned
    assert (await h.reconciler.run_agent(POOL)).drained == 0
    h2 = make_harness(
        defaults={
            "mode": "stateless",
            "max_sessions": 4,
            "version_drain": "unbound",
            "agent_version": "2",
        }
    )
    h2.fake.add_session(POOL)
    assert (await h2.reconciler.run_agent(POOL)).drained == 1


STATEFUL = {"coding-agent": {"mode": "stateful", "max_sessions": 3}}


async def test_user_sessions_are_kept_unless_the_policy_is_idle() -> None:
    for policy, drained in (("unbound", 0), ("idle", 1)):
        h = make_harness(STATEFUL, defaults={"version_drain": policy})
        await h.pool.execute(make_request("coding-agent", "u1"))
        h.fake.latest_version = "2"
        report = await h.reconciler.run_agent("coding-agent")
        assert report.drained == drained
        assert (await h.registry.count("coding-agent")) == 1 - drained


async def test_restorable_user_sessions_are_kept_too_unless_the_policy_is_idle() -> None:
    deriver = SessionIdDeriver(b"k" * 32)
    for policy, drained in (("unbound", 0), ("idle", 1)):
        h = make_harness(STATEFUL, defaults={"version_drain": policy}, session_ids=deriver)
        h.fake.add_session("coding-agent", session_id=deriver.for_user("coding-agent", "u1"))
        h.fake.latest_version = "2"
        assert (await h.reconciler.run_agent("coding-agent")).drained == drained


async def test_a_failure_to_read_the_latest_version_is_reported_not_raised() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 2, "version_drain": "unbound"})
    h.fake.add_session(POOL)
    h.fake.latest_version_error = FoundryUnavailable("down")
    report = await h.reconciler.run_agent(POOL)
    assert report.drained == 0 and "FoundryUnavailable" in report.errors
    assert h.fake.deleted == []


async def test_a_drain_that_cannot_delete_keeps_the_session_retiring() -> None:
    h = make_harness(
        defaults={
            "mode": "stateless",
            "max_sessions": 2,
            "version_drain": "unbound",
        },
        delete_retries=0,
    )
    sid = h.fake.add_session(POOL).session_id
    h.fake.latest_version = "2"
    from hosted_agent_kit.domain.errors import FoundryRejected

    h.fake.delete_errors = [FoundryRejected(403)]
    report = await h.reconciler.run_agent(POOL)
    record = await h.registry.get(POOL, sid)
    assert report.drained == 0 and record is not None and record.local_state is L.RETIRING
    report = await h.reconciler.run_agent(POOL)  # the next sync finishes the job
    assert await h.registry.get(POOL, sid) is None


# ------------------------------------------------------------ conversation session ids


def test_a_conversation_key_changes_the_derived_id_and_no_key_does_not() -> None:
    deriver = SessionIdDeriver(b"k" * 32)
    plain = deriver.for_user("agent", "user")
    assert plain == deriver.for_user("agent", "user", None)  # unchanged from earlier releases
    a = deriver.for_user("agent", "user", "a")
    b = deriver.for_user("agent", "user", "b")
    assert len({plain, a, b}) == 3 and all(SessionIdDeriver.is_derived(x) for x in (a, b))
    assert deriver.for_user("agent", "user", "a") == a  # stable across restarts
    assert deriver.for_user("agent", "user2", "a") != a


async def test_each_conversation_derives_its_own_session_and_is_found_again_after_a_restart() -> (
    None
):
    deriver = SessionIdDeriver(b"k" * 32)
    fake = FakeFoundry()
    h = make_harness(STATEFUL, defaults={}, fake=fake, session_ids=deriver)
    for key in ("a", "b", None):
        await h.pool.execute(make_request("coding-agent", "u1", conversation_key=key))
    expected = {deriver.for_user("coding-agent", "u1", k) for k in ("a", "b", None)}
    assert set(fake.created) == expected
    # A new process finds them again by derivation and reuses them without creating anything.
    restarted = make_harness(STATEFUL, defaults={}, fake=fake, session_ids=deriver)
    await restarted.reconciler.run_agent("coding-agent")
    fake.created.clear()
    await restarted.pool.execute(make_request("coding-agent", "u1", conversation_key="b"))
    assert fake.created == []
    assert fake.invocations[-1].session_id == deriver.for_user("coding-agent", "u1", "b")


async def test_a_waiting_request_keeps_its_conversation_key() -> None:
    h = make_harness(
        {"coding-agent": {"mode": "stateful", "max_sessions": 1, "queue": {"max_wait_seconds": 5}}},
        defaults={},
    )
    h.fake.invoke_gate = asyncio.Event()
    first = asyncio.create_task(
        h.pool.execute(make_request("coding-agent", "u1", conversation_key="a"))
    )
    await settle()
    second = asyncio.create_task(
        h.pool.execute(make_request("coding-agent", "u1", conversation_key="a"))
    )
    await settle()
    assert await h.queue.depth("coding-agent") == 1  # same conversation: waits for its session
    h.fake.invoke_gate.set()
    await asyncio.gather(first, second)
    assert len(h.fake.created) == 1


# ------------------------------------------------------------------ idempotency store


def store(
    clock: FakeClock | None = None,
    *,
    ttl_seconds: float = 60,
    max_entries: int = 10,
    max_body_bytes: int = 100,
) -> tuple[IdempotencyStore, FakeClock]:
    clock = clock or FakeClock()
    made = IdempotencyStore(
        clock, ttl_seconds=ttl_seconds, max_entries=max_entries, max_body_bytes=max_body_bytes
    )
    return made, clock


def test_the_first_request_runs_and_a_repeat_replays() -> None:
    s, _ = store()
    fp = fingerprint("route", "body")
    assert s.begin("k", fp) == (Outcome.NEW, None)
    assert s.begin("k", fp) == (Outcome.IN_PROGRESS, None)
    response = StoredResponse(200, "application/json", b"{}")
    s.complete("k", response)
    assert s.begin("k", fp) == (Outcome.REPLAY, response)
    assert s.begin("k", fingerprint("route", "other"))[0] is Outcome.MISMATCH


def test_an_abandoned_claim_lets_the_next_request_run() -> None:
    s, _ = store()
    fp = fingerprint("x")
    s.begin("k", fp)
    s.abandon("k")
    assert s.begin("k", fp)[0] is Outcome.NEW
    s.complete("k", StoredResponse(200, "text/plain", b"ok"))
    s.abandon("k")  # a finished response is not forgotten by a late abandon
    assert s.begin("k", fp)[0] is Outcome.REPLAY
    s.abandon("never-seen")
    s.complete("never-seen", StoredResponse(200, "text/plain", b""))


def test_a_response_that_is_too_large_is_not_stored() -> None:
    s, _ = store(max_body_bytes=3)
    fp = fingerprint("x")
    s.begin("k", fp)
    s.complete("k", StoredResponse(200, "text/plain", b"toolarge"))
    assert s.begin("k", fp)[0] is Outcome.NEW


def test_stored_responses_expire_but_running_claims_do_not() -> None:
    s, clock = store(ttl_seconds=60)
    fp = fingerprint("x")
    s.begin("done", fp)
    s.complete("done", StoredResponse(200, "text/plain", b"ok"))
    s.begin("running", fp)
    clock.advance(61)
    assert s.begin("done", fp)[0] is Outcome.NEW
    assert s.begin("running", fp)[0] is Outcome.IN_PROGRESS


def test_the_oldest_finished_entries_are_dropped_when_full() -> None:
    s, clock = store(max_entries=3)
    fp = fingerprint("x")
    for index in range(3):
        s.begin(f"k{index}", fp)
        s.complete(f"k{index}", StoredResponse(200, "text/plain", b"ok"))
        clock.advance(1)
    s.begin("k3", fp)  # needs room: k0 is the oldest finished entry
    assert s.begin("k0", fp)[0] is Outcome.NEW
    assert s.begin("k2", fp)[0] is Outcome.REPLAY


def test_a_zero_ttl_turns_the_store_off() -> None:
    s, _ = store(ttl_seconds=0)
    assert s.enabled is False and s.ttl_seconds == 0


def test_fingerprints_distinguish_how_the_parts_are_split() -> None:
    assert fingerprint("ab", "c") != fingerprint("a", "bc")
    assert fingerprint(b"x") == fingerprint("x")
