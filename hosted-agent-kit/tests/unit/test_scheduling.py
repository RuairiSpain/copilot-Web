"""The scheduling framework: plugins, profiles, the phases of a cycle and unreserving."""

from __future__ import annotations

import asyncio
from datetime import UTC, datetime, timedelta

import pytest

from hosted_agent_kit.adapters.memory_affinity import MemoryAffinityStore
from hosted_agent_kit.adapters.memory_registry import MemorySessionRegistry
from hosted_agent_kit.config.models import AgentConfig, ConfigError
from hosted_agent_kit.domain.enums import FoundrySessionStatus as P
from hosted_agent_kit.domain.enums import LocalSessionState as L
from hosted_agent_kit.domain.models import SessionAffinityKey, SessionRecord
from hosted_agent_kit.ports.queue import CreateGrant, SessionGrant
from hosted_agent_kit.services.metrics import GatedMetrics, InMemoryMetrics
from hosted_agent_kit.services.scheduling import (
    CycleState,
    PluginRegistry,
    SchedulerFramework,
    SchedulingRequest,
    default_plugins,
)
from hosted_agent_kit.services.scheduling import plugins as plug
from hosted_agent_kit.services.scheduling.framework import Profile, Wait
from hosted_agent_kit.services.scheduling.profiles import CORE_FILTERS, build_profiles
from hosted_agent_kit.services.session_ids import SessionIdDeriver
from tests.conftest import make_config, make_harness, make_request, settle

T0 = datetime(2026, 1, 1, tzinfo=UTC)
AGENT = "a"


def cfg(**agent: object) -> AgentConfig:
    return make_config({AGENT: {"mode": "stateless", **agent}}, {}).agents[AGENT]


def request(
    agent: AgentConfig | None = None,
    key: SessionAffinityKey | None = None,
    derived: bool = False,
) -> SchedulingRequest:
    return SchedulingRequest(
        agent=agent or cfg(), request_id="r1", affinity_key=key, now=T0, derived_ids=derived
    )


def rec(sid: str = "s1", **kw: object) -> SessionRecord:
    base: dict[str, object] = {
        "session_id": sid,
        "agent_name": AGENT,
        "platform_status": P.ACTIVE,
        "local_state": L.AVAILABLE,
        "created_at": T0,
        "last_seen_at": T0,
    }
    base.update(kw)
    return SessionRecord(**base)  # type: ignore[arg-type]


USER = SessionAffinityKey(user_id="u", agent_name=AGENT)
OTHER = SessionAffinityKey(user_id="v", agent_name=AGENT)


# ----------------------------------------------------------------------- filters


def test_ready_needs_an_idle_session_that_is_not_being_deleted() -> None:
    f = plug.Ready()
    assert f.filter(request(), rec(), CycleState())
    assert not f.filter(request(), rec(local_state=L.LEASED), CycleState())
    assert not f.filter(request(), rec(local_state=L.UNAVAILABLE), CycleState())
    assert not f.filter(request(), rec(deletion_timestamp=T0), CycleState())


def test_affinity_compatible_separates_users_and_keeps_stateless_to_free_sessions() -> None:
    f = plug.AffinityCompatible()
    free, mine, theirs = rec(), rec(affinity_key=USER), rec(affinity_key=OTHER)
    state = CycleState()
    assert f.filter(request(), free, state) and not f.filter(request(), mine, state)  # stateless
    assert f.filter(request(key=USER), free, state) and f.filter(request(key=USER), mine, state)
    assert not f.filter(request(key=USER), theirs, state)


def test_restore_held_keeps_free_sessions_from_users_when_ids_are_derived() -> None:
    f = plug.RestoreHeld()
    free = rec("someone-elses")
    assert f.filter(request(key=USER, derived=False), free, CycleState())  # not in derived mode
    assert f.filter(request(derived=True), free, CycleState())  # no key: not a user request
    assert not f.filter(request(key=USER, derived=True), free, CycleState())
    assert f.filter(request(key=USER, derived=True), rec(affinity_key=USER), CycleState())
    mine = CycleState(derived_id="mine")
    assert f.filter(request(key=USER, derived=True), rec("mine"), mine)  # found after a restart


