"""Per-user queue limits and round-robin service between users."""

from __future__ import annotations

import asyncio

import pytest

from hosted_agent_kit.domain.errors import QueueFullError
from hosted_agent_kit.ports.queue import QueueTicket
from hosted_agent_kit.services.pool import take_turns
from tests.conftest import make_harness, make_request, settle
from tests.fakes.clock import FakeClock


def ticket(request_id: str, user: str | None, loop: asyncio.AbstractEventLoop) -> QueueTicket:
    return QueueTicket(
        request_id=request_id,
        agent_name="a",
        user_id=user,
        enqueued_at=FakeClock().now(),
        pinned=False,
        future=loop.create_future(),
    )


async def test_take_turns_interleaves_users_and_keeps_each_users_order() -> None:
    loop = asyncio.get_running_loop()
    tickets = [
        ticket(i, u, loop)
        for i, u in [("1", "a"), ("2", "a"), ("3", "a"), ("4", "b"), ("5", "c"), ("6", "b")]
    ]
    order = [t.request_id for t in take_turns(tickets)]
    assert order == ["1", "4", "5", "2", "6", "3"]  # a, b, c, then a, b, then a
    assert take_turns([]) == []
    assert [t.request_id for t in take_turns(tickets[:1])] == ["1"]


async def test_a_user_cannot_have_more_requests_waiting_than_the_limit() -> None:
    h = make_harness(
        defaults={
            "mode": "stateless",
            "max_sessions": 1,
            "queue": {"max_wait_seconds": 5, "per_user_depth": 2},
        }
    )
    h.fake.invoke_gate = asyncio.Event()
    running = asyncio.create_task(h.pool.execute(make_request(user="hog")))
    await settle()
    waiting = [asyncio.create_task(h.pool.execute(make_request(user="hog"))) for _ in range(2)]
    await settle()
    with pytest.raises(QueueFullError, match="already has 2"):
        await h.pool.execute(make_request(user="hog"))
    other = asyncio.create_task(h.pool.execute(make_request(user="other")))  # not affected
    await settle()
    assert not other.done()
    h.fake.invoke_gate.set()
    results = await asyncio.gather(running, *waiting, other)
    assert all(r.status_code == 200 for r in results)


async def test_round_robin_serves_a_late_user_before_a_hogs_backlog() -> None:
    h = make_harness(
        defaults={
            "mode": "stateless",
            "max_sessions": 1,
            "queue": {"max_wait_seconds": 5, "fairness": "round_robin"},
        }
    )
    h.fake.invoke_gate = asyncio.Event()
    calls = [asyncio.create_task(h.pool.execute(make_request(user="hog", request_id="hog-0")))]
    await settle()
    for i in range(1, 4):
        calls.append(
            asyncio.create_task(h.pool.execute(make_request(user="hog", request_id=f"hog-{i}")))
        )
        await settle()
    calls.append(asyncio.create_task(h.pool.execute(make_request(user="late", request_id="late"))))
    await settle()
    h.fake.invoke_gate.set()
    await asyncio.gather(*calls)
    order = [c.request_id for c in h.fake.invocations]
    assert order[0] == "hog-0" and order[1] == "late"  # the late user is served at the next turn
    assert order[2:] == ["hog-1", "hog-2", "hog-3"]


async def test_fifo_serves_a_late_user_after_the_whole_backlog() -> None:
    h = make_harness(
        defaults={"mode": "stateless", "max_sessions": 1, "queue": {"max_wait_seconds": 5}}
    )
    h.fake.invoke_gate = asyncio.Event()
    calls = [asyncio.create_task(h.pool.execute(make_request(user="hog", request_id="hog-0")))]
    await settle()
    for i in range(1, 4):
        calls.append(
            asyncio.create_task(h.pool.execute(make_request(user="hog", request_id=f"hog-{i}")))
        )
        await settle()
    calls.append(asyncio.create_task(h.pool.execute(make_request(user="late", request_id="late"))))
    await settle()
    h.fake.invoke_gate.set()
    await asyncio.gather(*calls)
    assert [c.request_id for c in h.fake.invocations][-1] == "late"


async def test_fifo_is_the_default_and_keeps_arrival_order() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    assert h.config.agents["stateless-agent"].queue.fairness.value == "fifo"
    assert h.config.agents["stateless-agent"].queue.per_user_depth is None
