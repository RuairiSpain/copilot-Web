"""Circuit breaker state machine, registry and the adapter that applies it."""

from __future__ import annotations

import asyncio
import logging
from collections.abc import AsyncIterator

import pytest

from hosted_agent_kit.adapters.circuit_breaking import CircuitBreakingAdapter
from hosted_agent_kit.config.models import CircuitBreakerConfig
from hosted_agent_kit.domain.enums import CircuitState as C
from hosted_agent_kit.domain.errors import (
    FoundryCircuitOpen,
    FoundryRejected,
    FoundrySessionFailed,
    FoundrySessionNotFound,
    FoundryThrottled,
    FoundryTimeout,
    FoundryUnavailable,
)
from hosted_agent_kit.domain.models import InvokeContext
from hosted_agent_kit.services.circuit_breaker import (
    DISABLED,
    CircuitBreaker,
    CircuitBreakers,
    retry_after_seconds,
)
from hosted_agent_kit.services.metrics import InMemoryMetrics
from tests.conftest import make_config, settle
from tests.fakes.clock import FakeClock
from tests.fakes.foundry import FakeFoundry


def breaker(
    threshold: int = 3, open_seconds: float = 10.0, probes: int = 1
) -> tuple[CircuitBreaker, FakeClock, InMemoryMetrics]:
    clock, metrics = FakeClock(), InMemoryMetrics()
    config = CircuitBreakerConfig(
        failure_threshold=threshold, open_seconds=open_seconds, half_open_max_calls=probes
    )
    return CircuitBreaker("a", config, clock, metrics), clock, metrics


def state_of(b: CircuitBreaker) -> C:
    return b.state


def trip(b: CircuitBreaker) -> None:
    for _ in range(3):
        b.failure(b.before_call())


def gauge(metrics: InMemoryMetrics) -> float:
    return next(
        float(g["value"])
        for g in metrics.snapshot()["agents"]["a"]["gauges"]
        if g["name"] == "pool_circuit_state"
    )


def test_starts_closed_and_reports_the_gauge() -> None:
    b, _, metrics = breaker()
    assert state_of(b) is C.CLOSED and gauge(metrics) == 0
    assert b.before_call() is False


def test_failures_below_the_threshold_keep_it_closed_and_a_success_resets_the_count() -> None:
    b, _, _ = breaker(threshold=3)
    b.failure(False)
    b.failure(False)
    b.success(False)  # the streak is broken
    b.failure(False)
    b.failure(False)
    assert state_of(b) is C.CLOSED
    b.failure(False)
    assert state_of(b) is C.OPEN


def test_consecutive_failures_open_the_circuit_and_calls_fail_fast_with_time_left() -> None:
    b, clock, metrics = breaker(threshold=3, open_seconds=10)
    trip(b)
    assert state_of(b) is C.OPEN and gauge(metrics) == 2
    with pytest.raises(FoundryCircuitOpen) as first:
        b.before_call()
    assert first.value.retry_after_seconds == pytest.approx(10)
    clock.advance(4)
    with pytest.raises(FoundryCircuitOpen) as later:
        b.before_call()
    assert later.value.retry_after_seconds == pytest.approx(6)


def test_after_the_cool_down_one_probe_is_admitted_and_others_are_refused() -> None:
    b, clock, metrics = breaker(open_seconds=10, probes=1)
    trip(b)
    clock.advance(10)
    assert b.before_call() is True and state_of(b) is C.HALF_OPEN and gauge(metrics) == 1
    with pytest.raises(FoundryCircuitOpen) as refused:
        b.before_call()
    assert refused.value.retry_after_seconds == 1.0


def test_several_probes_may_be_allowed() -> None:
    b, clock, _ = breaker(open_seconds=5, probes=2)
    trip(b)
    clock.advance(5)
    assert b.before_call() is True and b.before_call() is True
    with pytest.raises(FoundryCircuitOpen):
        b.before_call()


