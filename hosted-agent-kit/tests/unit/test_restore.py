"""Finding users' sessions again after a restart (derived session ids)."""

from __future__ import annotations

import asyncio
import logging
from typing import Any

import pytest

from hosted_agent_kit.config.models import ConfigError
from hosted_agent_kit.config.settings import Settings
from hosted_agent_kit.domain.enums import FoundrySessionStatus as S
from hosted_agent_kit.domain.enums import LocalSessionState as L
from hosted_agent_kit.domain.errors import (
    AppError,
    FoundryConflict,
    FoundrySessionNotFound,
)
from hosted_agent_kit.domain.models import SessionAffinityKey
from hosted_agent_kit.runtime import session_deriver
from hosted_agent_kit.service.main import create_app
from hosted_agent_kit.services.session_ids import SessionIdDeriver
from tests.conftest import Harness, make_config, make_harness, make_request, make_settings, settle
from tests.fakes.foundry import FakeFoundry

KEY = b"k" * 32
DERIVER = SessionIdDeriver(KEY)
AGENTS: dict[str, dict[str, Any]] = {"a": {"mode": "stateful", "max_sessions": 5}}


def started(fake: FakeFoundry | None = None, **kwargs: Any) -> Harness:
    return make_harness(AGENTS, defaults={}, fake=fake, session_ids=DERIVER, **kwargs)


def counter(h: Harness, name: str) -> float:
    agent = h.metrics.snapshot()["agents"].get("a", {"counters": []})
    return sum(float(c["value"]) for c in agent["counters"] if c["name"] == name)


def derived(user: str) -> str:
    return DERIVER.for_user("a", user)


def key(user: str) -> SessionAffinityKey:
    return SessionAffinityKey(user_id=user, agent_name="a")


# ----------------------------------------------------------------- the helper


def test_derived_ids_are_stable_distinct_and_well_formed() -> None:
    first = DERIVER.for_user("a", "u1")
    assert first == DERIVER.for_user("a", "u1")
    assert first != DERIVER.for_user("a", "u2")
    assert first != DERIVER.for_user("b", "u1")
    assert first != SessionIdDeriver(b"z" * 32).for_user("a", "u1")
    assert first.startswith("pool-") and len(first) == 45 and "u1" not in first
    assert DERIVER.for_user("ab", "c") != DERIVER.for_user("a", "bc")  # no ambiguity at the join


@pytest.mark.parametrize(
    ("value", "expected"),
    [
        ("pool-" + "0" * 40, True),
        ("pool-" + "0" * 39, False),
        ("pool-" + "0" * 41, False),
        ("pool-" + "G" * 40, False),
        ("pool-" + "A" * 40, False),
        ("sess-0001", False),
        ("x" + "pool-" + "0" * 40, False),
        ("", False),
    ],
)
def test_only_ids_in_the_derived_format_are_recognised(value: str, expected: bool) -> None:
    assert SessionIdDeriver.is_derived(value) is expected


# ------------------------------------------------------------------- creation


async def test_stateful_sessions_are_created_under_the_derived_id_and_reused() -> None:
    h = started()
    await h.pool.execute(make_request("a", "u1"))
    await h.pool.execute(make_request("a", "u1"))
    assert h.fake.create_requests == [derived("u1")] and h.fake.created == [derived("u1")]
    assert {c.session_id for c in h.fake.invocations} == {derived("u1")}
    entry = await h.affinity.get(key("u1"))
    assert entry is not None and entry.session_id == derived("u1")
    assert counter(h, "pool_sessions_created_total") == 1
    assert counter(h, "pool_sessions_restored_total") == 0


async def test_stateless_agents_keep_random_ids_and_shared_sessions() -> None:
    h = make_harness({"s": {"mode": "stateless"}}, defaults={}, session_ids=DERIVER)
    await h.pool.execute(make_request("s", "u1"))
    await h.pool.execute(make_request("s", "u2"))
    assert h.fake.create_requests == [None] and len(h.fake.created) == 1


async def test_stateful_users_never_receive_an_unbound_session() -> None:
    h = make_harness(
        {"a": {"mode": "stateful", "adopt_unbound_sessions": True}},
        defaults={},
        session_ids=DERIVER,
    )
    unbound = h.fake.add_session("a")
    await h.reconciler.run_agent("a")
    await h.pool.execute(make_request("a", "u1"))
    assert h.fake.create_requests == [derived("u1")]
    assert h.fake.invocations[0].session_id != unbound.session_id


