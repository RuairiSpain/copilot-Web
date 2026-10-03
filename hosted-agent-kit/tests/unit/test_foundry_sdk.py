"""SdkFoundryAdapter against SDK-shaped stubs, using the real SDK models and exceptions."""

from __future__ import annotations

from collections.abc import AsyncIterator
from datetime import UTC, datetime
from types import SimpleNamespace
from typing import Any

import httpx2
import openai
import pytest
from azure.ai.projects.models import AgentSessionResource, VersionRefIndicator
from azure.core.exceptions import (
    AzureError,
    ClientAuthenticationError,
    HttpResponseError,
    ResourceNotFoundError,
    ServiceRequestError,
)

from hosted_agent_kit.adapters import foundry_sdk
from hosted_agent_kit.adapters.foundry_sdk import (
    SdkFoundryAdapter,
    normalise_session,
    scrub_session_id,
    sse_frame,
    translate_exception,
)
from hosted_agent_kit.domain.enums import FoundrySessionStatus
from hosted_agent_kit.domain.errors import (
    FoundryRejected,
    FoundrySessionFailed,
    FoundrySessionNotFound,
    FoundryThrottled,
    FoundryTimeout,
    FoundryUnavailable,
)
from hosted_agent_kit.domain.models import InvokeContext

NOW = datetime(2026, 10, 2, tzinfo=UTC)
REQUEST = httpx2.Request("POST", "https://example.test/responses")


def resource(sid: str = "s1", status: str = "active", **kwargs: Any) -> AgentSessionResource:
    values: dict[str, Any] = {
        "agent_session_id": sid,
        "version_indicator": VersionRefIndicator(agent_version="4"),
        "status": status,
        "created_at": NOW,
        "last_accessed_at": NOW,
        "expires_at": NOW,
    }
    values.update(kwargs)
    return AgentSessionResource(**values)


def http_error(status: int, headers: dict[str, str] | None = None) -> HttpResponseError:
    response = SimpleNamespace(status_code=status, headers=headers or {}, reason="x")
    return HttpResponseError(message="boom", response=response)  # type: ignore[arg-type]


def openai_status(
    cls: type[openai.APIStatusError], status: int, headers: dict[str, str] | None = None
) -> openai.APIStatusError:
    response = httpx2.Response(status, request=REQUEST, headers=headers or {})
    return cls("boom", response=response, body=None)


class StubAgents:
    def __init__(self) -> None:
        self.calls: list[tuple[str, tuple[Any, ...], dict[str, Any]]] = []
        self.sessions: list[Any] = []
        self.error: Exception | None = None
        self.get_result: Any = resource()
        self.latest_version = "7"
        self.created = resource("new", "creating")
        self.mid_list_error: Exception | None = None

    async def _maybe_fail(self) -> None:
        if self.error is not None:
            raise self.error

    def list(self, **kwargs: Any) -> AsyncIterator[Any]:
        self.calls.append(("list", (), kwargs))

        async def gen() -> AsyncIterator[Any]:
            await self._maybe_fail()
            yield SimpleNamespace(name="agent-a", state=SimpleNamespace(value="enabled"))
            yield SimpleNamespace(name="agent-b", state="disabled")

        return gen()

    def list_sessions(self, agent: str, **kwargs: Any) -> AsyncIterator[Any]:
        self.calls.append(("list_sessions", (agent,), kwargs))

        async def gen() -> AsyncIterator[Any]:
            for index, item in enumerate(self.sessions):
                if self.mid_list_error is not None and index == 1:
                    raise self.mid_list_error
                yield item

        return gen()

    async def get_session(self, agent: str, session_id: str) -> Any:
        self.calls.append(("get_session", (agent, session_id), {}))
        await self._maybe_fail()
        return self.get_result

    async def get(self, agent: str) -> Any:
        self.calls.append(("get", (agent,), {}))
        await self._maybe_fail()
        return SimpleNamespace(
            versions=SimpleNamespace(latest=SimpleNamespace(version=self.latest_version))
        )

    async def create_session(self, agent: str, **kwargs: Any) -> Any:
        self.calls.append(("create_session", (agent,), kwargs))
        await self._maybe_fail()
        return self.created

    async def stop_session(self, agent: str, session_id: str) -> None:
        self.calls.append(("stop_session", (agent, session_id), {}))
        await self._maybe_fail()

    async def delete_session(self, agent: str, session_id: str) -> None:
        self.calls.append(("delete_session", (agent, session_id), {}))
        await self._maybe_fail()


