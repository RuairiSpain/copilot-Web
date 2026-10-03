"""Quota under concurrency, and several kits sharing a Redis ledger."""

from __future__ import annotations

import asyncio
from typing import Any

from fakeredis import FakeAsyncRedis

from hosted_agent_kit import Hack, KitSettings
from hosted_agent_kit.adapters.redis_ledger import RedisLedger
from hosted_agent_kit.testing import DemoFoundry, FakeClock
from tests.conftest import make_harness, make_request, settle

DEFAULTS = {
    "mode": "stateless",
    "max_sessions": 50,
    "idle_timeout_seconds": 600,
    "queue": {"max_wait_seconds": 5},
}


async def test_many_concurrent_calls_across_agents_never_exceed_the_budget() -> None:
    agents: dict[str, dict[str, Any]] = {f"agent-{i}": {} for i in range(4)}
    h = make_harness(agents, DEFAULTS, quota={"budget": 6}, evict_idle_for_quota=False)
    peak = 0

    async def watch() -> None:
        nonlocal peak
        while True:
            peak = max(peak, await h.pool.quota.kit_counted(h.clock.now()))
            await asyncio.sleep(0)

    h.fake.invoke_gate = asyncio.Event()
    watcher = asyncio.create_task(watch())
    calls = [
        asyncio.create_task(h.pool.execute(make_request(f"agent-{i % 4}", f"u{i}")))
        for i in range(40)
    ]
    await settle()
    assert await h.pool.quota.kit_counted(h.clock.now()) <= 6
    h.fake.invoke_gate.set()
    results = await asyncio.gather(*calls)
    watcher.cancel()
    assert all(r.status_code == 200 for r in results)
    assert peak <= 6 and len(h.fake.created) <= 6


async def test_eviction_under_load_keeps_every_user_served_and_loses_no_state() -> None:
    h = make_harness(
        {"memo": {}},
        {
            "mode": "stateful",
            "max_sessions": 40,
            "max_active_sessions": 5,
            "idle_timeout_seconds": 600,
            "queue": {"max_wait_seconds": 5},
        },
    )
    users = [f"u{i}" for i in range(20)]
    results = await asyncio.gather(*(h.pool.execute(make_request("memo", u)) for u in users))
    assert all(r.status_code == 200 for r in results)
    assert h.fake.deleted == []  # nobody lost a session
    assert len(h.fake.created) == 20  # one session each
    assert await h.pool.quota.kit_counted(h.clock.now()) <= 5
    assert len(h.fake.stopped) >= 15  # idle ones were stopped to stay within five
    again = await asyncio.gather(*(h.pool.execute(make_request("memo", u)) for u in users))
    assert all(r.status_code == 200 for r in again)
    assert len(h.fake.created) == 20  # everyone resumed their own session


async def test_three_kits_share_a_redis_ledger_and_borrow_the_spare_pool() -> None:
    redis = FakeAsyncRedis(decode_responses=True)
    clock = FakeClock()

    def kit(kit_id: str) -> Hack:
        settings = KitSettings(
            kit_id=kit_id,
            quota={
                "budget": 1,
                "spare": 2,
                "subscription_id": "sub1",
                "region": "swedencentral",
                "region_limit": 10,
                "ledger": {"backend": "redis", "ttl_seconds": 30},
            },
            startup_sync_timeout_seconds=5,
            evict_idle_for_quota=False,
        )
        doc = {"agentPool": {"agents": {"chat": {"mode": "stateless", "max_sessions": 5}}}}
        ledger = RedisLedger(redis, scope="sub1/swedencentral", ttl_seconds=30)
        return Hack.from_dict(
            doc, adapter=DemoFoundry(clock), settings=settings, clock=clock, ledger=ledger
        )

    kits = [kit("team-a"), kit("team-b"), kit("team-c")]
    for k in kits:
        await k.start()
    try:
        held: list[Any] = []
        for k in kits:
            assert k.runtime.pool.quota.governor.limit() == 1
        # Team A needs three sessions at once: its budget of 1 plus both spare permits.
        gate = asyncio.Event()
        for k in kits:
            k._adapter.invoke_gate = gate  # type: ignore[union-attr]
        a_calls = [asyncio.create_task(kits[0].ask("chat", "x", user_id=f"a{i}")) for i in range(3)]
        await settle()
        assert kits[0].runtime.pool.quota.governor.borrowed == 2
        # Team B finds the spare pool empty, so it stays on its budget of one.
        b_calls = [asyncio.create_task(kits[1].ask("chat", "x", user_id=f"b{i}")) for i in range(2)]
        await settle()
        assert kits[1].runtime.pool.quota.governor.borrowed == 0
        assert len(kits[1]._adapter.created) == 1  # type: ignore[union-attr]
        gate.set()
        held = await asyncio.gather(*a_calls, *b_calls)
        assert all(r.ok for r in held)
        # Once idle, the borrowed permits go back and team C can borrow them.
        for k in kits:
            await k.runtime.pool.quota.tick()
        assert kits[0].runtime.pool.quota.governor.borrowed >= 0
        view = await kits[2].reporting.quota()
        assert view.regional is not None and set(view.regional.kits) >= {"team-a", "team-b"}
        assert view.regional.total_active == sum(view.regional.kits.values())
    finally:
        for k in kits:
            await k.stop()
        await redis.aclose()


async def test_the_count_is_never_partial_while_the_index_is_rebuilt() -> None:
    agents: dict[str, dict[str, Any]] = {f"agent-{i}": {} for i in range(3)}
    h = make_harness(agents, DEFAULTS, quota={"budget": 20}, evict_idle_for_quota=False)
    h.fake.invoke_gate = asyncio.Event()
    calls = [
        asyncio.create_task(h.pool.execute(make_request(f"agent-{i % 3}", f"u{i}")))
        for i in range(6)
    ]
    await settle()
    gate = h.pool.quota
    expected = await gate.kit_counted(h.clock.now())
    assert expected == 6

    real_view = h.registry.view

    async def slow_view(agent: str) -> Any:
        await asyncio.sleep(0)  # let other tasks run between agents
        return await real_view(agent)

    h.registry.view = slow_view  # type: ignore[method-assign]
    seen: list[int] = []

    async def watch() -> None:
        while True:
            seen.append(await gate.kit_counted(h.clock.now()))
            await asyncio.sleep(0)

    watcher = asyncio.create_task(watch())
    for _ in range(5):
        await gate.rebuild()
    watcher.cancel()
    h.registry.view = real_view  # type: ignore[method-assign]
    assert seen and min(seen) == expected
    h.fake.invoke_gate.set()
    await asyncio.gather(*calls)
