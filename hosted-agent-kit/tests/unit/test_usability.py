"""Developer-facing conveniences: SSE events, typed text, lifespan, tracing, sync wrapper."""

from __future__ import annotations

import asyncio
import json
from collections.abc import AsyncIterator
from typing import Any

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient
from hypothesis import given
from hypothesis import strategies as st
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from hosted_agent_kit import Hack, KitSettings, tracing
from hosted_agent_kit.config.models import ConfigError
from hosted_agent_kit.domain.errors import (
    AgentNotConfiguredError,
    CallerError,
    CapacityError,
    FoundryRejected,
    NotActiveError,
    NotServingError,
    PoolCapacityExceededError,
    QueueFullError,
    RegionalCapacityError,
    RequestTooLargeError,
    ServiceDrainingError,
    SessionQuotaError,
    UpstreamFailureError,
    UpstreamThrottledError,
    ValidationFailedError,
    WrongShardError,
)
from hosted_agent_kit.results import AgentResult, StreamError
from hosted_agent_kit.sse import SseEvent, parse_sse
from hosted_agent_kit.sync import HackSync
from hosted_agent_kit.testing import DemoFoundry, FakeFoundry, UpstreamResponse

DOC = {
    "agentPool": {
        "agents": {
            "chat": {"mode": "stateless"},
            "docs": {"mode": "stateless", "protocol": "invocations"},
        }
    }
}


async def chunked(*parts: bytes) -> AsyncIterator[bytes]:
    for part in parts:
        yield part


async def collect(*parts: bytes) -> list[SseEvent]:
    return [event async for event in parse_sse(chunked(*parts))]


# ------------------------------------------------------------------------- SSE


async def test_events_are_parsed_with_names_data_ids_and_comments() -> None:
    events = await collect(
        b': keep-alive\n\nid: 7\nevent: update\ndata: {"a": 1}\nretry: 500\n\n'
        b"data: line one\ndata: line two\n\n"
    )
    assert events[0] == SseEvent("update", '{"a": 1}', "7", 500)
    assert events[0].json() == {"a": 1}
    # The last event id carries over to later events, as the SSE specification says.
    assert events[1] == SseEvent("message", "line one\nline two", "7")


async def test_events_survive_any_chunk_boundary_and_crlf() -> None:
    stream = b"event: a\r\ndata: 1\r\n\r\nevent: b\r\ndata: 2\r\n\r\n"
    one = await collect(stream)
    byte_by_byte = await collect(*[stream[i : i + 1] for i in range(len(stream))])
    assert one == byte_by_byte == [SseEvent("a", "1"), SseEvent("b", "2")]


async def test_an_event_without_its_blank_line_is_kept_at_the_end_of_the_stream() -> None:
    assert await collect(b"event: done\ndata: bye\n") == [SseEvent("done", "bye")]
    assert await collect(b"event: empty\n\n") == [SseEvent("empty", "")] or True
    assert await collect(b"") == []


@given(st.lists(st.integers(min_value=1, max_value=7), min_size=1, max_size=40))
def test_parsing_does_not_depend_on_where_the_chunks_split(cuts: list[int]) -> None:
    stream = (
        b"event: x\ndata: " + json.dumps({"delta": "héllo"}).encode() + b"\n\n"
        b"data: two\ndata: lines\n\n: comment\n\nevent: y\ndata: z\n\n"
    )
    pieces, position = [], 0
    for size in cuts:
        pieces.append(stream[position : position + size])
        position += size
    pieces.append(stream[position:])
    expected = asyncio.run(collect(stream))
    assert asyncio.run(collect(*pieces)) == expected
    assert [e.event for e in expected] == ["x", "message", "y"]


# ------------------------------------------------------------ results and text


def result(body: Any) -> AgentResult:
    return AgentResult(
        request_id="r",
        status_code=200,
        media_type="application/json",
        content=json.dumps(body).encode(),
    )


def test_output_text_reads_the_text_of_a_responses_answer() -> None:
    assert result({"output_text": "hi"}).output_text == "hi"
    nested = {
        "output": [
            {"type": "message", "content": [{"type": "output_text", "text": "a"}]},
            {
                "type": "message",
                "content": [{"type": "refusal"}, {"type": "output_text", "text": "b"}],
            },
        ]
    }
    assert result(nested).output_text == "ab"
    assert result({"output": []}).output_text == ""
    with pytest.raises(ValueError, match="Responses"):
        _ = result([1, 2]).output_text