def test_a_successful_probe_closes_the_circuit_and_resets_the_count() -> None:
    b, clock, metrics = breaker(threshold=3, open_seconds=5)
    trip(b)
    clock.advance(5)
    b.success(b.before_call())
    assert state_of(b) is C.CLOSED and gauge(metrics) == 0
    b.failure(False)
    b.failure(False)
    assert state_of(b) is C.CLOSED  # the old count did not carry over


def test_a_failed_probe_reopens_the_circuit_with_a_fresh_timer() -> None:
    b, clock, _ = breaker(open_seconds=10)
    trip(b)
    clock.advance(10)
    b.failure(b.before_call())
    assert state_of(b) is C.OPEN
    clock.advance(9)
    with pytest.raises(FoundryCircuitOpen) as info:
        b.before_call()
    assert info.value.retry_after_seconds == pytest.approx(1)
    clock.advance(1)
    assert b.before_call() is True  # half open again


def test_an_abandoned_probe_gives_its_place_back() -> None:
    b, clock, _ = breaker(open_seconds=5, probes=1)
    trip(b)
    clock.advance(5)
    probe = b.before_call()
    b.abandoned(probe)
    assert state_of(b) is C.HALF_OPEN and b.before_call() is True
    b.abandoned(False)  # a normal call abandoning changes nothing


def test_abandoning_one_of_several_probes_frees_exactly_one_place() -> None:
    b, clock, _ = breaker(open_seconds=5, probes=2)
    trip(b)
    clock.advance(5)
    first, second = b.before_call(), b.before_call()
    b.abandoned(first)
    assert b.before_call() is True  # the freed place
    with pytest.raises(FoundryCircuitOpen):  # but only one
        b.before_call()
    assert second is True


def test_results_of_calls_started_before_the_circuit_opened_do_not_change_it() -> None:
    b, _, _ = breaker(open_seconds=10)
    in_flight = [b.before_call() for _ in range(2)]
    trip(b)
    b.failure(in_flight[0])  # not a probe and the circuit is already open
    b.success(in_flight[1])
    assert state_of(b) is C.OPEN


def test_late_probe_results_do_not_disturb_a_closed_circuit() -> None:
    b, clock, _ = breaker(open_seconds=5, probes=2)
    trip(b)
    clock.advance(5)
    first, second = b.before_call(), b.before_call()
    b.success(first)  # closes
    b.failure(second)  # a straggler from the half-open period
    b.success(second)
    assert state_of(b) is C.CLOSED


def test_transitions_are_counted_and_logged(caplog: pytest.LogCaptureFixture) -> None:
    caplog.set_level(logging.INFO, logger="hosted_agent_kit")
    b, clock, metrics = breaker(open_seconds=5)
    trip(b)
    clock.advance(5)
    b.success(b.before_call())
    transitions = {
        c["labels"]["to"]: c["value"]
        for c in metrics.snapshot()["agents"]["a"]["counters"]
        if c["name"] == "pool_circuit_transitions_total"
    }
    assert transitions == {"open": 1, "half_open": 1, "closed": 1}
    events = {
        r.getMessage(): r.levelno for r in caplog.records if r.getMessage().startswith("circuit_")
    }
    assert events == {
        "circuit_open": logging.WARNING,
        "circuit_half_open": logging.INFO,
        "circuit_closed": logging.INFO,
    }
    assert 'pool_circuit_state{agent="a"} 0' in metrics.prometheus_text()


@pytest.mark.parametrize(("seconds", "expected"), [(0.2, 1), (1.0, 1), (2.1, 3), (30, 30)])
def test_retry_after_is_whole_seconds_and_at_least_one(seconds: float, expected: int) -> None:
    assert retry_after_seconds(FoundryCircuitOpen(seconds)) == expected


