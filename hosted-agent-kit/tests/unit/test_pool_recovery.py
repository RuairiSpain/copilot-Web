"""Failure handling: replacement, retries, creation, deletion and streaming."""

from __future__ import annotations

import asyncio

import pytest

from hosted_agent_kit.domain.enums import FoundrySessionStatus, LocalSessionState
from hosted_agent_kit.domain.errors import (
    FoundryRejected,
    FoundrySessionFailed,
    FoundrySessionNotFound,
    FoundryThrottled,
    FoundryTimeout,
    FoundryTimeoutAppError,
    FoundryUnavailable,
    FoundryUnavailableError,
    SessionFailedError,
    SessionNotFoundUpstreamError,
    UpstreamError,
    UpstreamThrottledError,
    ValidationFailedError,
)
from hosted_agent_kit.domain.models import SessionAffinityKey
from tests.conftest import make_harness, make_request, settle

STATEFUL = {"coding-agent": {"mode": "stateful", "max_sessions": 3}}


async def test_externally_deleted_session_is_replaced_once() -> None:
    """A9."""
    h = make_harness(STATEFUL, defaults={})
    await h.pool.execute(make_request("coding-agent", "u1"))
    old = h.fake.created[0]
    h.fake.invoke_errors = [FoundrySessionNotFound()]
    result = await h.pool.execute(make_request("coding-agent", "u1"))
    assert result.status_code == 200
    assert len(h.fake.created) == 2
    new = h.fake.created[1]
    assert h.fake.invocations[-1].session_id == new != old
    entry = await h.affinity.get(SessionAffinityKey(user_id="u1", agent_name="coding-agent"))
    assert entry is not None and entry.session_id == new
    assert await h.registry.get("coding-agent", old) is None


async def test_second_not_found_is_surfaced() -> None:
    h = make_harness(STATEFUL, defaults={})
    h.fake.invoke_errors = [FoundrySessionNotFound(), FoundrySessionNotFound()]
    with pytest.raises(SessionNotFoundUpstreamError):
        await h.pool.execute(make_request("coding-agent", "u1"))
    assert len(h.fake.created) == 2  # exactly one replacement


async def test_not_found_replacement_keeps_capacity_for_the_request() -> None:
    h = make_harness({"a": {"mode": "stateless", "max_sessions": 1}}, defaults={})
    await h.pool.execute(make_request("a"))
    h.fake.invoke_errors = [FoundrySessionNotFound()]
    await h.pool.execute(make_request("a"))
    assert await h.registry.count("a") == 1
    assert await h.registry.reserved("a") == 0


async def test_failed_session_without_idempotency_key_is_deleted_not_retried() -> None:
    h = make_harness(STATEFUL, defaults={})
    h.fake.invoke_errors = [FoundrySessionFailed()]
    with pytest.raises(SessionFailedError):
        await h.pool.execute(make_request("coding-agent", "u1"))
    assert h.fake.deleted == h.fake.created[:1]
    assert await h.registry.count("coding-agent") == 0
    assert await h.affinity.count("coding-agent") == 0
    counters = {
        c["name"]: c["value"] for c in h.metrics.snapshot()["agents"]["coding-agent"]["counters"]
    }
    assert counters["pool_failed_sessions_total"] == 1


async def test_failed_session_with_idempotency_key_retries_once() -> None:
    h = make_harness(STATEFUL, defaults={})
    h.fake.invoke_errors = [FoundrySessionFailed()]
    result = await h.pool.execute(make_request("coding-agent", "u1", idempotency_key="k1"))
    assert result.status_code == 200
    assert len(h.fake.created) == 2 and len(h.fake.deleted) == 1


async def test_failed_session_with_idempotency_key_second_failure_surfaces() -> None:
    h = make_harness(STATEFUL, defaults={})
    h.fake.invoke_errors = [FoundrySessionFailed(), FoundrySessionFailed()]
    with pytest.raises(SessionFailedError):
        await h.pool.execute(make_request("coding-agent", "u1", idempotency_key="k1"))
    assert len(h.fake.created) == 2