class StubResponses:
    def __init__(self) -> None:
        self.calls: list[dict[str, Any]] = []
        self.error: Exception | None = None
        self.result: Any = SimpleNamespace(
            model_dump=lambda mode, **_: {
                "id": "resp_1",
                "status": "completed",
                "agent_session_id": "SECRET",
                "output": [{"agent_session_id": "SECRET", "t": 1}],
            }
        )
        self.events: list[Any] = []
        self.stream_error: Exception | None = None
        self.stream_closed = False

    async def create(self, **kwargs: Any) -> Any:
        self.calls.append(kwargs)
        if self.error is not None:
            raise self.error
        if kwargs.get("stream"):
            return self._stream()
        return self.result

    def _stream(self) -> Any:
        outer = self

        class Stream:
            def __aiter__(self) -> AsyncIterator[Any]:
                async def gen() -> AsyncIterator[Any]:
                    for event in outer.events:
                        yield event
                    if outer.stream_error is not None:
                        raise outer.stream_error

                return gen()

            async def close(self) -> None:
                outer.stream_closed = True

        return Stream()


class StubOpenAI:
    def __init__(self) -> None:
        self.responses = StubResponses()
        self.closed = False

    async def close(self) -> None:
        self.closed = True


class StubProject:
    def __init__(self) -> None:
        self.agents = StubAgents()
        self.openai = StubOpenAI()
        self.openai_requests: list[tuple[str | None, dict[str, Any]]] = []
        self.closed = False

    def get_openai_client(self, *, agent_name: str | None = None, **kwargs: Any) -> StubOpenAI:
        self.openai_requests.append((agent_name, kwargs))
        return self.openai

    async def close(self) -> None:
        self.closed = True


@pytest.fixture
async def stub() -> tuple[SdkFoundryAdapter, StubProject]:
    project = StubProject()
    adapter = SdkFoundryAdapter("https://x", client_factory=lambda: project)  # type: ignore[arg-type,return-value]
    await adapter.start()
    return adapter, project


def ctx(stream: bool = False, payload: dict[str, Any] | None = None) -> InvokeContext:
    return InvokeContext(
        agent_name="agent-a",
        session_id="sess-123",
        payload=payload or {"input": "hi", "stream": stream},
        timeout_seconds=12.5,
        request_id="r",
        correlation_id="c",
        stream=stream,
    )


# ------------------------------------------------------------ normalisation


def test_normalise_session_maps_all_fields() -> None:
    session = normalise_session("agent-a", resource())
    assert session.session_id == "s1" and session.agent_name == "agent-a"
    assert session.agent_version == "4" and session.status is FoundrySessionStatus.ACTIVE
    assert session.created_at == NOW and session.expires_at == NOW


@pytest.mark.parametrize(
    "status", [s.value for s in FoundrySessionStatus if s is not FoundrySessionStatus.UNKNOWN]
)
def test_every_documented_status_maps(status: str) -> None:
    assert normalise_session("a", resource(status=status)).status.value == status


def test_unknown_status_and_missing_optionals_are_tolerated() -> None:
    raw = {"agent_session_id": "s", "status": "hibernating", "created_at": 1_700_000_000}
    session = normalise_session("a", raw)
    assert session.status is FoundrySessionStatus.UNKNOWN
    assert session.agent_version is None and session.last_accessed_at is None
    assert session.created_at == datetime.fromtimestamp(1_700_000_000, UTC)


def test_naive_datetimes_are_treated_as_utc_and_garbage_is_ignored() -> None:
    raw = {
        "agent_session_id": "s",
        "status": "idle",
        "created_at": datetime(2026, 1, 1),
        "last_accessed_at": "not-a-date",
    }
    session = normalise_session("a", raw)
    assert session.created_at.tzinfo is UTC and session.last_accessed_at is None


def test_status_enum_instances_are_accepted() -> None:
    from azure.ai.projects.models import AgentSessionStatus

    assert (
        normalise_session("a", resource(status=AgentSessionStatus.FAILED)).status
        is FoundrySessionStatus.FAILED
    )


def test_scrub_session_id_is_recursive() -> None:
    data = {
        "a": 1,
        "agent_session_id": "x",
        "n": [{"agent_session_id": "y", "k": 2}, 3],
        "d": {"agent_session_id": "z"},
    }
    assert scrub_session_id(data) == {"a": 1, "n": [{"k": 2}, 3], "d": {}}