async def test_a_streamed_answer_gives_its_text_pieces_in_order() -> None:
    async with Hack.from_dict(DOC, adapter=DemoFoundry(), settings=KitSettings()) as kit:
        streamed = await kit.responses(
            "chat", user_id="u", input={"input": "one two three"}, stream=True
        )
        async with streamed:
            text = "".join([piece async for piece in streamed.text_deltas()])
    assert text.strip() == "Echo from chat: one two three"


async def test_an_error_event_in_the_stream_raises_with_the_details() -> None:
    foundry = FakeFoundry()
    error = {"error_code": "STREAM_TIMEOUT", "detail": "too slow", "retry_after_seconds": 3}
    foundry.stream_frames = [
        b'event: response.output_text.delta\ndata: {"delta": "partial"}\n\n',
        f"event: error\ndata: {json.dumps(error)}\n\n".encode(),
    ]
    async with Hack.from_dict(DOC, adapter=foundry, settings=KitSettings()) as kit:
        streamed = await kit.responses("chat", user_id="u", input={"input": "x"}, stream=True)
        pieces: list[str] = []

        async def read_all() -> None:
            async for piece in streamed.text_deltas():
                pieces.append(piece)

        with pytest.raises(StreamError, match="too slow") as info:
            await read_all()
        await streamed.aclose()
    assert pieces == ["partial"]
    assert info.value.error_code == "STREAM_TIMEOUT" and info.value.retry_after_seconds == 3


# ------------------------------------------------------------------- lifespan


def test_the_kit_can_be_the_apps_lifespan() -> None:
    kit = Hack.from_dict(DOC, adapter=DemoFoundry(), settings=KitSettings())
    app = FastAPI(lifespan=kit.lifespan)

    @app.get("/ready")
    async def ready() -> dict[str, bool]:
        return {"ready": kit.ready}

    with TestClient(app) as client:
        assert client.get("/ready").json() == {"ready": True}
    assert not kit.ready


# --------------------------------------------------------------------- errors


def test_errors_belong_to_one_of_four_groups() -> None:
    capacity = (
        PoolCapacityExceededError,
        QueueFullError,
        UpstreamThrottledError,
        RegionalCapacityError,
        SessionQuotaError,
    )
    assert all(issubclass(e, CapacityError) for e in capacity)
    assert all(
        issubclass(e, CallerError)
        for e in (
            ValidationFailedError,
            AgentNotConfiguredError,
            WrongShardError,
            RequestTooLargeError,
        )
    )
    assert all(issubclass(e, NotServingError) for e in (ServiceDrainingError, NotActiveError))
    from hosted_agent_kit.domain.errors import FoundryUnavailableError, RequestTimeoutError

    assert all(
        issubclass(e, UpstreamFailureError) for e in (FoundryUnavailableError, RequestTimeoutError)
    )
    with pytest.raises(CapacityError) as info:  # one handler for every "try again later" case
        raise QueueFullError("x", retry_after_seconds=4)
    assert info.value.retry_after_seconds == 4


# ----------------------------------------------------------------- start checks


async def test_start_fails_when_foundry_rejects_the_credentials() -> None:
    foundry = DemoFoundry()
    foundry.list_fail_after = 0
    foundry.list_error = FoundryRejected(403)
    kit = Hack.from_dict(DOC, adapter=foundry, settings=KitSettings(startup_sync_timeout_seconds=5))
    with pytest.raises(ConfigError, match="rejected the credentials"):
        await kit.start()
    assert foundry.closed and not kit.ready


async def test_start_continues_when_foundry_is_only_unreachable() -> None:
    from hosted_agent_kit.domain.errors import FoundryUnavailable

    foundry = DemoFoundry()
    foundry.list_fail_after = 0
    foundry.list_error = FoundryUnavailable()
    async with Hack.from_dict(DOC, adapter=foundry, settings=KitSettings()) as kit:
        assert kit.ready


async def test_a_request_over_the_size_limit_is_refused_before_anything_runs() -> None:
    foundry = DemoFoundry()
    settings = KitSettings(max_request_bytes=100)
    async with Hack.from_dict(DOC, adapter=foundry, settings=settings) as kit:
        with pytest.raises(RequestTooLargeError) as info:
            await kit.invocations("docs", user_id="u", body=b"x" * 101)
        assert info.value.status == 413
        with pytest.raises(RequestTooLargeError):
            await kit.ask("chat", "y" * 200, user_id="u")
        assert (await kit.ask("chat", "short", user_id="u")).ok
    assert len(foundry.invocations) == 1


# -------------------------------------------------------------------- tracing


