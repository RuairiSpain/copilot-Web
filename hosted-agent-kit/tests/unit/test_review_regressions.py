"""Regression tests for the lifecycle, streaming and recovery defects found in review."""

from __future__ import annotations

import asyncio
import json
from collections.abc import AsyncIterator
from typing import Any

import httpx
import pytest

from hosted_agent_kit.domain.enums import FoundrySessionStatus as S
from hosted_agent_kit.domain.enums import LocalSessionState as L
from hosted_agent_kit.domain.errors import (
    FoundryRejected,
    FoundryResponseTooLarge,
    FoundrySessionFailed,
    FoundryThrottled,
    FoundryTimeoutAppError,
    FoundryUnavailable,
    FoundryUnavailableError,
    RequestTimeoutError,
    ResponseTooLargeError,
    SessionFailedError,
    UpstreamThrottledError,
)
from hosted_agent_kit.ports.foundry import UpstreamResponse
from hosted_agent_kit.service.api.client import _BoundedStreamingResponse
from hosted_agent_kit.service.main import create_app
from hosted_agent_kit.service.security.auth import JwksCache
from tests.conftest import make_config, make_harness, make_request, make_settings, settle
from tests.fakes.entra import JwksServer
from tests.fakes.foundry import FakeFoundry

AGENT = "stateless-agent"
NO_RETRIES = {"mode": "stateless", "max_sessions": 2, "create_retries": 0}


# --------------------------------------------------------------- creation cleanup


async def test_readiness_timeout_deletes_the_session_it_created() -> None:
    fake = FakeFoundry()
    fake.create_status = S.CREATING  # never becomes ready
    h = make_harness(fake=fake, defaults=NO_RETRIES, create_ready_timeout_seconds=1)
    with pytest.raises(FoundryTimeoutAppError):
        await h.pool.execute(make_request())
    assert len(fake.created) == 1 and fake.deleted == fake.created and not fake.sessions
    assert await h.registry.count(AGENT) == 0 and await h.registry.reserved(AGENT) == 0


async def test_every_retry_after_a_readiness_timeout_cleans_up_its_own_session() -> None:
    fake = FakeFoundry()
    fake.create_status = S.CREATING
    h = make_harness(
        fake=fake,
        defaults={"mode": "stateless", "max_sessions": 2, "create_retries": 2},
        create_ready_timeout_seconds=1,
    )
    with pytest.raises(FoundryTimeoutAppError):
        await h.pool.execute(make_request())
    assert len(fake.created) == 3 and sorted(fake.deleted) == sorted(fake.created)
    assert not fake.sessions


async def test_cancellation_while_waiting_for_readiness_deletes_the_session() -> None:
    fake = FakeFoundry()
    fake.create_status = S.CREATING
    h = make_harness(fake=fake, defaults=NO_RETRIES)
    stuck = asyncio.Event()

    async def never(seconds: float) -> None:
        await stuck.wait()

    h.clock.sleep = never  # type: ignore[method-assign]
    task = asyncio.create_task(h.pool.execute(make_request()))
    await settle(10)
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task
    assert len(fake.created) == 1 and fake.deleted == fake.created
    assert await h.registry.reserved(AGENT) == 0


async def test_an_unready_session_that_cannot_be_deleted_is_tracked_then_cleaned_by_sync() -> None:
    fake = FakeFoundry()
    fake.create_status = S.CREATING
    fake.delete_errors = [FoundryRejected(403)]
    h = make_harness(fake=fake, defaults=NO_RETRIES, create_ready_timeout_seconds=1)
    with pytest.raises(FoundryTimeoutAppError):
        await h.pool.execute(make_request())
    sid = fake.created[0]
    record = await h.registry.get(AGENT, sid)
    assert record is not None and record.local_state is L.RETIRING and sid in fake.sessions
    await h.reconciler.run_agent(AGENT)
    assert await h.registry.get(AGENT, sid) is None and fake.deleted == [sid]


async def test_a_restored_session_is_not_deleted_when_it_is_slow_to_become_ready() -> None:
    from hosted_agent_kit.services.session_ids import SessionIdDeriver

    deriver = SessionIdDeriver(b"k" * 32)
    fake = FakeFoundry()
    sid = deriver.for_user("coding-agent", "u1")
    fake.add_session("coding-agent", S.CREATING, sid)
    h = make_harness(
        {"coding-agent": {"mode": "stateful", "max_sessions": 3, "create_retries": 0}},
        defaults={},
        fake=fake,
        session_ids=deriver,
        create_ready_timeout_seconds=1,
    )
    with pytest.raises(FoundryTimeoutAppError):
        await h.pool.execute(make_request("coding-agent", "u1"))
    assert fake.deleted == [] and sid in fake.sessions  # it belongs to the user


# ------------------------------------------------------------- failed-session delete