# --------------------------------------------------------------------- restart


async def restarted_with_two_users() -> tuple[Harness, FakeFoundry]:
    h1 = started()
    await h1.pool.execute(make_request("a", "u1"))
    await h1.pool.execute(make_request("a", "u2"))
    return started(h1.fake), h1.fake


async def test_after_a_restart_the_sync_finds_each_users_session_and_holds_it_for_them() -> None:
    h2, fake = await restarted_with_two_users()
    fake.add_session("a")  # a session the service did not create
    report = await h2.reconciler.run_agent("a")
    records = {r.session_id: r for r in await h2.registry.list("a")}
    assert report.discovered == 3 and set(records) == {derived("u1"), derived("u2")}
    assert all(r.local_state is L.AVAILABLE and r.affinity_key is None for r in records.values())
    assert await h2.affinity.count("a") == 0


async def test_a_returning_user_gets_their_own_session_back_and_nobody_else_can() -> None:
    h2, fake = await restarted_with_two_users()
    await h2.reconciler.run_agent("a")
    created_before = len(fake.created)
    await h2.pool.execute(make_request("a", "u1"))
    assert len(fake.created) == created_before
    assert fake.invocations[-1].session_id == derived("u1")
    entry = await h2.affinity.get(key("u1"))
    assert entry is not None and entry.session_id == derived("u1")
    assert await h2.affinity.get(key("u2")) is None  # u2 has not come back yet
    await h2.pool.execute(make_request("a", "u3"))
    assert fake.created[-1] == derived("u3")  # u3 did not take u2's session
    assert counter(h2, "pool_sessions_restored_total") == 1
    assert counter(h2, "pool_sessions_created_total") == 1
    await h2.pool.execute(make_request("a", "u2"))
    assert fake.invocations[-1].session_id == derived("u2")


async def test_a_user_arriving_before_the_first_sync_still_gets_their_session() -> None:
    h2, fake = await restarted_with_two_users()
    created_before = len(fake.created)
    await h2.pool.execute(make_request("a", "u1"))
    assert len(fake.created) == created_before and fake.invocations[-1].session_id == derived("u1")
    assert counter(h2, "pool_sessions_restored_total") == 1
    assert counter(h2, "pool_sessions_created_total") == 0
    (record,) = await h2.registry.list("a")
    assert record.affinity_key == key("u1") and record.local_state is L.AVAILABLE


async def test_a_sync_that_runs_while_a_user_is_reclaiming_does_not_break_the_claim() -> None:
    h2, fake = await restarted_with_two_users()
    original = fake.get_session

    async def adopt_meanwhile(agent: str, session_id: str):  # type: ignore[no-untyped-def]
        session = await original(agent, session_id)
        await h2.reconciler.run_agent("a")  # registers the session behind the claim's back
        return session

    fake.get_session = adopt_meanwhile  # type: ignore[method-assign]
    await h2.pool.execute(make_request("a", "u1"))
    records = [r for r in await h2.registry.list("a") if r.session_id == derived("u1")]
    assert len(records) == 1 and records[0].affinity_key == key("u1")
    assert records[0].lease_request_id is None


async def test_a_session_still_provisioning_makes_the_user_wait_then_serves_them() -> None:
    h = started()
    fake = h.fake
    fake.add_session("a", S.CREATING, derived("u1"))
    await h.reconciler.run_agent("a")
    (record,) = await h.registry.list("a")
    assert record.local_state is L.UNAVAILABLE
    task = asyncio.create_task(h.pool.execute(make_request("a", "u1")))
    await settle(10)
    assert not task.done() and await h.queue.depth("a") == 1
    fake.set_status(derived("u1"), S.ACTIVE)
    await h.reconciler.run_agent("a")
    result = await asyncio.wait_for(task, 2)
    assert (result.status_code == 200 and fake.created == []) or fake.created == []
    assert fake.invocations[-1].session_id == derived("u1")