async def test_failed_delete_is_retried_for_transient_errors_only() -> None:
    h = make_harness(STATEFUL, defaults={})
    h.fake.invoke_errors = [FoundrySessionFailed()]
    h.fake.delete_errors = [FoundryUnavailable("x"), FoundryThrottled(0.02)]
    with pytest.raises(SessionFailedError):
        await h.pool.execute(make_request("coding-agent", "u1"))
    assert len(h.fake.deleted) == 1  # third attempt succeeded
    assert h.clock.sleeps  # backed off between attempts


async def test_delete_not_found_counts_as_converged() -> None:
    h = make_harness(STATEFUL, defaults={})
    h.fake.invoke_errors = [FoundrySessionFailed()]
    h.fake.delete_errors = [FoundrySessionNotFound()]
    with pytest.raises(SessionFailedError):
        await h.pool.execute(make_request("coding-agent", "u1"))
    deletes = [
        c
        for c in h.metrics.snapshot()["agents"]["coding-agent"]["counters"]
        if c["name"] == "pool_session_delete_total"
    ]
    assert deletes[0]["labels"]["outcome"] == "success"


async def test_persistent_delete_failure_is_recorded_and_not_retried_forever() -> None:
    h = make_harness(STATEFUL, defaults={}, delete_retries=1)
    h.fake.invoke_errors = [FoundrySessionFailed()]
    h.fake.delete_errors = [FoundryUnavailable("x"), FoundryUnavailable("x")]
    with pytest.raises(SessionFailedError):
        await h.pool.execute(make_request("coding-agent", "u1"))
    deletes = [
        c
        for c in h.metrics.snapshot()["agents"]["coding-agent"]["counters"]
        if c["name"] == "pool_session_delete_total"
    ]
    assert deletes[0]["labels"]["outcome"] == "failure"


async def test_non_transient_delete_error_is_not_retried() -> None:
    h = make_harness(STATEFUL, defaults={})
    h.fake.invoke_errors = [FoundrySessionFailed()]
    h.fake.delete_errors = [FoundryRejected(403), FoundryUnavailable("unused")]
    with pytest.raises(SessionFailedError):
        await h.pool.execute(make_request("coding-agent", "u1"))
    assert h.fake.delete_errors  # second scripted error was never consumed


async def test_throttle_is_retried_with_retry_after_then_succeeds() -> None:
    h = make_harness()
    h.fake.invoke_errors = [FoundryThrottled(0.02)]
    result = await h.pool.execute(make_request())
    assert result.status_code == 200
    assert len(h.fake.invocations) == 2
    assert h.fake.invocations[0].session_id == h.fake.invocations[1].session_id  # lease kept
    assert h.clock.sleeps and h.clock.sleeps[0] >= 0.02


async def test_throttle_exhaustion_maps_to_upstream_throttled_with_retry_after() -> None:
    h = make_harness(upstream_throttle_retries=1)
    h.fake.invoke_errors = [FoundryThrottled(0.03), FoundryThrottled(0.03)]
    with pytest.raises(UpstreamThrottledError) as info:
        await h.pool.execute(make_request())
    assert info.value.retry_after_seconds == 1
    assert len(h.fake.invocations) == 2


async def test_retry_after_longer_than_the_backoff_cap_is_returned_not_cut_short() -> None:
    h = make_harness(upstream_throttle_retries=3)  # backoff cap is 0.05 seconds
    h.fake.invoke_errors = [FoundryThrottled(3.2)]
    with pytest.raises(UpstreamThrottledError) as info:
        await h.pool.execute(make_request())
    assert info.value.retry_after_seconds == 4  # the upstream value, not the cap
    assert len(h.fake.invocations) == 1 and not h.clock.sleeps  # no early retry


async def test_throttle_without_hint_uses_default_retry_after() -> None:
    h = make_harness(upstream_throttle_retries=0)
    h.fake.invoke_errors = [FoundryThrottled(None)]
    with pytest.raises(UpstreamThrottledError) as info:
        await h.pool.execute(make_request())
    assert info.value.retry_after_seconds == 7