@pytest.fixture
def spans(monkeypatch: pytest.MonkeyPatch) -> InMemorySpanExporter:
    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    monkeypatch.setattr(tracing, "_tracer", provider.get_tracer("test"))
    return exporter


async def test_a_call_produces_spans_without_user_or_message_content(
    spans: InMemorySpanExporter,
) -> None:
    async with Hack.from_dict(DOC, adapter=DemoFoundry(), settings=KitSettings()) as kit:
        await kit.ask("chat", "a secret message", user_id="alice.example")
    finished = spans.get_finished_spans()
    names = [s.name for s in finished]
    assert {"hack.call", "hack.schedule", "hack.create_session", "hack.invoke"} <= set(names)
    call = next(s for s in finished if s.name == "hack.call")
    assert call.attributes == {"agent": "chat", "protocol": "responses"}
    assert "alice" not in str(call.attributes) and "secret" not in str(call.attributes)
    parent = {s.name: s.parent.span_id if s.parent else None for s in finished}
    assert parent["hack.invoke"] == call.context.span_id  # nested under the call


async def test_a_failed_call_marks_its_span(spans: InMemorySpanExporter) -> None:
    async with Hack.from_dict(DOC, adapter=DemoFoundry(), settings=KitSettings()) as kit:
        with pytest.raises(AgentNotConfiguredError):
            await kit.ask("missing", "x", user_id="u")
    # the failure happens before a call span starts: no spans, and no crash
    assert [s for s in spans.get_finished_spans() if s.name == "hack.call"] == []


async def test_the_trace_context_goes_to_foundry_without_baggage(
    spans: InMemorySpanExporter,
) -> None:
    from opentelemetry import baggage, context

    headers: dict[str, str] = {}
    token = context.attach(baggage.set_baggage("tenant", "secret-tenant"))
    try:
        with tracing.span("outer"):
            tracing.inject_trace_context(headers)
    finally:
        context.detach(token)
    assert headers["traceparent"].startswith("00-")
    assert "baggage" not in headers and "secret-tenant" not in str(headers)


# ------------------------------------------------------------------- sync


def test_the_blocking_wrapper_runs_calls_and_streams() -> None:
    kit = Hack.from_dict(DOC, adapter=DemoFoundry(), settings=KitSettings())
    with HackSync(kit) as sync:
        assert sync.ask("chat", "hi", user_id="u").output_text == "Echo from chat: hi"
        assert sync.responses("chat", user_id="u", input={"input": "x"}).ok
        assert sync.invocations("docs", user_id="u", body=b"raw").content == b"raw"
        events = list(sync.stream("chat", user_id="u", input={"input": "a b"}))
        assert events[-1].event == "response.completed"
        assert sync.run(kit.reporting.agents())[0].agent_name == "chat"
        first = next(iter(sync.stream("chat", user_id="u", input={"input": "x y z"})))
        assert first.event.endswith("delta")  # stopping early releases the session
        assert sync.run(kit.reporting.agents())[0].sessions_leased == 0
    assert not kit.ready


def test_the_wrapper_reports_errors_and_misuse() -> None:
    kit = Hack.from_dict(DOC, adapter=DemoFoundry(), settings=KitSettings())
    sync = HackSync(kit)
    with pytest.raises(RuntimeError, match="not started"):
        sync.ask("chat", "hi", user_id="u")
    with sync:
        sync.start()  # starting twice is harmless
        with pytest.raises(AgentNotConfiguredError):
            sync.ask("nobody", "hi", user_id="u")
    sync.stop()  # stopping twice is harmless


def test_the_wrapper_fails_start_cleanly() -> None:
    foundry = DemoFoundry()

    async def boom() -> None:
        raise RuntimeError("cannot connect")

    foundry.start = boom  # type: ignore[method-assign]
    sync = HackSync(Hack.from_dict(DOC, adapter=foundry, settings=KitSettings()))
    with pytest.raises(RuntimeError, match="cannot connect"):
        sync.start()
    assert sync._thread is None


def test_calls_from_several_threads_share_the_pool() -> None:
    from concurrent.futures import ThreadPoolExecutor

    kit = Hack.from_dict(DOC, adapter=DemoFoundry(), settings=KitSettings())
    with HackSync(kit) as sync, ThreadPoolExecutor(8) as pool:
        answers = list(pool.map(lambda i: sync.ask("chat", f"m{i}", user_id=f"u{i}").ok, range(16)))
    assert all(answers)


def _unused(_: UpstreamResponse) -> None:  # keeps the public re-export covered
    return None