def test_registry_creates_breakers_only_for_enabled_agents() -> None:
    config = make_config(
        {
            "on": {"mode": "stateless"},
            "off": {"mode": "stateless", "circuit_breaker": {"enabled": False}},
        }
    )
    registry = CircuitBreakers(config, FakeClock(), InMemoryMetrics())
    assert registry.get("on") is not None and registry.get("off") is None
    assert registry.state("on") == "closed" and registry.state("off") == DISABLED
    assert registry.state("unknown") == DISABLED


# ---------------------------------------------------------------- the adapter


def guarded(
    threshold: int = 2, open_seconds: float = 10.0
) -> tuple[CircuitBreakingAdapter, FakeFoundry, CircuitBreakers, FakeClock]:
    clock = FakeClock()
    fake = FakeFoundry(clock)
    config = make_config(
        {
            "a": {
                "mode": "stateless",
                "circuit_breaker": {"failure_threshold": threshold, "open_seconds": open_seconds},
            },
            "off": {"mode": "stateless", "circuit_breaker": {"enabled": False}},
        }
    )
    breakers = CircuitBreakers(config, clock, InMemoryMetrics())
    return CircuitBreakingAdapter(fake, breakers), fake, breakers, clock


def context(agent: str = "a", stream: bool = False) -> InvokeContext:
    return InvokeContext(
        agent_name=agent,
        session_id="s1",
        payload={"input": "x"},
        timeout_seconds=5,
        request_id="r",
        correlation_id="c",
        stream=stream,
    )


async def test_unavailable_and_timeout_errors_open_the_circuit_without_reaching_foundry() -> None:
    adapter, fake, breakers, _ = guarded(threshold=2)
    fake.create_errors = [FoundryUnavailable("x"), FoundryTimeout()]
    for _ in range(2):
        with pytest.raises((FoundryUnavailable, FoundryTimeout)):
            await adapter.create_session("a")
    assert breakers.state("a") == "open"
    with pytest.raises(FoundryCircuitOpen):
        await adapter.create_session("a")
    with pytest.raises(FoundryCircuitOpen):
        await adapter.get_session("a", "s")
    assert len(fake.create_requests) == 2 and fake.get_calls == []


@pytest.mark.parametrize(
    "error",
    [FoundryRejected(403), FoundrySessionNotFound(), FoundryThrottled(1), FoundrySessionFailed()],
)
async def test_other_errors_prove_foundry_is_responding_and_never_trip_it(error: Exception) -> None:
    adapter, fake, breakers, _ = guarded(threshold=2)
    for _ in range(6):
        fake.invoke_errors = [error]
        with pytest.raises(type(error)):
            await adapter.invoke(context())
    assert breakers.state("a") == "closed"


async def test_a_success_between_failures_keeps_the_circuit_closed() -> None:
    adapter, fake, breakers, _ = guarded(threshold=2)
    for _ in range(4):
        fake.invoke_errors = [FoundryUnavailable("x")]
        with pytest.raises(FoundryUnavailable):
            await adapter.invoke(context())
        await adapter.invoke(context())
    assert breakers.state("a") == "closed"


async def test_recovery_through_a_successful_probe() -> None:
    adapter, fake, breakers, clock = guarded(threshold=1, open_seconds=10)
    fake.invoke_errors = [FoundryUnavailable("x")]
    with pytest.raises(FoundryUnavailable):
        await adapter.invoke(context())
    assert breakers.state("a") == "open"
    clock.advance(10)
    await adapter.invoke(context())
    assert breakers.state("a") == "closed"


async def test_a_cancelled_probe_does_not_leave_the_circuit_stuck_half_open() -> None:
    adapter, fake, breakers, clock = guarded(threshold=1, open_seconds=10)
    fake.invoke_errors = [FoundryUnavailable("x")]
    with pytest.raises(FoundryUnavailable):
        await adapter.invoke(context())
    clock.advance(10)
    fake.invoke_gate = asyncio.Event()
    probe = asyncio.create_task(adapter.invoke(context()))
    await settle()
    with pytest.raises(FoundryCircuitOpen):  # the one probe is in flight
        await adapter.invoke(context())
    probe.cancel()
    with pytest.raises(asyncio.CancelledError):
        await probe
    fake.invoke_gate = None
    await adapter.invoke(context())  # the place was given back
    assert breakers.state("a") == "closed"


