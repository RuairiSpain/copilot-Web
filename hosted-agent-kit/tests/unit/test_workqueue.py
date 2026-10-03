"""The keyed work queue: deduplication, the dirty bit, backoff and delayed keys."""

from __future__ import annotations

import asyncio

from hosted_agent_kit.controllers.workqueue import WorkQueue
from tests.conftest import settle
from tests.fakes.clock import FakeClock


def queue(clock: FakeClock | None = None, **kw: float | None) -> tuple[WorkQueue[str], FakeClock]:
    clock = clock or FakeClock()
    return WorkQueue(clock, **kw), clock  # type: ignore[arg-type]


def drain(q: WorkQueue[str]) -> list[str]:
    out = []
    while (key := q.get_nowait()) is not None:
        out.append(key)
        q.done(key)
    return out


def test_a_key_is_queued_once_however_often_it_is_added() -> None:
    q, _ = queue()
    for _ in range(5):
        q.add("a")
    q.add("b")
    assert len(q) == 2 and drain(q) == ["a", "b"]  # first in, first out


def test_a_key_added_while_it_is_processed_is_queued_again_once() -> None:
    q, _ = queue()
    q.add("a")
    key = q.get_nowait()
    assert key == "a"
    q.add("a")
    q.add("a")  # two changes during the run still need only one more run
    assert len(q) == 0 and not q.idle  # held back until the run ends
    q.done("a")
    assert len(q) == 1 and drain(q) == ["a"]


def test_idle_means_nothing_queued_or_running() -> None:
    q, _ = queue()
    assert q.idle
    q.add("a")
    assert not q.idle
    key = q.get_nowait()
    assert not q.idle
    q.done(key or "")
    assert q.idle


def test_failures_back_off_exponentially_up_to_a_cap_and_forget_resets() -> None:
    q, clock = queue(base_delay=1, max_delay=5)
    delays = [q.add_rate_limited("a") for _ in range(5)]
    assert delays == [1, 2, 4, 5, 5]
    assert q.num_requeues("a") == 5
    q.forget("a")
    assert q.num_requeues("a") == 0 and q.add_rate_limited("a") == 1
    assert q.release_due() == 0  # nothing is due yet
    clock.advance(1)
    assert q.release_due() == 1 and drain(q) == ["a"]


def test_retries_run_out_after_the_limit() -> None:
    q, _ = queue(base_delay=1, max_retries=2)
    assert q.add_rate_limited("a") == 1 and q.add_rate_limited("a") == 2
    assert q.add_rate_limited("a") is None  # gave up, and the count starts again
    assert q.num_requeues("a") == 0


def test_the_earliest_delay_wins() -> None:
    q, clock = queue()
    q.add_after("a", 10)
    q.add_after("a", 3)
    q.add_after("a", 30)  # a later request does not push it back
    clock.advance(3)
    assert q.release_due() == 1 and drain(q) == ["a"]
    assert q.delayed_keys() == []


def test_a_zero_delay_queues_at_once() -> None:
    q, _ = queue()
    q.add_after("a", 0)
    assert drain(q) == ["a"]


async def test_get_waits_for_a_key_and_returns_none_after_shutdown() -> None:
    q, _ = queue()
    got: list[str | None] = []

    async def consumer() -> None:
        got.append(await q.get())
        got.append(await q.get())

    task = asyncio.create_task(consumer())
    await settle()
    assert got == []  # waiting
    q.add("a")
    await settle()
    assert got == ["a"]
    q.done("a")
    q.shutdown()
    await asyncio.wait_for(task, 1)
    assert got == ["a", None]
    q.add("late")  # a stopped queue takes no more work
    assert len(q) == 0


async def test_wait_idle_returns_when_the_last_key_is_done() -> None:
    q, _ = queue()
    q.add("a")
    waiter = asyncio.create_task(q.wait_idle())
    await settle()
    assert not waiter.done()
    key = q.get_nowait()
    await settle()
    assert not waiter.done()
    q.done(key or "")
    await asyncio.wait_for(waiter, 1)


async def test_timers_release_delayed_keys_on_the_clock_when_enabled() -> None:
    q, clock = queue()
    q.add_after("early", 5)  # delayed before timers were enabled
    q.enable_timers()
    q.add_after("later", 7)
    await settle(10)
    assert clock.sleeps  # the timers slept on the injected clock
    assert sorted(drain(q)) == ["early", "later"]
    q.add_after("again", 2)
    q.add_after("again", 1)  # a sooner request replaces the timer
    await settle(10)
    assert drain(q) == ["again"]
    q.shutdown()


async def test_a_queue_can_be_reopened_after_shutdown() -> None:
    q, _ = queue()
    q.shutdown()
    q.add("a")
    assert len(q) == 0
    q.reopen()
    q.add("a")
    assert drain(q) == ["a"]
