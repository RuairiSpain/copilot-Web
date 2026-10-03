"""Foundry adapter built on the stable ``azure-ai-projects`` SDK.

This is the only module that imports Azure SDK or OpenAI client types. Session lifecycle
calls use ``AIProjectClient.agents``. The Responses protocol uses the OpenAI-compatible client
that the SDK exposes through ``get_openai_client`` (it requires ``allow_preview=True``). The
Invocations protocol has no typed SDK client, so it is sent through the SDK's own pipeline with
``send_request``, which keeps authentication and the retry policy. See
docs/adr/0002-foundry-sdk-and-invocation.md.
"""

from __future__ import annotations

import asyncio
import json
import logging
from collections.abc import AsyncIterator, Callable
from datetime import UTC, datetime
from email.utils import parsedate_to_datetime
from typing import Any
from urllib.parse import quote

import openai
from azure.ai.projects.aio import AIProjectClient
from azure.ai.projects.models import VersionRefIndicator
from azure.core.exceptions import (
    AzureError,
    ClientAuthenticationError,
    HttpResponseError,
    ResourceNotFoundError,
    ServiceRequestError,
)
from azure.core.rest import HttpRequest
from azure.identity.aio import DefaultAzureCredential

from hosted_agent_kit.domain.enums import AgentProtocol, FoundrySessionStatus
from hosted_agent_kit.domain.errors import (
    QUOTA_REGIONAL,
    QUOTA_SESSION,
    FoundryConflict,
    FoundryError,
    FoundryQuotaExceeded,
    FoundryRejected,
    FoundryResponseTooLarge,
    FoundrySessionFailed,
    FoundrySessionNotFound,
    FoundryThrottled,
    FoundryTimeout,
    FoundryUnavailable,
)
from hosted_agent_kit.domain.models import AgentSummary, FoundrySession, InvokeContext
from hosted_agent_kit.ports.foundry import UpstreamResponse

logger = logging.getLogger(__name__)

SESSION_ID_FIELD = "agent_session_id"
LIST_PAGE_SIZE = 100
API_VERSION = "v1"
ISOLATION_HEADER = "x-ms-user-isolation-key"
IDENTITY_HEADER = "x-ms-user-identity"
SSE_MEDIA_TYPE = "text/event-stream"


_QUOTA_CODES = {
    "regional_session_quota_exceeded": QUOTA_REGIONAL,
    "session_quota_exceeded": QUOTA_SESSION,
}


def _quota_scope(code: str | None) -> str | None:
    return _QUOTA_CODES.get(code.lower()) if code else None


def _error_code(exc: BaseException) -> str | None:
    """The service's error code from an SDK exception, wherever the SDK put it."""
    candidates: list[Any] = []
    error = getattr(exc, "error", None)  # azure.core: the parsed ODataV4 error
    candidates.append(getattr(error, "code", None))
    candidates.append(getattr(exc, "code", None))  # openai: the error code
    body = getattr(exc, "body", None)  # openai: the parsed body
    if isinstance(body, dict):
        inner = body.get("error")
        candidates.append(inner.get("code") if isinstance(inner, dict) else body.get("code"))
    for candidate in candidates:
        if isinstance(candidate, str) and candidate:
            return candidate
    return None


def _from_status(
    status: int, retry_after: str | None = None, code: str | None = None
) -> FoundryError:
    if status == 429:
        scope = _quota_scope(code)
        if scope is not None:
            return FoundryQuotaExceeded(scope, _parse_retry_after(retry_after))
    if status == 404:
        return FoundrySessionNotFound()
    if status == 409:
        return FoundryConflict()
    if status == 429:
        return FoundryThrottled(_parse_retry_after(retry_after))
    if status in (408, 504):
        return FoundryTimeout()
    if status >= 500:
        return FoundryUnavailable(f"upstream returned {status}")
    return FoundryRejected(status)


def _parse_retry_after(value: str | None) -> float | None:
    if value is None:
        return None
    try:
        seconds = float(value)
    except ValueError:
        return _retry_after_from_date(value)
    return seconds if seconds >= 0 else None


def _retry_after_from_date(value: str) -> float | None:
    """``Retry-After`` may also be an HTTP-date. Return the seconds until then."""
    try:
        when = parsedate_to_datetime(value)
    except (TypeError, ValueError):
        return None
    if when.tzinfo is None:
        when = when.replace(tzinfo=UTC)
    return max(0.0, (when - datetime.now(UTC)).total_seconds())


