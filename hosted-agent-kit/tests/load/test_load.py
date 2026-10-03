"""Load behaviour: concurrency, isolation, lease exclusivity and memory stability."""

from __future__ import annotations

import asyncio
import gc
import random
import tracemalloc
from collections import Counter
from typing import Any

import pytest

from hosted_agent_kit.domain.enums import LocalSessionState
from hosted_agent_kit.domain.errors import AppError
from hosted_agent_kit.domain.models import InvokeContext
from hosted_agent_kit.ports.foundry import UpstreamResponse
from tests.conftest import Harness, make_harness, make_request, settle
from tests.fakes.foundry import FakeFoundry
from tests.integration.conftest import start_api

pytestmark = pytest.mark.load


class TrackingFoundry(FakeFoundry):
    """Records the highest number of simultaneous invocations seen per session."""

    def __init__(self) -> None:
        super().__init__()
        self.active: Counter[str] = Counter()
        self.peak: Counter[str] = Counter()
        self.total: Counter[str] = Counter()
        self.delay = 0.0

    async def invoke(self, context: InvokeContext) -> UpstreamResponse:
        sid = context.session_id
        self.active[sid] += 1
        self.peak[sid] = max(self.peak[sid], self.active[sid])
        self.total[sid] += 1
        try:
            await asyncio.sleep(self.delay)
            return UpstreamResponse(body={"status": "completed"})
        finally:
            self.active[sid] -= 1


def tracked_harness(
    agents: dict[str, dict[str, Any]] | None = None,
    defaults: dict[str, Any] | None = None,
) -> tuple[Harness, TrackingFoundry]:
    harness = make_harness(agents, defaults)
    tracker = TrackingFoundry()
    tracker.clock = harness.clock
    harness.fake = tracker  # type: ignore[assignment]
    harness.pool._adapter = tracker
    harness.pool._remote._adapter = tracker
    harness.reconciler._adapter = tracker
    return harness, tracker


async def test_100_concurrent_requests_never_share_a_session_and_respect_capacity() -> None:
    h, fake = tracked_harness(defaults={"mode": "stateless", "max_sessions": 10})
    fake.delay = 0.002
    results = await asyncio.gather(
        *(h.pool.execute(make_request(user=f"u{i}")) for i in range(100))
    )
    assert len(results) == 100 and all(r.status_code == 200 for r in results)
    assert sum(fake.total.values()) == 100
    assert max(fake.peak.values()) == 1, "a session served two requests at once"
    assert len(fake.created) <= 10 and await h.registry.count("stateless-agent") <= 10
    assert await h.queue.depth("stateless-agent") == 0
    assert await h.registry.reserved("stateless-agent") == 0
    records = await h.registry.list("stateless-agent")
    assert all(
        r.local_state is LocalSessionState.AVAILABLE and r.lease_request_id is None for r in records
    )


async def test_100_concurrent_stateful_requests_keep_one_session_per_user() -> None:
    h, fake = tracked_harness(
        agents={"coding-agent": {"mode": "stateful", "max_sessions": 20}}, defaults={}
    )
    fake.delay = 0.001
    users = [f"user-{i % 20}" for i in range(100)]  # 20 users, 5 requests each
    await asyncio.gather(*(h.pool.execute(make_request("coding-agent", u)) for u in users))
    assert len(fake.created) == 20, "exactly one session per user"
    assert max(fake.peak.values()) == 1
    sessions_per_user: dict[str, set[str]] = {}
    for record in await h.registry.list("coding-agent"):
        assert record.affinity_key is not None
        sessions_per_user.setdefault(record.affinity_key.user_id, set()).add(record.session_id)
    assert len(sessions_per_user) == 20 and all(len(s) == 1 for s in sessions_per_user.values())
    assert sorted(fake.total.values()) == [5] * 20


async def test_stateful_users_beyond_capacity_are_queued_without_duplicate_sessions() -> None:
    h, fake = tracked_harness(
        agents={
            "coding-agent": {
                "mode": "stateful",
                "max_sessions": 5,
                "queue": {"max_wait_seconds": 0.5},
            }
        },
        defaults={},
    )
    fake.delay = 0.001
    outcomes = await asyncio.gather(
        *(h.pool.execute(make_request("coding-agent", f"u{i % 8}")) for i in range(64)),
        return_exceptions=True,
    )
    assert max(fake.peak.values()) == 1
    assert len(fake.created) <= 5 and await h.registry.count("coding-agent") <= 5
    for outcome in outcomes:
        assert not isinstance(outcome, BaseException) or isinstance(outcome, AppError)
    assert await h.queue.depth("coding-agent") == 0
    assert await h.registry.reserved("coding-agent") == 0


