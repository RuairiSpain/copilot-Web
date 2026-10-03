"""The kit budget, the adaptive limit, borrowing from the spare pool, and the regional view."""

from __future__ import annotations

import asyncio
from typing import Any

import pytest

from hosted_agent_kit import Hack, KitSettings
from hosted_agent_kit.adapters.memory_ledger import MemoryLedger
from hosted_agent_kit.config.models import ConfigError
from hosted_agent_kit.config.settings import QuotaSettings
from hosted_agent_kit.domain.errors import QUOTA_REGIONAL, QUOTA_SESSION, FoundryQuotaExceeded
from hosted_agent_kit.services.quota import KitGovernor
from hosted_agent_kit.testing import DemoFoundry, FakeClock
from tests.conftest import make_harness, make_request, settle

TWO_AGENTS: dict[str, dict[str, Any]] = {"a": {}, "b": {}}
DEFAULTS = {
    "mode": "stateless",
    "max_sessions": 10,
    "idle_timeout_seconds": 600,
    "queue": {"max_wait_seconds": 2},
}


def governor(clock: FakeClock, shared: MemoryLedger | None = None, **values: Any) -> KitGovernor:
    return KitGovernor(QuotaSettings(**values), clock, kit_id="kit-1", ledger=shared)


# ------------------------------------------------------------------ settings


def test_quota_settings_are_checked() -> None:
    with pytest.raises(ConfigError, match="region"):
        QuotaSettings(ledger={"backend": "memory"})
    with pytest.raises(ConfigError, match="ledger"):
        QuotaSettings(spare=5)
    with pytest.raises(ConfigError, match="min_limit"):
        QuotaSettings(budget=2, min_limit=3)
    ok = QuotaSettings(
        region="swedencentral", region_limit=2000, spare=100, ledger={"backend": "memory"}
    )
    assert ok.ledger is not None and ok.spare == 100


# ----------------------------------------------------------- adaptive limit


def test_without_a_budget_the_limit_is_open_until_foundry_refuses() -> None:
    clock = FakeClock()
    g = governor(clock)
    assert g.limit() is None
    g.on_refused(QUOTA_REGIONAL, active_now=40)
    assert g.limit() == 28 and g.ceiling == 40  # 70 percent of what the kit was holding


def test_the_limit_is_raised_a_step_at_a_time_back_to_where_it_was() -> None:
    clock = FakeClock()
    g = governor(clock, probe_seconds=60, increase_step=4, cooldown_seconds=0)
    g.on_refused(QUOTA_REGIONAL, active_now=40)
    assert g.limit() == 28
    g.tick()
    assert g.limit() == 28  # not yet: a refusal was just seen
    clock.advance(61)
    g.tick()
    assert g.limit() == 32
    for _ in range(2):
        clock.advance(61)
        g.tick()
    assert g.limit() == 40
    clock.advance(61)
    g.tick()
    assert g.limit() is None  # it worked at 40 before, so the cap is lifted


def test_with_a_budget_the_budget_is_the_ceiling() -> None:
    clock = FakeClock()
    g = governor(clock, budget=100, probe_seconds=30, increase_step=10, cooldown_seconds=0)
    assert g.limit() == 100
    g.on_refused(QUOTA_SESSION, active_now=100)
    assert g.limit() == 70
    clock.advance(31)
    g.tick()
    assert g.limit() == 80
    for _ in range(5):
        clock.advance(31)
        g.tick()
    assert g.limit() == 100  # never above the budget
    clock.advance(31)
    g.tick()
    assert g.limit() == 100


def test_refusals_during_the_cooldown_do_not_lower_it_again() -> None:
    clock = FakeClock()
    g = governor(clock, budget=100, cooldown_seconds=30)
    g.on_refused(QUOTA_REGIONAL, 100)
    g.on_refused(QUOTA_REGIONAL, 100)
    g.on_refused(QUOTA_REGIONAL, 100)
    assert g.limit() == 70 and g.refusals == 3
    clock.advance(31)
    g.on_refused(QUOTA_REGIONAL, 70)
    assert g.limit() == 49


def test_the_limit_never_goes_below_the_minimum() -> None:
    clock = FakeClock()
    g = governor(clock, budget=10, min_limit=4, cooldown_seconds=0)
    for _ in range(10):
        g.on_refused(QUOTA_REGIONAL, 10)
    assert g.limit() == 4


# ------------------------------------------------------------ kit budget


async def test_the_budget_is_shared_by_all_the_kits_agents() -> None:
    h = make_harness(TWO_AGENTS, DEFAULTS, quota={"budget": 2})
    h.fake.invoke_gate = asyncio.Event()
    calls = [
        asyncio.create_task(h.pool.execute(make_request("a", "u1"))),
        asyncio.create_task(h.pool.execute(make_request("b", "u2"))),
    ]
    await settle()
    assert len(h.fake.created) == 2
    third = asyncio.create_task(h.pool.execute(make_request("a", "u3")))
    await settle()
    assert len(h.fake.created) == 2  # the budget of 2 is used up, though agent a has room
    h.fake.invoke_gate.set()
    await asyncio.gather(*calls, third)
    assert len(h.fake.created) == 2  # the third call reused a freed session
    view = await h.pool.quota.kit_counted(h.clock.now())
    assert view == 2


