"""The client: authentication, problem details, retry guidance, correlation ids and streaming."""

from __future__ import annotations

import asyncio
import inspect
import json
import uuid
from collections.abc import AsyncIterator, Awaitable, Callable, Mapping
from types import TracebackType
from typing import Any, Self

import httpx

from hosted_agent_kit_client.errors import PoolError, PoolStreamError
from hosted_agent_kit_client.models import AgentInfo, InvocationResult, ResponsesResult
from hosted_agent_kit_client.sse import SseEvent, parse_sse

TokenProvider = str | Callable[[], str | Awaitable[str]]
Body = bytes | str | dict[str, Any] | list[Any] | int | float | bool | None
_RETRYABLE_STATUS = {429, 503, 504}


class PoolClient:
    """Talks to one Foundry Agent Pool service.

    ``token`` is a bearer token, or a function (sync or async) that returns a fresh one for each
    attempt. Errors that are safe to repeat and carry a retry delay are retried up to
    ``max_retries`` times, waiting at least the delay the service asked for. A delay longer than
    ``max_retry_after`` is not waited for: the error is raised with the delay on it.
    """

    def __init__(
        self,
        base_url: str,
        *,
        token: TokenProvider | None = None,
        http: httpx.AsyncClient | None = None,
        max_retries: int = 2,
        max_retry_after: float = 30.0,
        timeout: float = 180.0,
        sleep: Callable[[float], Awaitable[None]] = asyncio.sleep,
    ) -> None:
        self._token = token
        self._max_retries = max_retries
        self._max_retry_after = max_retry_after
        self._sleep = sleep
        self._owns_http = http is None
        self._http = http or httpx.AsyncClient(base_url=base_url, timeout=timeout)
        self._base = "" if http is None else base_url.rstrip("/")

    async def __aenter__(self) -> Self:
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        await self.aclose()

    async def aclose(self) -> None:
        if self._owns_http:
            await self._http.aclose()

    # ------------------------------------------------------------------ discovery

    async def agents(self) -> list[AgentInfo]:
        response = await self._send("GET", "/v1/agents")
        return [AgentInfo.from_json(item) for item in response.json()]

    async def agent(self, name: str) -> AgentInfo:
        response = await self._send("GET", f"/v1/agents/{name}")
        return AgentInfo.from_json(response.json())

    # ------------------------------------------------------------------ responses

    async def respond(
        self,
        agent: str,
        input: dict[str, Any],
        *,
        conversation_key: str | None = None,
        subject: str | None = None,
        idempotency_key: str | None = None,
        timeout_seconds: float | None = None,
    ) -> ResponsesResult:
        """Call a Responses agent and return the full result."""
        body: dict[str, Any] = {"input": input}
        if timeout_seconds is not None:
            body["timeout_seconds"] = timeout_seconds
        response = await self._send(
            "POST",
            f"/v1/agents/{agent}/responses",
            json_body=body,
            headers=self._headers(conversation_key, subject, idempotency_key),
        )
        return self._responses_result(response)

    async def chat(
        self,
        agent: str,
        message: str,
        *,
        previous_response_id: str | None = None,
        conversation_id: str | None = None,
        conversation_key: str | None = None,
        subject: str | None = None,
        idempotency_key: str | None = None,
    ) -> ResponsesResult:
        body: dict[str, Any] = {"message": message}
        if previous_response_id:
            body["previous_response_id"] = previous_response_id
        if conversation_id:
            body["conversation_id"] = conversation_id
        response = await self._send(
            "POST",
            f"/v1/agents/{agent}/chat",
            json_body=body,
            headers=self._headers(conversation_key, subject, idempotency_key),
        )
        return self._responses_result(response)

    async def stream_responses(
        self,
        agent: str,
        input: dict[str, Any],
        *,
        conversation_key: str | None = None,
        subject: str | None = None,
        timeout_seconds: float | None = None,
    ) -> AsyncIterator[SseEvent]:
        """Yield the agent's events. A final error event raises ``PoolStreamError``."""
        body: dict[str, Any] = {"input": input, "stream": True}
        if timeout_seconds is not None:
            body["timeout_seconds"] = timeout_seconds
        async for event in self._stream(
            f"/v1/agents/{agent}/responses",
            json_body=body,
            headers=self._headers(conversation_key, subject, None),
        ):
            yield event

    # ---------------------------------------------------------------- invocations

    async def invoke(
        self,
        agent: str,
        body: Body = None,
        *,
        content_type: str | None = None,
        conversation_key: str | None = None,
        subject: str | None = None,
        idempotency_key: str | None = None,
        timeout_seconds: float | None = None,
    ) -> InvocationResult:
        """Call an Invocations agent. ``body`` may be bytes, text, or any JSON value."""
        content, media = _encode_body(body, content_type)
        headers = self._headers(conversation_key, subject, idempotency_key)
        if media:
            headers["Content-Type"] = media
        params = {"timeout_seconds": timeout_seconds} if timeout_seconds is not None else None
        response = await self._send(
            "POST",
            f"/v1/agents/{agent}/invocations",
            content=content,
            headers=headers,
            params=params,
        )
        return InvocationResult(
            status_code=response.status_code,
            content_type=response.headers.get("content-type", ""),
            body=response.content,
            request_id=response.headers.get("x-request-id"),
            correlation_id=response.headers.get("x-correlation-id"),
            replayed=response.headers.get("idempotent-replayed") == "true",
            headers=dict(response.headers),
        )

    async def stream_invocation(
        self,
        agent: str,
        body: Body = None,
        *,
        content_type: str | None = None,
        conversation_key: str | None = None,
        subject: str | None = None,
    ) -> AsyncIterator[SseEvent]:
        """Call an Invocations agent that answers with server-sent events."""
        content, media = _encode_body(body, content_type)
        headers = self._headers(conversation_key, subject, None)
        if media:
            headers["Content-Type"] = media
        async for event in self._stream(
            f"/v1/agents/{agent}/invocations", content=content, headers=headers
        ):
            yield event

    # ------------------------------------------------------------------- plumbing

    @staticmethod
    def _headers(
        conversation_key: str | None, subject: str | None, idempotency_key: str | None
    ) -> dict[str, str]:
        headers: dict[str, str] = {}
        if conversation_key:
            headers["X-Conversation-Key"] = conversation_key
        if subject:
            headers["X-Pool-Subject"] = subject
        if idempotency_key:
            headers["Idempotency-Key"] = idempotency_key
        return headers

    @staticmethod
    def _responses_result(response: httpx.Response) -> ResponsesResult:
        data = response.json()
        return ResponsesResult(
            request_id=data["request_id"],
            result=data["result"],
            correlation_id=response.headers.get("x-correlation-id"),
            replayed=response.headers.get("idempotent-replayed") == "true",
        )

    async def _auth(self) -> dict[str, str]:
        source = self._token
        value: str | Awaitable[str] | None = source() if callable(source) else source
        if inspect.isawaitable(value):
            value = await value
        return {"Authorization": f"Bearer {value}"} if value else {}

    def _request_headers(self, headers: Mapping[str, str] | None, correlation_id: str) -> Any:
        return {"X-Correlation-ID": correlation_id, **(headers or {})}

    async def _send(
        self,
        method: str,
        path: str,
        *,
        json_body: Any = None,
        content: bytes | None = None,
        headers: Mapping[str, str] | None = None,
        params: Mapping[str, Any] | None = None,
    ) -> httpx.Response:
        correlation_id = uuid.uuid4().hex  # the same id on every attempt of one call
        attempt = 0
        while True:
            request_headers = {
                **self._request_headers(headers, correlation_id),
                **await self._auth(),
            }
            try:
                response = await self._http.request(
                    method,
                    self._base + path,
                    json=json_body,
                    content=content,
                    headers=request_headers,
                    params=params,
                )
            except httpx.ConnectError:
                # Nothing was sent, so repeating is always safe.
                if attempt >= self._max_retries:
                    raise
                attempt += 1
                await self._sleep(min(0.25 * 2**attempt, self._max_retry_after))
                continue
            if response.status_code < 400:
                return response
            error = _error_from(response)
            delay = self._retry_delay(error, response, attempt)
            if delay is None:
                raise error
            attempt += 1
            await self._sleep(delay)

    def _retry_delay(
        self, error: PoolError, response: httpx.Response, attempt: int
    ) -> float | None:
        if attempt >= self._max_retries or not error.retry_safe:
            return None
        if response.status_code not in _RETRYABLE_STATUS:
            return None
        asked = error.retry_after_seconds
        header = response.headers.get("retry-after")
        if asked is None and header is not None and header.isdigit():
            asked = float(header)
        if asked is not None and asked > self._max_retry_after:
            return None  # waiting that long is the caller's decision
        backoff: float = min(0.25 * 2 ** (attempt + 1), self._max_retry_after)
        return max(asked or 0.0, backoff)

    async def _stream(
        self,
        path: str,
        *,
        json_body: Any = None,
        content: bytes | None = None,
        headers: Mapping[str, str] | None = None,
    ) -> AsyncIterator[SseEvent]:
        correlation_id = uuid.uuid4().hex
        request_headers = {**self._request_headers(headers, correlation_id), **await self._auth()}
        request = self._http.build_request(
            "POST", self._base + path, json=json_body, content=content, headers=request_headers
        )
        response = await self._http.send(request, stream=True)
        try:
            if response.status_code >= 400:
                await response.aread()
                raise _error_from(response)
            async for event in parse_sse(response.aiter_text()):
                if event.event == "error" and isinstance(event.data, dict):
                    raise PoolStreamError(event.data)
                yield event
        finally:
            await response.aclose()


def _encode_body(body: Body, content_type: str | None) -> tuple[bytes | None, str | None]:
    if body is None:
        return None, content_type
    if isinstance(body, bytes):
        return body, content_type or "application/octet-stream"
    if isinstance(body, str):
        return body.encode(), content_type or "text/plain; charset=utf-8"
    return json.dumps(body).encode(), content_type or "application/json"


def _error_from(response: httpx.Response) -> PoolError:
    try:
        problem = response.json()
    except ValueError:
        problem = {}
    if not isinstance(problem, dict):
        problem = {}
    problem.setdefault("title", response.reason_phrase)
    problem.setdefault("detail", response.text[:200])
    problem.setdefault("correlation_id", response.headers.get("x-correlation-id"))
    problem.setdefault("request_id", response.headers.get("x-request-id"))
    return PoolError(response.status_code, problem)