@pytest.mark.parametrize(
    ("error", "expected"),
    [
        (FoundryTimeout(), FoundryTimeoutAppError),
        (FoundryUnavailable("x"), FoundryUnavailableError),
        (FoundryRejected(400), ValidationFailedError),
        (FoundryRejected(422), ValidationFailedError),
        (FoundryRejected(403), UpstreamError),
    ],
)
async def test_invoke_errors_map_to_app_errors(error: Exception, expected: type[Exception]) -> None:
    h = make_harness()
    h.fake.invoke_errors = [error]
    with pytest.raises(expected):
        await h.pool.execute(make_request())
    (record,) = await h.registry.list("stateless-agent")
    assert record.local_state is LocalSessionState.AVAILABLE  # lease released


async def test_creation_retries_transient_errors_then_succeeds() -> None:
    h = make_harness()
    h.fake.create_errors = [FoundryUnavailable("x"), FoundryThrottled(None)]
    result = await h.pool.execute(make_request())
    assert result.status_code == 200
    assert len(h.clock.sleeps) == 2


async def test_creation_exhaustion_maps_error_and_returns_capacity() -> None:
    h = make_harness(
        {"a": {"mode": "stateless", "max_sessions": 1, "create_retries": 1}}, defaults={}
    )
    h.fake.create_errors = [FoundryUnavailable("x"), FoundryUnavailable("x")]
    with pytest.raises(FoundryUnavailableError):
        await h.pool.execute(make_request("a"))
    assert await h.registry.reserved("a") == 0
    assert await h.registry.count("a") == 0
    await h.pool.execute(make_request("a"))  # capacity is usable again


async def test_creation_failure_clears_pending_affinity() -> None:
    h = make_harness({"a": {"mode": "stateful", "create_retries": 0}}, defaults={})
    h.fake.create_errors = [FoundryTimeout()]
    with pytest.raises(FoundryTimeoutAppError):
        await h.pool.execute(make_request("a", "u1"))
    assert await h.affinity.get(SessionAffinityKey(user_id="u1", agent_name="a")) is None


async def test_non_retryable_creation_errors_surface_immediately() -> None:
    h = make_harness()
    h.fake.create_errors = [FoundryRejected(403)]
    with pytest.raises(UpstreamError):
        await h.pool.execute(make_request())
    h.fake.create_errors = [FoundrySessionNotFound()]
    with pytest.raises(UpstreamError):
        await h.pool.execute(make_request())


async def test_creation_waits_for_creating_status_to_become_active() -> None:
    h = make_harness()
    h.fake.create_status = FoundrySessionStatus.CREATING
    h.fake.create_progress = [FoundrySessionStatus.CREATING, FoundrySessionStatus.ACTIVE]
    result = await h.pool.execute(make_request())
    assert result.status_code == 200
    assert len(h.fake.get_calls) == 2


async def test_creation_times_out_if_never_active() -> None:
    h = make_harness(
        {"a": {"mode": "stateless", "create_retries": 0}},
        defaults={},
        create_ready_timeout_seconds=0.05,
        backoff_base_seconds=0.02,
    )
    h.fake.create_status = FoundrySessionStatus.CREATING
    with pytest.raises(FoundryTimeoutAppError):
        await h.pool.execute(make_request("a"))


async def test_session_that_fails_during_creation_is_deleted_and_retried() -> None:
    h = make_harness()
    h.fake.create_status = FoundrySessionStatus.FAILED
    h.fake.create_errors = []
    original = h.fake.create_session
    calls = 0

    async def flaky(agent: str, session_id: str | None = None, version: str | None = None):  # type: ignore[no-untyped-def]
        nonlocal calls
        calls += 1
        if calls == 2:
            h.fake.create_status = FoundrySessionStatus.ACTIVE
        return await original(agent)

    h.fake.create_session = flaky  # type: ignore[method-assign]
    result = await h.pool.execute(make_request())
    assert result.status_code == 200
    assert len(h.fake.deleted) == 1