def test_version_compatible_applies_only_when_a_version_is_pinned() -> None:
    f = plug.VersionCompatible()
    assert f.filter(request(), rec(agent_version="1"), CycleState())  # nothing pinned
    pinned = request(agent=cfg(agent_version="2"))
    assert f.filter(pinned, rec(agent_version="2"), CycleState())
    assert f.filter(pinned, rec(agent_version=None), CycleState())  # unknown version is allowed
    assert not f.filter(pinned, rec(agent_version="1"), CycleState())


def test_not_expiring_skips_sessions_about_to_expire() -> None:
    f = plug.NotExpiring()
    assert f.filter(request(), rec(), CycleState())
    assert f.filter(request(), rec(expires_at=T0 + timedelta(minutes=5)), CycleState())
    assert not f.filter(request(), rec(expires_at=T0 + timedelta(seconds=10)), CycleState())


# ------------------------------------------------------------------------ scores


def test_affinity_preference_version_preferred_and_expiry_risk() -> None:
    assert plug.AffinityPreference().score(request(key=USER), rec(affinity_key=USER), CycleState())
    assert not plug.AffinityPreference().score(request(key=USER), rec(), CycleState())
    assert not plug.AffinityPreference().score(request(), rec(affinity_key=USER), CycleState())
    pinned = request(agent=cfg(agent_version="2"))
    assert plug.VersionPreferred().score(pinned, rec(agent_version="2"), CycleState()) == 1
    assert plug.VersionPreferred().score(pinned, rec(agent_version="1"), CycleState()) == 0
    assert plug.VersionPreferred().score(request(), rec(agent_version="2"), CycleState()) == 0
    risk = plug.ExpiryRisk()
    assert risk.score(request(), rec(), CycleState()) == 10
    assert risk.score(request(), rec(expires_at=T0 + timedelta(hours=2)), CycleState()) == 10
    assert risk.score(request(), rec(expires_at=T0 + timedelta(minutes=30)), CycleState()) == 5
    assert risk.score(request(), rec(expires_at=T0 - timedelta(minutes=1)), CycleState()) == 0


# --------------------------------------------------------------------- registry


def test_the_default_registry_has_every_documented_plugin() -> None:
    registry = default_plugins()
    assert registry.filter_names == sorted(
        ["Ready", "AffinityCompatible", "RestoreHeld", "VersionCompatible", "NotExpiring"]
    )
    assert registry.score_names == sorted(
        [
            "FirstAvailable",
            "RoundRobin",
            "OldestIdle",
            "NewestIdle",
            "AffinityPreference",
            "VersionPreferred",
            "ExpiryRisk",
        ]
    )
    assert set(CORE_FILTERS) <= set(registry.filter_names)


def test_an_unknown_plugin_name_is_refused_with_the_known_names() -> None:
    registry = default_plugins()
    with pytest.raises(ConfigError, match=r"unknown scheduler filter 'Nope'.*Ready"):
        registry.filter("Nope")
    with pytest.raises(ConfigError, match=r"unknown scheduler score 'Nope'.*OldestIdle"):
        registry.score("Nope")


def build(agent: dict[str, object], plugins: PluginRegistry | None = None) -> dict[str, Profile]:
    config = make_config({AGENT: {"mode": "stateless", **agent}}, {})
    return build_profiles(
        config=config,
        plugins=plugins or default_plugins(),
        registry=MemorySessionRegistry(),
        affinity=MemoryAffinityStore(),
        session_ids=None,
    )