async def test_a_full_budget_stops_an_idle_session_of_another_agent() -> None:
    h = make_harness(TWO_AGENTS, DEFAULTS, quota={"budget": 2})
    h.fake.invoke_gate = asyncio.Event()
    calls = [asyncio.create_task(h.pool.execute(make_request("a", user))) for user in ("u1", "u2")]
    await settle()
    h.fake.invoke_gate.set()
    await asyncio.gather(*calls)  # two sessions of agent a, both idle and counted
    assert len(h.fake.created) == 2
    await h.pool.execute(make_request("b", "u3"))  # agent b has none, and the budget is full
    assert len(h.fake.stopped) == 1 and h.fake.stopped[0] in h.fake.created
    assert h.fake.deleted == []  # stopped, not deleted
    assert len(h.fake.created) == 3


async def test_a_quota_refusal_lowers_the_kits_limit() -> None:
    h = make_harness(TWO_AGENTS, DEFAULTS, quota={"budget": 10, "cooldown_seconds": 0})
    h.fake.create_errors = [FoundryQuotaExceeded(QUOTA_SESSION)]
    with pytest.raises(Exception, match="quota"):
        await h.pool.execute(make_request("a", "u1"))
    limit = h.pool.quota.governor.limit()
    assert limit is not None and limit < 10  # lowered from the budget
    assert h.pool.quota.governor.refusals == 1


async def test_the_ticker_serves_a_waiter_once_the_limit_rises() -> None:
    h = make_harness(
        TWO_AGENTS,
        {**DEFAULTS, "queue": {"max_wait_seconds": 30}},
        quota={"budget": 10, "min_limit": 1, "probe_seconds": 60, "cooldown_seconds": 0},
        evict_idle_for_quota=False,
    )
    await h.pool.execute(make_request("a", "u1"))  # one session is counted
    h.pool.quota.governor.on_refused(QUOTA_REGIONAL, active_now=1)  # lowers the limit to 1
    assert h.pool.quota.governor.limit() == 1
    h.fake.invoke_gate = asyncio.Event()
    busy = asyncio.create_task(h.pool.execute(make_request("a", "u1")))
    await settle()
    waiter = asyncio.create_task(h.pool.execute(make_request("b", "u2")))
    await settle()
    assert not waiter.done()  # no room for b's session while a's holds the only unit
    h.fake.invoke_gate.set()
    await busy
    h.clock.advance(61)
    await h.pool.quota.tick()  # the limit is raised one step
    await h.pool.dispatch_waiting()
    assert (await asyncio.wait_for(waiter, 2)).status_code == 200


# ------------------------------------------------------------- borrowing


async def test_a_full_kit_borrows_from_the_spare_pool_and_gives_it_back() -> None:
    clock = FakeClock()
    ledger = MemoryLedger(clock)
    g = governor(
        clock,
        ledger,
        budget=2,
        spare=2,
        region="r",
        region_limit=10,
        ledger={"backend": "memory"},
    )
    assert g.limit() == 2 and g.borrowed == 0
    assert await g.try_borrow() and await g.try_borrow()
    assert g.limit() == 4 and g.borrowed == 2
    assert not await g.try_borrow()  # the pool of 2 is empty
    await g.rebalance(counted=3)  # needs 3: keeps one permit, returns one
    assert g.borrowed == 1 and g.limit() == 3
    await g.rebalance(counted=1)
    assert g.borrowed == 0 and g.limit() == 2
    snap = await ledger.snapshot(2, 10)
    assert snap.spare_used == 0


async def test_a_second_kit_cannot_borrow_what_the_first_holds() -> None:
    clock = FakeClock()
    ledger = MemoryLedger(clock)
    options: dict[str, Any] = {
        "budget": 2,
        "spare": 1,
        "region": "r",
        "region_limit": 10,
        "ledger": {"backend": "memory"},
    }
    one = KitGovernor(QuotaSettings(**options), clock, kit_id="one", ledger=ledger)
    two = KitGovernor(QuotaSettings(**options), clock, kit_id="two", ledger=ledger)
    assert await one.try_borrow()
    assert not await two.try_borrow()
    await one.rebalance(counted=0)
    assert await two.try_borrow()


async def test_a_ledger_outage_leaves_the_kit_on_its_own_budget() -> None:
    clock = FakeClock()
    ledger = MemoryLedger(clock)
    g = governor(
        clock, ledger, budget=2, spare=3, region="r", region_limit=10, ledger={"backend": "memory"}
    )
    assert await g.try_borrow()
    ledger.available = False
    assert not await g.try_borrow()  # no permit, no error
    await g.heartbeat(5)  # a failed heartbeat is only logged
    await g.rebalance(0)  # the permit cannot be returned now, and is kept
    assert g.borrowed == 1
    ledger.available = True
    await g.rebalance(0)
    assert g.borrowed == 0