async def test_a_saturated_agent_does_not_delay_or_reject_another_agent() -> None:
    h, fake = tracked_harness(
        agents={
            "slow": {"mode": "stateless", "max_sessions": 1, "queue": {"max_depth": 3}},
            "fast": {"mode": "stateless", "max_sessions": 4},
        },
        defaults={},
    )
    gate = asyncio.Event()
    original = fake.invoke

    async def invoke(context: InvokeContext) -> UpstreamResponse:
        if context.agent_name == "slow":
            await gate.wait()
        result: UpstreamResponse = await original(context)
        return result

    fake.invoke = invoke  # type: ignore[method-assign]
    slow = [asyncio.create_task(h.pool.execute(make_request("slow", f"s{i}"))) for i in range(4)]
    await settle(10)
    assert await h.queue.depth("slow") == 3  # one running, three queued: full
    with pytest.raises(AppError) as full:
        await h.pool.execute(make_request("slow", "overflow"))
    assert full.value.code == "QUEUE_FULL"
    started = asyncio.get_running_loop().time()
    fast = await asyncio.gather(*(h.pool.execute(make_request("fast", f"f{i}")) for i in range(50)))
    elapsed = asyncio.get_running_loop().time() - started
    assert all(r.status_code == 200 for r in fast)
    assert elapsed < 2.0, "fast agent was held up by the saturated one"
    assert await h.queue.depth("fast") == 0 and await h.queue.depth("slow") == 3
    gate.set()
    assert all(r.status_code == 200 for r in await asyncio.gather(*slow))


async def test_cancellation_storm_leaves_no_leases_waiters_or_reservations() -> None:
    h, fake = tracked_harness(defaults={"mode": "stateless", "max_sessions": 4})
    fake.delay = 0.01
    rng = random.Random(1234)
    tasks = [asyncio.create_task(h.pool.execute(make_request(user=f"u{i}"))) for i in range(100)]
    await asyncio.sleep(0.005)
    for task in rng.sample(tasks, 50):
        task.cancel()
        await asyncio.sleep(0)
    outcomes = await asyncio.gather(*tasks, return_exceptions=True)
    assert any(isinstance(o, asyncio.CancelledError) for o in outcomes)
    assert max(fake.peak.values()) == 1
    assert await h.queue.depth("stateless-agent") == 0
    assert await h.registry.reserved("stateless-agent") == 0
    records = await h.registry.list("stateless-agent")
    assert all(r.lease_request_id is None for r in records)
    assert len(records) <= 4
    assert (
        await h.pool.execute(make_request(user="after"))
    ).status_code == 200  # pool still serves


async def test_memory_and_task_counts_are_stable_across_repeated_bursts() -> None:
    h, fake = tracked_harness(defaults={"mode": "stateless", "max_sessions": 8})
    fake.delay = 0.0

    async def burst() -> None:
        await asyncio.gather(*(h.pool.execute(make_request(user=f"u{i}")) for i in range(100)))

    for _ in range(3):  # warm caches before measuring
        await burst()
    gc.collect()
    tracemalloc.start()
    try:
        baseline = tracemalloc.take_snapshot()
        for _ in range(10):
            await burst()
        gc.collect()
        growth = sum(
            stat.size_diff for stat in tracemalloc.take_snapshot().compare_to(baseline, "filename")
        )
    finally:
        tracemalloc.stop()
    assert growth < 1_000_000, f"retained {growth} bytes after 1000 extra requests"
    assert len(asyncio.all_tasks()) == 1, "tasks leaked after the bursts"
    assert await h.registry.count("stateless-agent") <= 8
    assert await h.queue.depth("stateless-agent") == 0


async def test_stateful_affinity_store_is_bounded_by_users_not_requests() -> None:
    h, fake = tracked_harness(
        agents={"coding-agent": {"mode": "stateful", "max_sessions": 10}}, defaults={}
    )
    for _ in range(20):
        await asyncio.gather(
            *(h.pool.execute(make_request("coding-agent", f"u{i}")) for i in range(10))
        )
    assert await h.affinity.count("coding-agent") == 10
    assert len(fake.created) == 10 and sum(fake.total.values()) == 200


async def test_100_concurrent_http_requests_end_to_end() -> None:
    api, manager = await start_api(
        agents={"research-agent": {"mode": "stateless", "max_sessions": 10}}, defaults={}
    )
    try:
        body = {"input": {"input": "load"}}
        responses = await asyncio.gather(
            *(
                api.client.post(
                    "/v1/agents/research-agent/invoke", json=body, headers=api.headers(f"u{i}")
                )
                for i in range(100)
            )
        )
        assert Counter(r.status_code for r in responses) == {200: 100}
        assert len(api.fake.created) <= 10
        assert len({r.headers["x-request-id"] for r in responses}) == 100
        snapshot = await api.container.pool.snapshot("research-agent")
        assert snapshot.sessions_leased == 0 and snapshot.queue_depth == 0
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)