def test_core_filters_always_run_and_configured_ones_are_added() -> None:
    profile = build({"scheduler_profile": {"filters": ["NotExpiring", "Ready"]}})[AGENT]
    assert [f.name for f in profile.filters] == [*CORE_FILTERS, "NotExpiring"]
    assert [p.name for p in profile.pre_filters] == ["AffinityResolution"]


def test_the_strategy_is_the_default_score_and_a_profile_replaces_it() -> None:
    default = build({"scheduler": "oldest_idle"})[AGENT]
    assert [(s.name, w) for s, w in default.scores] == [("OldestIdle", 1)]
    custom = build({"scheduler_profile": {"scores": {"AffinityPreference": 100, "ExpiryRisk": 5}}})
    assert [(s.name, w) for s, w in custom[AGENT].scores] == [
        ("AffinityPreference", 100),
        ("ExpiryRisk", 5),
    ]


def test_an_unknown_name_in_a_profile_fails_when_the_pool_is_built() -> None:
    with pytest.raises(ConfigError, match="unknown scheduler score"):
        make_harness(defaults={"mode": "stateless", "scheduler_profile": {"scores": {"Bogus": 1}}})
    with pytest.raises(ConfigError, match="unknown scheduler filter"):
        make_harness(defaults={"mode": "stateless", "scheduler_profile": {"filters": ["Bogus"]}})


def test_profile_weights_and_keys_are_validated() -> None:
    with pytest.raises(ConfigError):
        make_config({AGENT: {"mode": "stateless", "scheduler_profile": {"scores": {"X": 0}}}}, {})
    with pytest.raises(ConfigError):
        make_config(
            {AGENT: {"mode": "stateless", "scheduler_profile": {"scores": {"X": 1001}}}}, {}
        )
    with pytest.raises(ConfigError):
        make_config({AGENT: {"mode": "stateless", "scheduler_profile": {"bogus": []}}}, {})


# --------------------------------------------------------------- whole cycles


async def test_scores_are_weighted_and_the_best_session_is_leased() -> None:
    h = make_harness(
        defaults={
            "mode": "stateless",
            "max_sessions": 5,
            "scheduler_profile": {"scores": {"VersionPreferred": 10, "OldestIdle": 1}},
            "agent_version": "2",
        }
    )
    old = h.fake.add_session("stateless-agent")  # version 1
    new = h.fake.add_session("stateless-agent")
    h.fake.sessions[new.session_id] = new.model_copy(update={"agent_version": "2"})
    await h.reconciler.run_agent("stateless-agent")
    result = await h.pool.execute(make_request())
    assert h.fake.invocations[-1].session_id == new.session_id  # the weight beats being older
    assert old.session_id != new.session_id and result.status_code == 200


async def test_a_filter_added_in_the_profile_removes_sessions_from_consideration() -> None:
    h = make_harness(
        defaults={
            "mode": "stateless",
            "max_sessions": 5,
            "agent_version": "2",
            "scheduler_profile": {"filters": ["VersionCompatible"]},
        }
    )
    stale = h.fake.add_session("stateless-agent")  # version 1: not on the pinned version
    await h.reconciler.run_agent("stateless-agent")
    await h.pool.execute(make_request())
    assert h.fake.invocations[-1].session_id != stale.session_id  # a session on version 2 was made
    assert h.fake.create_versions == ["2"]


async def test_applications_can_add_their_own_plugins() -> None:
    plugins = default_plugins()

    class OnlyEvenIds:
        name = "OnlyEvenIds"

        def filter(
            self, request: SchedulingRequest, session: SessionRecord, state: CycleState
        ) -> bool:
            return int(session.session_id.split("-")[-1]) % 2 == 0

    plugins.register_filter("OnlyEvenIds", OnlyEvenIds)
    h = make_harness(
        defaults={
            "mode": "stateless",
            "max_sessions": 5,
            "scheduler_profile": {"filters": ["OnlyEvenIds"]},
        },
        plugins=plugins,
    )
    odd = h.fake.add_session("stateless-agent")  # sess-0001
    even = h.fake.add_session("stateless-agent")  # sess-0002
    await h.reconciler.run_agent("stateless-agent")
    await h.pool.execute(make_request())
    assert (
        h.fake.invocations[-1].session_id == even.session_id and odd.session_id != even.session_id
    )