async def test_borrowed_permits_let_a_kit_exceed_its_budget() -> None:
    clock = FakeClock()
    ledger = MemoryLedger(clock)
    h = make_harness(
        TWO_AGENTS,
        DEFAULTS,
        fake=DemoFoundry(clock),
        quota={
            "budget": 1,
            "spare": 1,
            "region": "r",
            "region_limit": 10,
            "ledger": {"backend": "memory"},
        },
        evict_idle_for_quota=False,
    )
    h.pool.quota.governor._ledger = ledger
    h.fake.invoke_gate = asyncio.Event()
    calls = [
        asyncio.create_task(h.pool.execute(make_request("a", "u1"))),
        asyncio.create_task(h.pool.execute(make_request("b", "u2"))),
    ]
    await settle()
    assert len(h.fake.created) == 2  # one session on the budget, one on a borrowed permit
    assert h.pool.quota.governor.borrowed == 1
    h.fake.invoke_gate.set()
    await asyncio.gather(*calls)


# ----------------------------------------------------- regional view (kits)


async def test_two_kits_publish_to_one_ledger_and_see_the_regional_total() -> None:
    clock = FakeClock()
    ledger = MemoryLedger(clock)
    doc = {"agentPool": {"agents": {"chat": {"mode": "stateless", "max_sessions": 5}}}}

    def make(kit_id: str) -> Hack:
        settings = KitSettings(
            kit_id=kit_id,
            quota={
                "region": "swedencentral",
                "region_limit": 2000,
                "spare": 50,
                "ledger": {"backend": "memory"},
            },
            quota_tick_seconds=0.05,
            startup_sync_timeout_seconds=5,
        )
        return Hack.from_dict(
            doc, adapter=DemoFoundry(clock), settings=settings, clock=clock, ledger=ledger
        )

    one, two = make("team-a"), make("team-b")
    async with one, two:
        for i in range(3):
            await one.ask("chat", "x", user_id=f"u{i}")
        await two.ask("chat", "x", user_id="v")
        await one.runtime.pool.quota.tick()
        await two.runtime.pool.quota.tick()
        view = await one.reporting.quota()
        assert view.kit_id == "team-a" and view.counted >= 1
        assert view.regional is not None
        assert set(view.regional.kits) == {"team-a", "team-b"}
        assert view.regional.total_active == sum(view.regional.kits.values()) >= 2
        assert view.regional.region_limit == 2000 and view.regional.headroom is not None
        assert view.regional.headroom == 2000 - view.regional.total_active
    snap = await ledger.snapshot(50, 2000)
    assert snap.kits == {}  # both kits removed their counts on shutdown


async def test_reporting_works_without_a_ledger_and_survives_one_that_is_down() -> None:
    clock = FakeClock()
    ledger = MemoryLedger(clock)
    doc = {"agentPool": {"agents": {"chat": {"mode": "stateless"}}}}
    plain = Hack.from_dict(doc, adapter=DemoFoundry(), settings=KitSettings())
    async with plain:
        assert (await plain.reporting.quota()).regional is None
    settings = KitSettings(
        quota={"region": "r", "region_limit": 100, "ledger": {"backend": "memory"}}
    )
    kit = Hack.from_dict(doc, adapter=DemoFoundry(), settings=settings, ledger=ledger)
    async with kit:
        ledger.available = False
        view = await kit.reporting.quota()
        assert view.regional is None and view.kit_id.startswith("kit-")
        assert kit.reporting.health()["kit_id"] == view.kit_id


async def test_a_permit_that_lapsed_in_the_ledger_is_no_longer_counted_by_the_kit() -> None:
    clock = FakeClock()
    ledger = MemoryLedger(clock, ttl_seconds=30)
    g = governor(
        clock, ledger, budget=2, spare=3, region="r", region_limit=10, ledger={"backend": "memory"}
    )
    assert await g.try_borrow() and g.limit() == 3
    clock.advance(31)  # no heartbeat for longer than the permit lives
    await g.heartbeat(0)
    assert g.borrowed == 0 and g.limit() == 2
    assert await ledger.snapshot(3, 10) is not None


async def test_permits_expire_locally_when_the_ledger_stays_unreachable() -> None:
    clock = FakeClock()
    ledger = MemoryLedger(clock, ttl_seconds=30)
    g = governor(
        clock,
        ledger,
        budget=2,
        spare=3,
        region="r",
        region_limit=10,
        ledger={"backend": "memory", "ttl_seconds": 30},
    )
    assert await g.try_borrow()
    ledger.available = False
    clock.advance(20)
    await g.heartbeat(0)
    assert g.borrowed == 1  # not yet: it may still be valid
    clock.advance(11)
    await g.heartbeat(0)
    assert g.borrowed == 0  # past its time to live without an extension