def translate_exception(exc: BaseException) -> FoundryError | None:
    """Map SDK and HTTP exceptions to adapter errors; ``None`` means "not ours"."""
    if isinstance(exc, FoundryError):
        return exc
    if isinstance(exc, ResourceNotFoundError):
        return FoundrySessionNotFound()
    if isinstance(exc, ClientAuthenticationError):
        return FoundryRejected(exc.status_code or 401)
    if isinstance(exc, HttpResponseError):
        response: Any = exc.response
        headers = response.headers if response is not None else {}
        return _from_status(exc.status_code or 500, headers.get("retry-after"), _error_code(exc))
    if isinstance(exc, ServiceRequestError | AzureError):
        return FoundryUnavailable(type(exc).__name__)
    if isinstance(exc, openai.APITimeoutError | TimeoutError):
        return FoundryTimeout()
    if isinstance(exc, openai.APIStatusError):
        return _from_status(
            exc.status_code, exc.response.headers.get("retry-after"), _error_code(exc)
        )
    if isinstance(exc, openai.APIConnectionError):
        return FoundryUnavailable(type(exc).__name__)
    return None


def _as_datetime(value: Any) -> datetime | None:
    if value is None:
        return None
    if isinstance(value, int | float):
        return datetime.fromtimestamp(value, UTC)
    if isinstance(value, datetime):
        return value if value.tzinfo else value.replace(tzinfo=UTC)
    return None


def _normalise_status(value: Any) -> FoundrySessionStatus:
    text = str(getattr(value, "value", value)).lower()
    try:
        return FoundrySessionStatus(text)
    except ValueError:
        return FoundrySessionStatus.UNKNOWN


def normalise_session(agent_name: str, resource: Any) -> FoundrySession:
    indicator = resource.get("version_indicator")
    version = getattr(indicator, "agent_version", None) if indicator is not None else None
    return FoundrySession(
        session_id=resource["agent_session_id"],
        agent_name=agent_name,
        agent_version=str(version) if version is not None else None,
        status=_normalise_status(resource["status"]),
        created_at=_as_datetime(resource["created_at"]) or datetime.now(UTC),
        last_accessed_at=_as_datetime(resource.get("last_accessed_at")),
        expires_at=_as_datetime(resource.get("expires_at")),
    )


def scrub_session_id(value: Any) -> Any:
    """Remove the Foundry session identifier from any JSON-like structure."""
    if isinstance(value, dict):
        return {k: scrub_session_id(v) for k, v in value.items() if k != SESSION_ID_FIELD}
    if isinstance(value, list):
        return [scrub_session_id(item) for item in value]
    return value


def sse_frame(event: str, data: Any) -> bytes:
    payload = json.dumps(data, separators=(",", ":"))
    return f"event: {event}\ndata: {payload}\n\n".encode()


# ``allow_preview`` is required for the agent-scoped OpenAI client. ``retry_status=0`` stops the
# SDK pipeline from retrying 429 and 5xx responses itself: the pool owns retry policy so that
# attempts are bounded, counted and logged in one place. Connection-level retries remain.
CLIENT_OPTIONS: dict[str, Any] = {"allow_preview": True, "retry_status": 0}