async def test_a_reload_that_changes_the_profile_takes_effect() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 5})
    new = make_config(
        {"stateless-agent": {"mode": "stateless", "max_sessions": 5, "scheduler": "newest_idle"}},
        {},
    )
    h.pool.holder.replace(new)
    h.pool.refresh_profiles()
    assert [s.name for s, _ in h.pool._framework.profile("stateless-agent").scores] == [
        "NewestIdle"
    ]


# ------------------------------------------------------------- phases directly


def framework(
    session_ids: SessionIdDeriver | None = None,
) -> tuple[SchedulerFramework, MemorySessionRegistry, MemoryAffinityStore, AgentConfig]:
    config = make_config({AGENT: {"mode": "stateless", "max_sessions": 2}}, {})
    registry, affinity = MemorySessionRegistry(), MemoryAffinityStore()
    metrics = GatedMetrics([InMemoryMetrics()], lambda a: True)
    profiles = build_profiles(
        config=config,
        plugins=default_plugins(),
        registry=registry,
        affinity=affinity,
        session_ids=session_ids,
    )
    return (
        SchedulerFramework(
            registry=registry, affinity=affinity, metrics=metrics, profiles=profiles
        ),
        registry,
        affinity,
        config.agents[AGENT],
    )


async def test_a_cycle_leases_a_free_session_creates_when_there_is_room_and_waits_when_full() -> (
    None
):
    fw, registry, _, agent = framework()
    await registry.add(rec("s1"))
    req = SchedulingRequest(
        agent=agent, request_id="r1", affinity_key=None, now=T0, derived_ids=False
    )
    first = await fw.schedule(req)
    assert first == SessionGrant("s1")
    second = await fw.schedule(
        SchedulingRequest(
            agent=agent, request_id="r2", affinity_key=None, now=T0, derived_ids=False
        )
    )
    assert isinstance(second, CreateGrant)  # one session in use, one slot free
    third = await fw.schedule(
        SchedulingRequest(
            agent=agent, request_id="r3", affinity_key=None, now=T0, derived_ids=False
        )
    )
    assert third == Wait(pinned=False)  # at capacity: wait for anyone


async def test_unreserve_gives_back_a_lease_or_a_slot_and_is_safe_to_repeat() -> None:
    fw, registry, affinity, agent = framework()
    await registry.add(rec("s1"))
    req = SchedulingRequest(
        agent=agent, request_id="r1", affinity_key=None, now=T0, derived_ids=False
    )
    lease = await fw.schedule(req)
    assert isinstance(lease, SessionGrant)
    await fw.unreserve(agent, lease, "r1", T0)
    await fw.unreserve(agent, lease, "r1", T0)  # again: nothing more to give back
    assert (await registry.get(AGENT, "s1")).local_state is L.AVAILABLE  # type: ignore[union-attr]
    await registry.try_lease(AGENT, "s1", "someone-else", T0)
    await fw.unreserve(agent, lease, "r1", T0)  # not the holder: it must not release theirs
    assert (await registry.get(AGENT, "s1")).lease_request_id == "someone-else"  # type: ignore[union-attr]

    creating = SchedulingRequest(
        agent=agent, request_id="r9", affinity_key=USER, now=T0, derived_ids=False
    )
    grant = await fw.schedule(creating)
    assert isinstance(grant, CreateGrant) and await registry.reserved(AGENT) == 1
    assert (await affinity.get(USER)) is not None  # the claim is pending
    await fw.unreserve(agent, grant, "r9", T0)
    await fw.unreserve(agent, grant, "r9", T0)
    assert await registry.reserved(AGENT) == 0 and await affinity.get(USER) is None