async def test_a_retiring_reserved_session_is_not_claimed() -> None:
    h = started()
    h.fake.add_session("a", S.ACTIVE, derived("u1"))
    await h.reconciler.run_agent("a")
    await h.registry.mark_retiring("a", derived("u1"))
    h.fake.set_status(derived("u1"), S.DELETING)
    result = await h.pool.execute(make_request("a", "u1"))
    assert result.status_code == 200
    assert h.fake.invocations[-1].session_id != derived("u1")  # a replacement was created


async def test_a_reserved_session_that_is_leased_makes_the_claimant_wait() -> None:
    h = started()
    h.fake.add_session("a", S.ACTIVE, derived("u1"))
    await h.reconciler.run_agent("a")
    assert await h.registry.try_lease("a", derived("u1"), "someone", h.clock.now()) is not None
    task = asyncio.create_task(h.pool.execute(make_request("a", "u1")))
    await settle(10)
    assert not task.done()
    await h.registry.release("a", derived("u1"), "someone", h.clock.now())
    await h.pool.notify("a")
    assert (await asyncio.wait_for(task, 2)).status_code == 200


# -------------------------------------------------------------- dead sessions


async def test_a_failed_derived_session_is_deleted_and_replaced_under_a_random_id() -> None:
    h = started()
    h.fake.add_session("a", S.FAILED, derived("u1"))
    await h.pool.execute(make_request("a", "u1"))
    assert h.fake.deleted == [derived("u1")]
    assert h.fake.create_requests == [None]  # the old id may not be reusable yet
    entry = await h.affinity.get(key("u1"))
    assert entry is not None and entry.session_id == h.fake.created[0] != derived("u1")


@pytest.mark.parametrize("status", [S.DELETING, S.DELETED, S.EXPIRED])
async def test_other_dead_derived_sessions_are_left_alone_and_replaced(status: S) -> None:
    h = started()
    h.fake.add_session("a", status, derived("u1"))
    await h.pool.execute(make_request("a", "u1"))
    assert h.fake.deleted == [] and h.fake.create_requests == [None]


async def test_a_conflict_on_the_derived_id_falls_back_to_a_random_id(
    caplog: pytest.LogCaptureFixture,
) -> None:
    caplog.set_level(logging.INFO, logger="hosted_agent_kit")
    h = started()
    h.fake.add_session("a", S.ACTIVE, derived("u1"))
    h.fake.get_errors[derived("u1")] = FoundrySessionNotFound()  # a stale read
    await h.pool.execute(make_request("a", "u1"))
    assert h.fake.create_requests == [derived("u1"), None]
    assert any(r.getMessage() == "session_id_conflict" for r in caplog.records)


async def test_a_conflict_without_a_derived_id_is_an_upstream_error() -> None:
    h = make_harness({"s": {"mode": "stateless"}}, defaults={}, session_ids=DERIVER)
    h.fake.create_errors = [FoundryConflict()]
    with pytest.raises(AppError) as info:
        await h.pool.execute(make_request("s", "u1"))
    assert info.value.code == "UPSTREAM_ERROR"


async def test_a_derived_session_that_fails_while_starting_is_retried() -> None:
    h = started()
    h.fake.add_session("a", S.CREATING, derived("u1"))
    h.fake.create_progress = [S.FAILED]
    result = await h.pool.execute(make_request("a", "u1"))
    assert result.status_code == 200
    assert h.fake.deleted == [derived("u1")]


# ------------------------------------------------------------- adoption rules


async def test_the_sync_adopts_derived_sessions_but_still_ignores_other_unknown_ones(
    caplog: pytest.LogCaptureFixture,
) -> None:
    caplog.set_level(logging.INFO, logger="hosted_agent_kit")
    h = started()
    h.fake.add_session("a", S.ACTIVE, derived("u1"))
    h.fake.add_session("a", S.ACTIVE, "sess-foreign")
    await h.reconciler.run_agent("a")
    assert [r.session_id for r in await h.registry.list("a")] == [derived("u1")]
    assert sum(r.getMessage() == "session_ignored" for r in caplog.records) == 1


async def test_without_a_key_derived_looking_sessions_follow_the_normal_adoption_rule() -> None:
    h = make_harness(AGENTS, defaults={})
    h.fake.add_session("a", S.ACTIVE, derived("u1"))
    await h.reconciler.run_agent("a")
    assert await h.registry.count("a") == 0  # stateful agents ignore unknown sessions by default