async def test_a_programming_error_does_not_count_as_a_failure() -> None:
    adapter, fake, breakers, _ = guarded(threshold=1)
    fake.invoke_errors = [RuntimeError("bug")]
    with pytest.raises(RuntimeError):
        await adapter.invoke(context())
    assert breakers.state("a") == "closed"


async def test_a_stream_that_fails_midway_counts_and_is_closed() -> None:
    adapter, fake, breakers, _ = guarded(threshold=1)
    fake.stream_error = FoundryUnavailable("cut")
    response = await adapter.invoke(context(stream=True))
    assert breakers.state("a") == "closed"  # the call itself succeeded
    assert response.stream is not None
    with pytest.raises(FoundryUnavailable):
        _ = [frame async for frame in response.stream]
    assert breakers.state("a") == "open"


async def test_a_stream_that_completes_or_is_closed_early_is_harmless() -> None:
    adapter, fake, breakers, _ = guarded(threshold=1)
    first = await adapter.invoke(context(stream=True))
    assert first.stream is not None
    assert [f async for f in first.stream] == fake.stream_frames
    second = await adapter.invoke(context(stream=True))
    assert second.stream is not None
    iterator = second.stream.__aiter__()
    await iterator.__anext__()
    await iterator.aclose()  # type: ignore[attr-defined]
    assert breakers.state("a") == "closed"


async def test_a_stream_without_aclose_is_supported() -> None:
    adapter, fake, _, _ = guarded(threshold=1)

    class Plain:
        def __init__(self) -> None:
            self.items = [b"x"]

        def __aiter__(self) -> AsyncIterator[bytes]:
            return self

        async def __anext__(self) -> bytes:
            if not self.items:
                raise StopAsyncIteration
            return self.items.pop()

    from hosted_agent_kit.ports.foundry import UpstreamResponse

    fake.invoke_handler = lambda ctx: UpstreamResponse(
        stream=Plain(), media_type="text/event-stream"
    )
    response = await adapter.invoke(context(stream=True))
    assert response.stream is not None
    assert [f async for f in response.stream] == [b"x"]


async def test_list_sessions_counts_failures_successes_and_abandoned_iterations() -> None:
    adapter, fake, breakers, clock = guarded(threshold=1, open_seconds=10)
    fake.add_session("a")
    assert [s async for s in adapter.list_sessions("a")]  # success path
    fake.list_fail_after = 0
    fake.list_error = FoundryUnavailable("page failed")
    with pytest.raises(FoundryUnavailable):
        _ = [s async for s in adapter.list_sessions("a")]
    assert breakers.state("a") == "open"
    with pytest.raises(FoundryCircuitOpen):
        _ = [s async for s in adapter.list_sessions("a")]
    clock.advance(10)
    fake.list_fail_after = None
    stream = adapter.list_sessions("a")
    await stream.__anext__()  # becomes the half-open probe
    await stream.aclose()  # type: ignore[attr-defined]  # abandoned: the permit is returned
    assert [s async for s in adapter.list_sessions("a")]
    assert breakers.state("a") == "closed"


async def test_a_listing_rejected_by_foundry_is_not_a_failure() -> None:
    adapter, fake, breakers, _ = guarded(threshold=1)
    fake.list_fail_after = 0
    fake.list_error = FoundryRejected(403)
    fake.add_session("a")
    with pytest.raises(FoundryRejected):
        _ = [s async for s in adapter.list_sessions("a")]
    assert breakers.state("a") == "closed"