async def test_session_failed_during_creation_exhausts_to_session_failed() -> None:
    h = make_harness({"a": {"mode": "stateless", "create_retries": 0}}, defaults={})
    h.fake.create_status = FoundrySessionStatus.FAILED
    with pytest.raises(SessionFailedError):
        await h.pool.execute(make_request("a"))


async def test_cancel_during_creation_returns_slot() -> None:
    h = make_harness({"a": {"mode": "stateful", "max_sessions": 1}}, defaults={})
    gate = asyncio.Event()
    original = h.fake.create_session

    async def slow(agent: str, session_id: str | None = None, version: str | None = None):  # type: ignore[no-untyped-def]
        await gate.wait()
        return await original(agent)

    h.fake.create_session = slow  # type: ignore[method-assign]
    task = asyncio.create_task(h.pool.execute(make_request("a", "u1")))
    await settle()
    assert await h.registry.reserved("a") == 1
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task
    assert await h.registry.reserved("a") == 0
    assert await h.affinity.get(SessionAffinityKey(user_id="u1", agent_name="a")) is None


# ---------------------------------------------------------------- streaming


async def test_stream_holds_lease_until_consumed_and_scrubs_nothing_extra() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    result = await h.pool.execute(make_request(stream=True, payload={"input": "x", "stream": True}))
    assert result.stream is not None and result.media_type == "text/event-stream"
    (record,) = await h.registry.list("stateless-agent")
    assert record.local_state is LocalSessionState.LEASED
    frames = [frame async for frame in result.stream]
    assert frames == h.fake.stream_frames
    (record,) = await h.registry.list("stateless-agent")
    assert record.local_state is LocalSessionState.AVAILABLE


async def test_stream_close_without_iterating_releases_lease() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    result = await h.pool.execute(make_request(stream=True))
    await result.close()
    await result.close()  # idempotent
    (record,) = await h.registry.list("stateless-agent")
    assert record.local_state is LocalSessionState.AVAILABLE
    outcomes = [
        c["labels"]["outcome"]
        for c in h.metrics.snapshot()["agents"]["stateless-agent"]["counters"]
        if c["name"] == "pool_requests_total"
    ]
    assert outcomes == ["CLIENT_CANCELLED"]


async def test_stream_error_midway_emits_error_frame_and_releases() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    h.fake.stream_error = FoundryUnavailable("boom")
    result = await h.pool.execute(make_request(stream=True))
    assert result.stream is not None
    frames = [frame async for frame in result.stream]
    assert frames[-1].startswith(b"event: error")
    assert b"FOUNDRY_UNAVAILABLE" in frames[-1]
    (record,) = await h.registry.list("stateless-agent")
    assert record.local_state is LocalSessionState.AVAILABLE


async def test_stream_session_failure_deletes_the_session() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    h.fake.stream_error = FoundrySessionFailed()
    result = await h.pool.execute(make_request(stream=True))
    assert result.stream is not None
    frames = [frame async for frame in result.stream]
    assert b"SESSION_FAILED" in frames[-1]
    assert len(h.fake.deleted) == 1
    assert await h.registry.count("stateless-agent") == 0


async def test_stream_time_limit_ends_stream_with_error_frame() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1}, max_stream_seconds=0.05)
    h.fake.stream_stall = True
    result = await h.pool.execute(make_request(stream=True))
    assert result.stream is not None
    frames = [frame async for frame in result.stream]
    assert frames[-1].startswith(b"event: error") and b"STREAM_TIMEOUT" in frames[-1]
    (record,) = await h.registry.list("stateless-agent")
    assert record.local_state is LocalSessionState.AVAILABLE


async def test_stream_deadline_already_passed_stops_before_next_frame() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1}, max_stream_seconds=1)
    result = await h.pool.execute(make_request(stream=True))
    assert result.stream is not None
    iterator = result.stream.__aiter__()
    first = await iterator.__anext__()
    h.clock.advance(5)
    rest = [frame async for frame in iterator]
    assert first == h.fake.stream_frames[0]
    assert rest[-1].startswith(b"event: error")