async def test_a_stateless_agent_does_not_treat_derived_ids_specially() -> None:
    h = make_harness({"s": {"mode": "stateless"}}, defaults={}, session_ids=DERIVER)
    h.fake.add_session("s", S.ACTIVE, DERIVER.for_user("s", "u1"))
    await h.reconciler.run_agent("s")
    assert await h.registry.count("s") == 1  # adopted by the stateless rule, scheduled normally
    await h.pool.execute(make_request("s", "anyone"))
    assert h.fake.created == []


# ----------------------------------------------------------- start-up checks


async def test_the_first_sync_marks_the_service_ready_whatever_its_outcome() -> None:
    h = make_harness({"a": {"mode": "stateless"}, "b": {"mode": "stateless"}}, defaults={})
    assert h.reconciler.initial_sync_done is False
    await h.reconciler.run_agent("a")
    assert h.reconciler.initial_sync_done is False  # b has not synced
    h.fake.list_fail_after = 0
    from hosted_agent_kit.domain.errors import FoundryUnavailable

    h.fake.list_error = FoundryUnavailable("down")
    report = await h.reconciler.run_agent("b")
    assert report.outcome == "incomplete" and h.reconciler.initial_sync_done is True


async def test_a_skipped_sync_does_not_count_as_the_first_sync() -> None:
    h = make_harness()
    async with h.reconciler._mutexes["stateless-agent"]:
        await h.reconciler.run_agent("stateless-agent")
    assert h.reconciler.initial_sync_done is False


def test_the_key_must_be_long_enough_and_is_never_printed() -> None:
    with pytest.raises(ConfigError, match="POOL_SESSION_ID_KEY"):
        make_settings(session_id_key="too-short")
    settings = make_settings(session_id_key="s3cret-" + "x" * 40)
    assert "s3cret" not in repr(settings) and "s3cret" not in str(settings.model_dump())


def test_a_key_with_stateful_warm_sessions_is_refused_at_start_up() -> None:
    config = make_config({"a": {"mode": "stateful", "min_warm_sessions": 1}}, {})
    settings = make_settings(session_id_key="k" * 32)
    with pytest.raises(ConfigError, match="warm"):
        session_deriver(settings, config)
    with pytest.raises(ConfigError, match="warm"):
        create_app(settings, config=config, adapter=FakeFoundry())


def test_no_key_means_no_deriver_and_stateless_warm_sessions_are_fine() -> None:
    config = make_config({"a": {"mode": "stateful", "min_warm_sessions": 1}}, {})
    assert session_deriver(make_settings(), config) is None
    ok = make_config({"s": {"mode": "stateless", "min_warm_sessions": 1}}, {})
    assert session_deriver(make_settings(session_id_key="k" * 32), ok) is not None


def test_settings_read_the_key_from_the_environment(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("POOL_AUTH_MODE", "development")
    monkeypatch.setenv("POOL_SESSION_ID_KEY", "e" * 40)
    monkeypatch.setenv("POOL_FOUNDRY_ISOLATION_KEY", "partition-1")
    settings = Settings()
    assert settings.session_id_key is not None
    assert settings.session_id_key.get_secret_value() == "e" * 40
    assert settings.foundry_isolation_key == "partition-1"


async def test_losing_the_lease_race_while_reclaiming_makes_the_user_wait() -> None:
    h = started()
    h.fake.add_session("a", S.ACTIVE, derived("u1"))
    await h.reconciler.run_agent("a")
    original = h.registry.try_lease
    refused = 0

    async def refuse_once(agent_name: str, session_id: str, request_id: str, now):  # type: ignore[no-untyped-def]
        nonlocal refused
        if refused == 0:
            refused += 1
            return None
        return await original(agent_name, session_id, request_id, now)

    h.registry.try_lease = refuse_once  # type: ignore[method-assign]
    task = asyncio.create_task(h.pool.execute(make_request("a", "u1")))
    await settle(10)
    assert not task.done() and await h.queue.depth("a") == 1
    await h.pool.notify("a")
    assert (await asyncio.wait_for(task, 2)).status_code == 200
    assert h.fake.created == []