async def test_disabled_agents_pass_straight_through_and_agent_listing_is_never_guarded() -> None:
    adapter, fake, breakers, _ = guarded(threshold=1)
    for _ in range(3):
        fake.invoke_errors = [FoundryUnavailable("x")]
        with pytest.raises(FoundryUnavailable):
            await adapter.invoke(context("off"))
    assert breakers.state("off") == DISABLED
    fake.invoke_errors = [FoundryUnavailable("x")]
    with pytest.raises(FoundryUnavailable):
        await adapter.invoke(context("a"))
    assert breakers.state("a") == "open"
    assert [x async for x in adapter.list_agents()] == []
    fake.add_session("off")
    assert [s async for s in adapter.list_sessions("off")]
    await adapter.stop_session("off", "x")
    await adapter.delete_session("off", "x")


async def test_stop_delete_start_and_close_are_forwarded() -> None:
    adapter, fake, _, _ = guarded()
    await adapter.start()
    await adapter.stop_session("a", "s1")
    await adapter.delete_session("a", "s1")
    await adapter.close()
    assert fake.started and fake.closed and fake.stopped == ["s1"] and fake.deleted == ["s1"]


async def test_listing_errors_from_a_disabled_agent_propagate_untouched() -> None:
    adapter, fake, breakers, _ = guarded()
    fake.add_session("off")
    fake.list_fail_after = 0
    fake.list_error = FoundryUnavailable("page failed")
    with pytest.raises(FoundryUnavailable):
        _ = [s async for s in adapter.list_sessions("off")]
    fake.list_error = FoundryRejected(403)
    with pytest.raises(FoundryRejected):
        _ = [s async for s in adapter.list_sessions("off")]
    assert breakers.state("off") == DISABLED


async def _half_open_adapter() -> tuple[CircuitBreakingAdapter, FakeFoundry, CircuitBreakers]:
    adapter, fake, breakers, clock = guarded(threshold=3, open_seconds=10)
    fake.create_errors = [FoundryUnavailable("x")] * 3
    for _ in range(3):
        with pytest.raises(FoundryUnavailable):
            await adapter.create_session("a")
    assert breakers.state("a") == "open"
    clock.advance(11)
    return adapter, fake, breakers


async def test_a_streaming_probe_does_not_close_the_circuit_until_the_stream_ends() -> None:
    adapter, fake, breakers = await _half_open_adapter()
    response = await adapter.invoke(context(stream=True))
    assert response.stream is not None
    assert breakers.state("a") == "half_open"  # opening the stream proves nothing yet
    assert [frame async for frame in response.stream] == fake.stream_frames
    assert breakers.state("a") == "closed"


async def test_a_streaming_probe_that_fails_reopens_the_circuit_at_once() -> None:
    adapter, fake, breakers = await _half_open_adapter()
    fake.stream_error = FoundryUnavailable("reset")
    response = await adapter.invoke(context(stream=True))
    assert response.stream is not None
    with pytest.raises(FoundryUnavailable):
        _ = [frame async for frame in response.stream]
    assert breakers.state("a") == "open"
    with pytest.raises(FoundryCircuitOpen):
        await adapter.invoke(context(stream=True))


async def test_a_streaming_probe_closed_early_frees_the_probe_slot() -> None:
    adapter, _, breakers = await _half_open_adapter()
    response = await adapter.invoke(context(stream=True))
    assert response.stream is not None
    await response.stream.aclose()  # type: ignore[attr-defined]  # for example the client went away before reading
    assert breakers.state("a") == "half_open"
    again = await adapter.invoke(context(stream=True))  # a new probe is admitted
    assert again.stream is not None
    assert [frame async for frame in again.stream]
    assert breakers.state("a") == "closed"


async def test_a_stream_that_fails_in_the_closed_state_still_counts_a_failure() -> None:
    adapter, fake, breakers, _ = guarded(threshold=1)
    fake.stream_error = FoundryTimeout()
    response = await adapter.invoke(context(stream=True))
    assert response.stream is not None
    with pytest.raises(FoundryTimeout):
        _ = [frame async for frame in response.stream]
    assert breakers.state("a") == "open"