async def test_failed_remote_delete_keeps_a_retiring_record_for_the_next_sync() -> None:
    h = make_harness(delete_retries=0)
    h.fake.invoke_errors = [FoundrySessionFailed()]
    h.fake.delete_errors = [FoundryRejected(403)]
    with pytest.raises(SessionFailedError):
        await h.pool.execute(make_request())
    sid = h.fake.created[0]
    record = await h.registry.get(AGENT, sid)
    assert record is not None and record.local_state is L.RETIRING
    assert record.lease_request_id is None and sid in h.fake.sessions
    await h.reconciler.run_agent(AGENT)
    assert await h.registry.get(AGENT, sid) is None and h.fake.deleted == [sid]


# ---------------------------------------------------------------- stream handling


class _Stream:
    """An upstream stream whose close can fail, as an SDK connection can."""

    def __init__(self, frames: list[bytes], error: Exception | None = None) -> None:
        self._frames = list(frames)
        self._error = error

    def __aiter__(self) -> _Stream:
        return self

    async def __anext__(self) -> bytes:
        if self._frames:
            return self._frames.pop(0)
        if self._error is not None:
            raise self._error
        raise StopAsyncIteration

    async def aclose(self) -> None:
        raise RuntimeError("close failed")


async def _failing(error: Exception) -> AsyncIterator[bytes]:
    raise error
    yield b""  # pragma: no cover


def _streaming(h: Any, stream: Any) -> None:
    h.fake.invoke_handler = lambda _ctx: UpstreamResponse(
        stream=stream, media_type="text/event-stream"
    )


async def test_a_failing_upstream_close_does_not_strand_the_lease() -> None:
    h = make_harness()
    _streaming(h, _Stream([b"one"]))
    result = await h.pool.execute(make_request(stream=True))
    assert result.stream is not None
    assert [frame async for frame in result.stream] == [b"one"]
    (record,) = await h.registry.list(AGENT)
    assert record.local_state is L.AVAILABLE and record.lease_request_id is None


async def test_a_failing_close_after_a_stream_error_still_reports_it_and_releases() -> None:
    h = make_harness()
    _streaming(h, _Stream([b"one"], FoundryUnavailable("boom")))
    result = await h.pool.execute(make_request(stream=True))
    assert result.stream is not None
    frames = [frame async for frame in result.stream]
    assert frames[0] == b"one" and frames[1].startswith(b"event: error")
    (record,) = await h.registry.list(AGENT)
    assert record.local_state is L.AVAILABLE


async def test_closing_an_unread_stream_releases_the_lease() -> None:
    h = make_harness()
    _streaming(h, _Stream([b"one"]))
    result = await h.pool.execute(make_request(stream=True))
    await result.close()
    (record,) = await h.registry.list(AGENT)
    assert record.local_state is L.AVAILABLE


async def test_an_error_before_the_first_frame_is_a_normal_error_not_a_200_stream() -> None:
    h = make_harness()
    _streaming(h, _failing(FoundryUnavailable("down")))
    with pytest.raises(FoundryUnavailableError):
        await h.pool.execute(make_request(stream=True))
    (record,) = await h.registry.list(AGENT)
    assert record.local_state is L.AVAILABLE  # the lease was released


async def test_a_session_failure_before_the_first_frame_deletes_the_session() -> None:
    h = make_harness()
    _streaming(h, _failing(FoundrySessionFailed()))
    with pytest.raises(SessionFailedError):
        await h.pool.execute(make_request(stream=True))
    assert h.fake.deleted == h.fake.created and await h.registry.count(AGENT) == 0


async def test_an_empty_stream_is_a_valid_empty_response() -> None:
    h = make_harness()
    h.fake.stream_frames = []
    result = await h.pool.execute(make_request(stream=True))
    assert result.stream is not None and [f async for f in result.stream] == []


def _error_event(frame: bytes) -> dict[str, Any]:
    text = frame.decode()
    assert text.startswith("event: error\ndata: ")
    event: dict[str, Any] = json.loads(text.split("data: ", 1)[1])
    return event


async def test_a_stream_error_event_has_one_documented_shape() -> None:
    h = make_harness()
    h.fake.stream_error = FoundryUnavailable("boom")
    request = make_request(stream=True)
    result = await h.pool.execute(request)
    assert result.stream is not None
    event = _error_event([f async for f in result.stream][-1])
    assert event["error_code"] == "FOUNDRY_UNAVAILABLE"
    assert event["detail"] and event["title"]
    assert event["correlation_id"] == request.correlation_id
    assert event["retry_after_seconds"] == h.settings.retry_after_seconds


async def test_the_stream_time_limit_event_has_the_same_shape() -> None:
    h = make_harness(max_stream_seconds=0.05)
    h.fake.stream_stall = True
    request = make_request(stream=True)
    result = await h.pool.execute(request)
    assert result.stream is not None
    event = _error_event([f async for f in result.stream][-1])
    assert event["error_code"] == "STREAM_TIMEOUT" and event["detail"]
    assert event["correlation_id"] == request.correlation_id


