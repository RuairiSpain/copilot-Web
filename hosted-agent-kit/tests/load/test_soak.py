"""2,000 sessions and 2,000 open streams in one process (the regional limit of most regions)."""

from __future__ import annotations

import asyncio
import time
import tracemalloc

import pytest

from hosted_agent_kit import Hack, KitSettings
from hosted_agent_kit.testing import DemoFoundry, FakeClock

SESSIONS = 2000
DOC = {
    "agentPool": {
        "agents": {
            "chat": {
                "mode": "stateless",
                "max_sessions": SESSIONS,
                "queue": {"enabled": False},
                "create_retries": 0,
            }
        }
    }
}


@pytest.mark.load
async def test_two_thousand_concurrent_streams_use_two_thousand_sessions() -> None:
    kit = Hack.from_dict(
        DOC,
        adapter=DemoFoundry(FakeClock()),
        settings=KitSettings(startup_sync_timeout_seconds=5, quota_tick_seconds=0.5),
        clock=FakeClock(),
    )
    tracemalloc.start()
    async with kit:
        started = time.perf_counter()
        results = await asyncio.gather(
            *(
                kit.responses("chat", user_id=f"u{i}", input={"input": "a b c"}, stream=True)
                for i in range(SESSIONS)
            )
        )
        opened = time.perf_counter() - started
        summary = (await kit.reporting.agents())[0]
        assert summary.sessions_total == SESSIONS and summary.sessions_leased == SESSIONS
        assert summary.sessions_counted == SESSIONS
        quota = await kit.reporting.quota()
        assert quota.counted == SESSIONS

        async def drain(result_index: int) -> int:
            return len([event async for event in results[result_index].events()])

        counts = await asyncio.gather(*(drain(i) for i in range(SESSIONS)))
        assert all(count == 7 for count in counts)  # six words and the completion event
        drained = time.perf_counter() - started
        after = (await kit.reporting.agents())[0]
        assert after.sessions_leased == 0 and after.sessions_total == SESSIONS
        current, peak = tracemalloc.get_traced_memory()
    tracemalloc.stop()
    print(
        f"opened {SESSIONS} streams in {opened:.1f}s, drained in {drained:.1f}s, "
        f"peak {peak / 1e6:.0f} MB"
    )
    # Generous bounds: they catch a leak or a quadratic step, not a slow machine.
    assert opened < 60 and drained < 90
    assert peak < 600 * 1024 * 1024, f"peak memory {peak / 1e6:.0f} MB"
    assert current < 400 * 1024 * 1024


@pytest.mark.load
async def test_the_ledger_and_counting_stay_cheap_at_two_thousand_sessions() -> None:
    kit = Hack.from_dict(
        DOC,
        adapter=DemoFoundry(FakeClock()),
        settings=KitSettings(startup_sync_timeout_seconds=5),
        clock=FakeClock(),
    )
    async with kit:
        for i in range(0, SESSIONS, 250):
            await asyncio.gather(*(kit.ask("chat", "x", user_id=f"u{i + j}") for j in range(250)))
        started = time.perf_counter()
        for _ in range(20):
            await kit.runtime.pool.quota.kit_counted(kit.runtime.pool.clock.now())
        per_count = (time.perf_counter() - started) / 20
    assert per_count < 0.25  # counting 2,000 sessions takes milliseconds