class SdkFoundryAdapter:
    def __init__(
        self,
        endpoint: str,
        *,
        client_factory: Callable[[], AIProjectClient] | None = None,
        isolation_key: str | None = None,
        max_response_bytes: int | None = None,
    ) -> None:
        self._endpoint = endpoint
        self._max_response_bytes = max_response_bytes
        # Needed only when the agent endpoint uses the Header authorisation scheme. In the default
        # Entra scheme the platform ignores it. One constant key keeps every session in one
        # partition, which is what pool-wide listing and reconciliation need.
        self._headers: dict[str, str] = {ISOLATION_HEADER: isolation_key} if isolation_key else {}
        self._client_factory = client_factory
        self._client: AIProjectClient | None = None
        self._credential: DefaultAzureCredential | None = None
        self._openai_clients: dict[str, openai.AsyncOpenAI] = {}

    async def start(self) -> None:
        if self._client_factory is not None:
            self._client = self._client_factory()
            return
        self._credential = DefaultAzureCredential()
        self._client = AIProjectClient(
            endpoint=self._endpoint, credential=self._credential, **CLIENT_OPTIONS
        )

    async def close(self) -> None:
        for client in self._openai_clients.values():
            await client.close()
        self._openai_clients.clear()
        if self._client is not None:
            await self._client.close()
            self._client = None
        if self._credential is not None:
            await self._credential.close()
            self._credential = None

    def _session_options(self) -> dict[str, Any]:
        return {"headers": dict(self._headers)} if self._headers else {}

    def _call_headers(self, context: InvokeContext) -> dict[str, str]:
        """Headers for one invocation: the constant key, replaced by a per-user key when set."""
        headers = dict(self._headers)
        if context.isolation_key is not None:
            headers[ISOLATION_HEADER] = context.isolation_key
        if context.acting_user is not None:
            headers[IDENTITY_HEADER] = context.acting_user
        return headers

    def _openai_options(self, context: InvokeContext) -> dict[str, Any]:
        headers = self._call_headers(context)
        return {"extra_headers": headers} if headers else {}

    @property
    def _project(self) -> AIProjectClient:
        if self._client is None:
            raise RuntimeError("adapter is not started")
        return self._client

    async def list_agents(self) -> AsyncIterator[AgentSummary]:
        try:
            async for agent in self._project.agents.list(kind="hosted", limit=LIST_PAGE_SIZE):
                yield AgentSummary(
                    name=agent.name, state=str(getattr(agent.state, "value", agent.state))
                )
        except Exception as exc:
            raise _raise_translated(exc) from exc

    async def list_sessions(self, agent_name: str) -> AsyncIterator[FoundrySession]:
        try:
            async for resource in self._project.agents.list_sessions(
                agent_name, limit=LIST_PAGE_SIZE, **self._session_options()
            ):
                yield normalise_session(agent_name, resource)
        except Exception as exc:
            raise _raise_translated(exc) from exc

    async def get_session(self, agent_name: str, session_id: str) -> FoundrySession:
        try:
            resource = await self._project.agents.get_session(
                agent_name, session_id, **self._session_options()
            )
        except Exception as exc:
            raise _raise_translated(exc) from exc
        return normalise_session(agent_name, resource)

    async def latest_agent_version(self, agent_name: str) -> str | None:
        try:
            details = await self._project.agents.get(agent_name)
        except Exception as exc:
            raise _raise_translated(exc) from exc
        version = details.versions.latest.version
        return str(version) if version is not None else None

    async def create_session(
        self, agent_name: str, session_id: str | None = None, agent_version: str | None = None
    ) -> FoundrySession:
        options = self._session_options()
        if session_id is not None:
            options[SESSION_ID_FIELD] = session_id
        try:
            version: Any = agent_version
            if version is None:
                details = await self._project.agents.get(agent_name)
                version = details.versions.latest.version
            resource = await self._project.agents.create_session(
                agent_name, version_indicator=VersionRefIndicator(agent_version=version), **options
            )
        except Exception as exc:
            raise _raise_translated(exc) from exc
        return normalise_session(agent_name, resource)

    async def stop_session(self, agent_name: str, session_id: str) -> None:
        try:
            await self._project.agents.stop_session(
                agent_name, session_id, **self._session_options()
            )
        except Exception as exc:
            raise _raise_translated(exc) from exc

    async def delete_session(self, agent_name: str, session_id: str) -> None:
        try:
            await self._project.agents.delete_session(
                agent_name, session_id, **self._session_options()
            )
        except Exception as exc:
            raise _raise_translated(exc) from exc

    def _openai_for(self, agent_name: str) -> openai.AsyncOpenAI:
        client = self._openai_clients.get(agent_name)
        if client is None:
            # max_retries=0: the pool owns retry policy, and invocations may not be idempotent.
            client = self._project.get_openai_client(agent_name=agent_name, max_retries=0)
            self._openai_clients[agent_name] = client
        return client

    async def invoke(self, context: InvokeContext) -> UpstreamResponse:
        if context.protocol is AgentProtocol.INVOCATIONS:
            return await self._invoke_invocations(context)
        client = self._openai_for(context.agent_name)
        body = {k: v for k, v in context.payload.items() if k != "stream"}
        body[SESSION_ID_FIELD] = context.session_id
        try:
            if context.stream:
                stream = await client.responses.create(
                    stream=True,
                    extra_body=body,
                    timeout=context.timeout_seconds,
                    **self._openai_options(context),
                )
                return UpstreamResponse(stream=_sse_frames(stream), media_type="text/event-stream")
            response = await client.responses.create(
                extra_body=body, timeout=context.timeout_seconds, **self._openai_options(context)
            )
        except Exception as exc:
            raise await self._classify_invoke_error(context, exc) from exc
        return UpstreamResponse(
            body=scrub_session_id(response.model_dump(mode="json", exclude_none=True))
        )

    async def _invoke_invocations(self, context: InvokeContext) -> UpstreamResponse:
        """POST the payload to the Invocations endpoint. The session is a query parameter."""
        headers = self._call_headers(context)
        body: dict[str, Any] = {"json": context.payload}
        if context.raw_body is not None:
            # Any content type: JSON of any shape, text, multipart or binary, sent as received.
            body = {"content": context.raw_body}
            headers["Content-Type"] = context.content_type or "application/octet-stream"
        request = HttpRequest(
            "POST",
            f"/agents/{quote(context.agent_name, safe='')}/endpoint/protocols/invocations",
            params={"api-version": API_VERSION, SESSION_ID_FIELD: context.session_id},
            headers=headers,
            **body,
        )
        try:
            async with asyncio.timeout(context.timeout_seconds):
                response = await self._project.send_request(request, stream=True)
                if response.status_code >= 400:
                    await response.read()
                    await response.close()
                    raise HttpResponseError(response=response)
                content_type = response.headers.get("content-type", "")
                if _media_type(content_type) == SSE_MEDIA_TYPE:
                    return UpstreamResponse(
                        status_code=response.status_code,
                        stream=_raw_stream(response),
                        media_type=content_type,
                    )
                try:
                    raw = await _read_capped(response, self._max_response_bytes)
                finally:
                    await response.close()
        except Exception as exc:
            raise await self._classify_invoke_error(context, exc) from exc
        return UpstreamResponse(
            status_code=response.status_code,
            raw=_scrub_json(raw, content_type),
            media_type=content_type or "application/octet-stream",
        )

    async def _classify_invoke_error(self, context: InvokeContext, exc: Exception) -> Exception:
        translated = translate_exception(exc)
        if translated is None:
            return exc
        needs_check = isinstance(translated, FoundrySessionNotFound) or (
            isinstance(translated, FoundryUnavailable)
            and (
                isinstance(exc, openai.InternalServerError)
                or (isinstance(exc, HttpResponseError) and exc.status_code == 500)
            )
        )
        if not needs_check:
            return translated
        try:
            session = await self.get_session(context.agent_name, context.session_id)
        except FoundrySessionNotFound:
            return FoundrySessionNotFound()
        except FoundryError:
            return translated
        if session.status is FoundrySessionStatus.FAILED:
            return FoundrySessionFailed()
        if isinstance(translated, FoundrySessionNotFound):
            return FoundryRejected(404)
        return translated