def test_sse_frame_format() -> None:
    assert sse_frame("x", {"a": 1}) == b'event: x\ndata: {"a":1}\n\n'


# --------------------------------------------------------------- translation


@pytest.mark.parametrize(
    ("exc", "expected"),
    [
        (ResourceNotFoundError("x"), FoundrySessionNotFound),
        (ClientAuthenticationError("x"), FoundryRejected),
        (http_error(404), FoundrySessionNotFound),
        (http_error(429, {"retry-after": "3"}), FoundryThrottled),
        (http_error(408), FoundryTimeout),
        (http_error(504), FoundryTimeout),
        (http_error(500), FoundryUnavailable),
        (http_error(503), FoundryUnavailable),
        (http_error(400), FoundryRejected),
        (http_error(403), FoundryRejected),
        (ServiceRequestError("down"), FoundryUnavailable),
        (AzureError("x"), FoundryUnavailable),
        (TimeoutError(), FoundryTimeout),
        (openai.APITimeoutError(request=REQUEST), FoundryTimeout),
        (openai.APIConnectionError(request=REQUEST), FoundryUnavailable),
        (openai_status(openai.NotFoundError, 404), FoundrySessionNotFound),
        (openai_status(openai.RateLimitError, 429, {"retry-after": "9"}), FoundryThrottled),
        (openai_status(openai.InternalServerError, 502), FoundryUnavailable),
        (openai_status(openai.BadRequestError, 400), FoundryRejected),
    ],
)
def test_exception_translation(exc: BaseException, expected: type[Exception]) -> None:
    assert isinstance(translate_exception(exc), expected)


def test_retry_after_is_parsed_and_bad_values_ignored() -> None:
    assert translate_exception(http_error(429, {"retry-after": "3"})).retry_after_seconds == 3.0  # type: ignore[union-attr]
    assert translate_exception(http_error(429, {"retry-after": "soon"})).retry_after_seconds is None  # type: ignore[union-attr]
    assert translate_exception(http_error(429, {"retry-after": "-4"})).retry_after_seconds is None  # type: ignore[union-attr]
    assert translate_exception(http_error(429)).retry_after_seconds is None  # type: ignore[union-attr]


def test_status_less_http_error_defaults_to_server_error() -> None:
    error = HttpResponseError(message="x")
    assert isinstance(translate_exception(error), FoundryUnavailable)


def test_adapter_errors_pass_through_and_unknown_errors_are_not_ours() -> None:
    original = FoundrySessionFailed()
    assert translate_exception(original) is original
    assert translate_exception(KeyError("bug")) is None


# ------------------------------------------------------------------ lifecycle


async def test_methods_require_start() -> None:
    adapter = SdkFoundryAdapter("https://x")
    with pytest.raises(RuntimeError, match="not started"):
        await adapter.get_session("a", "s")