async def test_a_client_that_stops_reading_cannot_hold_the_response_past_the_limit() -> None:
    closed: list[bool] = []

    async def body() -> AsyncIterator[bytes]:
        yield b"a"
        yield b"b"

    async def on_end() -> None:
        closed.append(True)

    response = _BoundedStreamingResponse(
        body(), limit_seconds=0.05, on_end=on_end, media_type="text/event-stream", headers={}
    )

    async def stalled_send(message: dict[str, Any]) -> None:
        if message.get("body") == b"a":
            await asyncio.sleep(3600)

    async def receive() -> dict[str, Any]:
        await asyncio.sleep(3600)
        return {"type": "http.disconnect"}

    scope = {"type": "http", "asgi": {"spec_version": "2.3"}, "method": "GET"}
    await asyncio.wait_for(response(scope, receive, stalled_send), 2)  # type: ignore[arg-type]
    assert closed == [True]


# ------------------------------------------------------------------ request limits


async def test_the_overall_request_deadline_covers_everything_before_streaming() -> None:
    h = make_harness(max_request_seconds=0.05)
    h.fake.invoke_gate = asyncio.Event()  # the upstream never answers
    with pytest.raises(RequestTimeoutError):
        await h.pool.execute(make_request())
    (record,) = await h.registry.list(AGENT)
    assert record.local_state is L.AVAILABLE and record.lease_request_id is None


async def test_request_duration_includes_the_time_spent_queued() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 1})
    h.fake.invoke_gate = asyncio.Event()
    first = asyncio.create_task(h.pool.execute(make_request(user="u1")))
    await settle()
    second = asyncio.create_task(h.pool.execute(make_request(user="u2")))
    await settle()
    h.clock.advance(5)  # the second request waits in the queue for these seconds
    h.fake.invoke_gate.set()
    await asyncio.gather(first, second)
    histograms = h.metrics.snapshot()["agents"][AGENT]["histograms"]
    total = next(x["sum"] for x in histograms if x["name"] == "pool_request_duration_seconds")
    assert total >= 10  # both requests were outstanding for the five seconds


# -------------------------------------------------------------------------- JWKS


async def test_concurrent_requests_share_one_jwks_download() -> None:
    server = JwksServer()

    async def slow(request: httpx.Request) -> httpx.Response:
        await asyncio.sleep(0.01)
        return server.handler(request)

    client = httpx.AsyncClient(transport=httpx.MockTransport(slow))
    cache = JwksCache("https://keys.test/keys", 3600, client)
    keys = await asyncio.gather(*(cache.get("k1") for _ in range(20)))
    assert server.fetches == 1 and all(k is not None for k in keys)


# ---------------------------------------------------------------------- start-up


async def test_a_failed_startup_still_closes_the_foundry_client(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    def boom(*_args: Any, **_kwargs: Any) -> None:
        raise RuntimeError("container failed")

    monkeypatch.setattr("hosted_agent_kit.service.main.build_container", boom)
    fake = FakeFoundry()
    app = create_app(
        make_settings(), config=make_config({"a": {"mode": "stateless"}}, {}), adapter=fake
    )
    with pytest.raises(RuntimeError, match="container failed"):
        async with app.router.lifespan_context(app):
            pass
    assert fake.closed  # the client is released although nothing was built


# -------------------------------------------------------- Retry-After and limits


async def test_a_long_retry_after_on_create_is_returned_instead_of_retried_early() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 2, "create_retries": 2})
    h.fake.create_errors = [FoundryThrottled(10)]  # the backoff cap in tests is 0.05 seconds
    with pytest.raises(UpstreamThrottledError) as info:
        await h.pool.execute(make_request())
    assert info.value.retry_after_seconds == 10
    assert len(h.fake.create_requests) == 1 and not h.clock.sleeps  # one attempt, no early retry


async def test_a_long_retry_after_on_delete_defers_the_retry_to_the_next_sync() -> None:
    h = make_harness(delete_retries=3)
    h.fake.invoke_errors = [FoundrySessionFailed()]
    h.fake.delete_errors = [FoundryThrottled(10)]
    with pytest.raises(SessionFailedError):
        await h.pool.execute(make_request())
    assert not h.clock.sleeps and h.fake.deleted == []
    (record,) = await h.registry.list(AGENT)
    assert record.local_state is L.RETIRING


async def test_an_oversized_agent_response_maps_to_a_bad_gateway_error() -> None:
    h = make_harness()
    h.fake.invoke_errors = [FoundryResponseTooLarge()]
    with pytest.raises(ResponseTooLargeError) as info:
        await h.pool.execute(make_request())
    assert info.value.status == 502


async def test_a_stream_that_sends_nothing_before_the_limit_is_a_gateway_timeout() -> None:
    h = make_harness(max_stream_seconds=0.05)
    h.fake.stream_frames = []
    h.fake.stream_stall = True
    with pytest.raises(FoundryTimeoutAppError):
        await h.pool.execute(make_request(stream=True))
    (record,) = await h.registry.list(AGENT)
    assert record.local_state is L.AVAILABLE