async def test_a_user_whose_session_is_being_created_waits_for_it() -> None:
    fw, _, affinity, agent = framework()
    await affinity.reserve(USER, "someone")
    decision = await fw.schedule(
        SchedulingRequest(agent=agent, request_id="r", affinity_key=USER, now=T0, derived_ids=False)
    )
    assert decision == Wait(pinned=True)


async def test_a_user_whose_session_is_busy_waits_for_that_one_and_never_takes_another() -> None:
    fw, registry, affinity, agent = framework()
    await registry.add(rec("mine", affinity_key=USER, local_state=L.LEASED, lease_request_id="x"))
    await registry.add(rec("free"))
    await affinity.bind(USER, "mine")
    decision = await fw.schedule(
        SchedulingRequest(agent=agent, request_id="r", affinity_key=USER, now=T0, derived_ids=False)
    )
    assert decision == Wait(pinned=True)
    assert (await registry.get(AGENT, "free")).local_state is L.AVAILABLE  # type: ignore[union-attr]


async def test_a_binding_to_a_session_that_is_going_is_dropped_and_a_new_session_is_made() -> None:
    fw, registry, affinity, agent = framework()
    await registry.add(
        rec("gone", affinity_key=USER, deletion_timestamp=T0, local_state=L.RETIRING)
    )
    await affinity.bind(USER, "gone")
    decision = await fw.schedule(
        SchedulingRequest(agent=agent, request_id="r", affinity_key=USER, now=T0, derived_ids=False)
    )
    assert isinstance(decision, CreateGrant)
    assert (await affinity.get(USER)).pending_request_id == "r"  # type: ignore[union-attr]


async def test_a_free_session_is_bound_to_the_user_who_takes_it() -> None:
    fw, registry, affinity, agent = framework()
    await registry.add(rec("free"))
    decision = await fw.schedule(
        SchedulingRequest(agent=agent, request_id="r", affinity_key=USER, now=T0, derived_ids=False)
    )
    assert decision == SessionGrant("free")
    stored = await registry.get(AGENT, "free")
    assert stored is not None and stored.affinity_key == USER
    assert (await affinity.get(USER)).session_id == "free"  # type: ignore[union-attr]


async def test_a_session_found_again_after_a_restart_is_restored_to_its_user() -> None:
    deriver = SessionIdDeriver(b"k" * 32)
    fw, registry, _, agent = framework(session_ids=deriver)
    mine = deriver.for_user(AGENT, "u")
    await registry.add(rec(mine))
    await registry.add(rec("pool-" + "0" * 40))  # another user's, held for them
    decision = await fw.schedule(
        SchedulingRequest(agent=agent, request_id="r", affinity_key=USER, now=T0, derived_ids=True)
    )
    assert decision == SessionGrant(mine)
    assert (await registry.get(AGENT, mine)).affinity_key == USER  # type: ignore[union-attr]


async def test_a_restorable_session_that_is_still_provisioning_makes_its_user_wait() -> None:
    deriver = SessionIdDeriver(b"k" * 32)
    fw, registry, _, agent = framework(session_ids=deriver)
    await registry.add(rec(deriver.for_user(AGENT, "u"), local_state=L.UNAVAILABLE))
    decision = await fw.schedule(
        SchedulingRequest(agent=agent, request_id="r", affinity_key=USER, now=T0, derived_ids=True)
    )
    assert decision == Wait(pinned=True)


async def test_waiting_requests_are_served_when_a_session_is_released() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    h.fake.invoke_gate = asyncio.Event()
    first = asyncio.create_task(h.pool.execute(make_request(user="u1")))
    await settle()
    second = asyncio.create_task(h.pool.execute(make_request(user="u2")))
    await settle()
    assert await h.queue.depth("stateless-agent") == 1
    h.fake.invoke_gate.set()
    await asyncio.gather(first, second)
    assert len(h.fake.created) == 1  # the one session served both