async def test_start_builds_real_client_with_preview_flag_and_close_releases(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    built: dict[str, Any] = {}

    class Credential:
        closed = False

        async def close(self) -> None:
            Credential.closed = True

    class Client(StubProject):
        def __init__(self, **kwargs: Any) -> None:
            super().__init__()
            built.update(kwargs)

    monkeypatch.setattr(foundry_sdk, "DefaultAzureCredential", Credential)
    monkeypatch.setattr(foundry_sdk, "AIProjectClient", Client)
    adapter = SdkFoundryAdapter("https://endpoint")
    await adapter.start()
    assert built["endpoint"] == "https://endpoint" and built["allow_preview"] is True
    assert built["retry_status"] == 0
    assert isinstance(built["credential"], Credential)
    await adapter.close()
    assert Credential.closed
    await adapter.close()  # idempotent


async def test_close_releases_openai_clients_and_project(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    await adapter.invoke(ctx())
    await adapter.close()
    assert project.openai.closed and project.closed


# --------------------------------------------------------- session operations


async def test_list_agents_requests_hosted_agents_only(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    agents = [a async for a in adapter.list_agents()]
    assert [(a.name, a.state) for a in agents] == [("agent-a", "enabled"), ("agent-b", "disabled")]
    assert project.agents.calls[0][2]["kind"] == "hosted"


async def test_list_sessions_pages_through_sdk_and_normalises(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    project.agents.sessions = [resource("a"), resource("b", "idle"), resource("c", "failed")]
    sessions = [s async for s in adapter.list_sessions("agent-a")]
    assert [s.session_id for s in sessions] == ["a", "b", "c"]
    assert sessions[2].status is FoundrySessionStatus.FAILED
    assert project.agents.calls[0][2]["limit"] == 100


async def test_list_sessions_failure_midway_is_translated(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    project.agents.sessions = [resource("a"), resource("b")]
    project.agents.mid_list_error = http_error(503)
    seen: list[str] = []

    async def consume() -> None:
        async for session in adapter.list_sessions("agent-a"):
            seen.append(session.session_id)

    with pytest.raises(FoundryUnavailable):
        await consume()
    assert seen == ["a"]


async def test_list_errors_are_translated_and_unknown_errors_propagate(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    project.agents.error = http_error(429, {"retry-after": "1"})
    with pytest.raises(FoundryThrottled):
        [a async for a in adapter.list_agents()]
    project.agents.error = KeyError("programming error")
    with pytest.raises(KeyError):
        [a async for a in adapter.list_agents()]


async def test_get_session(stub: tuple[SdkFoundryAdapter, StubProject]) -> None:
    adapter, project = stub
    session = await adapter.get_session("agent-a", "s1")
    assert session.session_id == "s1"
    project.agents.error = ResourceNotFoundError("gone")
    with pytest.raises(FoundrySessionNotFound):
        await adapter.get_session("agent-a", "s1")


async def test_create_session_pins_the_latest_agent_version(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    session = await adapter.create_session("agent-a")
    assert session.session_id == "new" and session.status is FoundrySessionStatus.CREATING
    create_call = next(c for c in project.agents.calls if c[0] == "create_session")
    indicator = create_call[2]["version_indicator"]
    assert isinstance(indicator, VersionRefIndicator) and indicator.agent_version == "7"
    project.agents.error = http_error(500)
    with pytest.raises(FoundryUnavailable):
        await adapter.create_session("agent-a")


async def test_stop_and_delete_session(stub: tuple[SdkFoundryAdapter, StubProject]) -> None:
    adapter, project = stub
    await adapter.stop_session("agent-a", "s1")
    await adapter.delete_session("agent-a", "s1")
    assert [c[0] for c in project.agents.calls] == ["stop_session", "delete_session"]
    project.agents.error = http_error(503)
    with pytest.raises(FoundryUnavailable):
        await adapter.stop_session("agent-a", "s1")
    with pytest.raises(FoundryUnavailable):
        await adapter.delete_session("agent-a", "s1")


# ----------------------------------------------------------------- invocation


async def test_invoke_binds_session_without_retries_and_scrubs_the_response(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    response = await adapter.invoke(ctx())
    assert project.openai_requests == [("agent-a", {"max_retries": 0})]
    call = project.openai.responses.calls[0]
    assert call["extra_body"] == {"input": "hi", "agent_session_id": "sess-123"}
    assert call["timeout"] == 12.5 and "stream" not in call
    assert response.body == {"id": "resp_1", "status": "completed", "output": [{"t": 1}]}
    assert "SECRET" not in repr(response.body) and response.stream is None


async def test_invoke_reuses_one_openai_client_per_agent(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    await adapter.invoke(ctx())
    await adapter.invoke(ctx())
    assert len(project.openai_requests) == 1


async def test_invoke_streams_scrubbed_sse_frames_and_closes_upstream(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    project.openai.responses.events = [
        SimpleNamespace(
            model_dump=lambda mode, **_: {
                "type": "response.created",
                "response": {"agent_session_id": "SECRET", "id": "r"},
            }
        ),
        SimpleNamespace(model_dump=lambda mode, **_: {"id": "no-type"}),
    ]
    response = await adapter.invoke(ctx(stream=True))
    assert response.media_type == "text/event-stream" and response.body is None
    assert project.openai.responses.calls[0]["stream"] is True
    assert "stream" not in project.openai.responses.calls[0]["extra_body"]
    assert response.stream is not None
    frames = [f async for f in response.stream]
    assert frames[0].startswith(b"event: response.created") and b"SECRET" not in b"".join(frames)
    assert frames[1].startswith(b"event: message")
    assert project.openai.responses.stream_closed


async def test_stream_failure_is_translated_and_stream_still_closed(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    project.openai.responses.stream_error = openai.APIConnectionError(request=REQUEST)
    response = await adapter.invoke(ctx(stream=True))
    assert response.stream is not None
    with pytest.raises(FoundryUnavailable):
        [f async for f in response.stream]
    assert project.openai.responses.stream_closed


@pytest.mark.parametrize(
    ("error", "expected"),
    [
        (openai.APITimeoutError(request=REQUEST), FoundryTimeout),
        (openai.APIConnectionError(request=REQUEST), FoundryUnavailable),
        (openai_status(openai.RateLimitError, 429, {"retry-after": "5"}), FoundryThrottled),
        (openai_status(openai.BadRequestError, 400), FoundryRejected),
        (openai_status(openai.AuthenticationError, 401), FoundryRejected),
    ],
)
async def test_invoke_error_classification(
    stub: tuple[SdkFoundryAdapter, StubProject], error: Exception, expected: type[Exception]
) -> None:
    adapter, project = stub
    project.openai.responses.error = error
    with pytest.raises(expected):
        await adapter.invoke(ctx())
    assert not [c for c in project.agents.calls if c[0] == "get_session"]  # no extra lookups


async def test_invoke_404_with_missing_session_means_session_not_found(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    project.openai.responses.error = openai_status(openai.NotFoundError, 404)
    project.agents.error = ResourceNotFoundError("gone")
    with pytest.raises(FoundrySessionNotFound):
        await adapter.invoke(ctx())


async def test_invoke_404_with_existing_session_is_a_routing_rejection(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    project.openai.responses.error = openai_status(openai.NotFoundError, 404)
    with pytest.raises(FoundryRejected) as info:
        await adapter.invoke(ctx())
    assert info.value.status_code == 404


async def test_invoke_404_failed_lookup_keeps_original_classification(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    project.openai.responses.error = openai_status(openai.NotFoundError, 404)
    project.agents.error = http_error(503)
    with pytest.raises(FoundrySessionNotFound):
        await adapter.invoke(ctx())


async def test_invoke_500_on_a_failed_session_reports_session_failed(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    project.openai.responses.error = openai_status(openai.InternalServerError, 500)
    project.agents.get_result = resource(status="failed")
    with pytest.raises(FoundrySessionFailed):
        await adapter.invoke(ctx())


async def test_invoke_500_on_a_healthy_session_is_unavailable(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    project.openai.responses.error = openai_status(openai.InternalServerError, 500)
    with pytest.raises(FoundryUnavailable):
        await adapter.invoke(ctx())


async def test_invoke_500_with_session_gone_reports_not_found(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    project.openai.responses.error = openai_status(openai.InternalServerError, 500)
    project.agents.error = ResourceNotFoundError("gone")
    with pytest.raises(FoundrySessionNotFound):
        await adapter.invoke(ctx())


async def test_invoke_programming_errors_are_not_disguised(
    stub: tuple[SdkFoundryAdapter, StubProject],
) -> None:
    adapter, project = stub
    project.openai.responses.error = KeyError("bug")
    with pytest.raises(KeyError):
        await adapter.invoke(ctx())


async def test_an_error_while_relaying_an_invocations_stream_is_translated_and_closes() -> None:
    from hosted_agent_kit.adapters.foundry_sdk import _raw_stream

    class Response:
        closed = False

        async def iter_bytes(self) -> AsyncIterator[bytes]:
            yield b"data: one\n\n"
            raise http_error(503)

        async def close(self) -> None:
            self.closed = True

    response = Response()
    received: list[bytes] = []

    async def relay() -> None:
        async for chunk in _raw_stream(response):
            received.append(chunk)

    with pytest.raises(FoundryUnavailable):
        await relay()
    assert received == [b"data: one\n\n"] and response.closed


def test_retry_after_accepts_an_http_date() -> None:
    from datetime import UTC, datetime, timedelta
    from email.utils import format_datetime

    future = format_datetime(datetime.now(UTC) + timedelta(seconds=30), usegmt=True)
    parsed = translate_exception(http_error(429, {"retry-after": future})).retry_after_seconds  # type: ignore[union-attr]
    assert parsed is not None and 25 <= parsed <= 31
    past = format_datetime(datetime.now(UTC) - timedelta(seconds=30), usegmt=True)
    assert translate_exception(http_error(429, {"retry-after": past})).retry_after_seconds == 0.0  # type: ignore[union-attr]