def _raise_translated(exc: Exception) -> Exception:
    translated = translate_exception(exc)
    return translated if translated is not None else exc


async def _sse_frames(stream: Any) -> AsyncIterator[bytes]:
    try:
        async for event in stream:
            data = scrub_session_id(event.model_dump(mode="json", exclude_none=True))
            yield sse_frame(str(data.get("type", "message")), data)
    except Exception as exc:
        raise _raise_translated(exc) from exc
    finally:
        await _close_quietly(stream.close)


def _media_type(content_type: str) -> str:
    return content_type.split(";", 1)[0].strip().lower()


def _scrub_json(raw: bytes, content_type: str) -> bytes:
    """Remove the session id from a JSON body. Other content is returned unchanged."""
    media = _media_type(content_type)
    if media != "application/json" and not media.endswith("+json"):
        return raw
    try:
        data = json.loads(raw)
    except ValueError:
        return raw
    return json.dumps(scrub_session_id(data), separators=(",", ":")).encode()


async def _raw_stream(response: Any) -> AsyncIterator[bytes]:
    try:
        async for chunk in response.iter_bytes():
            yield chunk
    except Exception as exc:
        raise _raise_translated(exc) from exc
    finally:
        await _close_quietly(response.close)


async def _close_quietly(close: Callable[[], Any]) -> None:
    """Close an upstream resource. A failing close must not hide the stream's own outcome."""
    try:
        await close()
    except Exception:
        logger.warning("upstream_close_failed", exc_info=True)


async def _read_capped(response: Any, limit: int | None) -> bytes:
    """Read a response body, refusing one larger than ``limit`` bytes."""
    if limit is None:
        return bytes(await response.read())
    declared = response.headers.get("content-length")
    if declared is not None and declared.isdigit() and int(declared) > limit:
        raise FoundryResponseTooLarge()
    chunks: list[bytes] = []
    size = 0
    async for chunk in response.iter_bytes():
        size += len(chunk)
        if size > limit:
            raise FoundryResponseTooLarge()
        chunks.append(chunk)
    return b"".join(chunks)
