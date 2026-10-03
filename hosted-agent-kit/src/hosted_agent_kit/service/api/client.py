"""Client API: Responses, Invocations, chat and discovery. No response carries a session id."""

from __future__ import annotations

import base64
import json
import logging
import re
import uuid
from collections.abc import Awaitable, Callable, Mapping
from typing import Annotated, Any

import anyio
from fastapi import APIRouter, Header, Path, Query, Request
from fastapi.responses import JSONResponse, Response, StreamingResponse
from starlette.types import Receive, Scope, Send

from hosted_agent_kit.config.models import AGENT_NAME_PATTERN, AgentConfig
from hosted_agent_kit.domain.enums import AgentProtocol
from hosted_agent_kit.domain.errors import (
    ClientDisconnectedError,
    IdempotencyInProgressError,
    IdempotencyKeyReusedError,
    InsufficientScopeError,
    SubjectRequiredError,
    ValidationFailedError,
)
from hosted_agent_kit.logging_config import correlation_id_var, log_event
from hosted_agent_kit.service.api.deps import ContainerDep, InvokePrincipal
from hosted_agent_kit.service.api.disconnect import run_until_disconnect
from hosted_agent_kit.service.api.schemas import (
    CONVERSATION_KEY_PATTERN,
    AgentCapabilities,
    ChatRequest,
    InvocationEnvelope,
    InvokeRequest,
    InvokeResponse,
    Limits,
    ProblemDetails,
    ResponsesRequest,
)
from hosted_agent_kit.service.container import Container
from hosted_agent_kit.service.security.claims import Principal, is_valid_user_id
from hosted_agent_kit.services.idempotency import Outcome, StoredResponse, fingerprint
from hosted_agent_kit.services.pool import PoolRequest, PoolResult

logger = logging.getLogger(__name__)
router = APIRouter(prefix="/v1/agents", tags=["agents"])

AgentName = Annotated[
    str, Path(pattern=AGENT_NAME_PATTERN.pattern, max_length=128, description="Configured agent")
]
IdempotencyKey = Annotated[
    str | None,
    Header(alias="Idempotency-Key", max_length=255, pattern=r"^[\x21-\x7e]+$"),
]

# The success body depends on the agent's protocol and on whether the caller asked for a stream.
ConversationHeader = Annotated[
    str | None,
    Header(
        alias="X-Conversation-Key",
        max_length=128,
        pattern=CONVERSATION_KEY_PATTERN,
        description="One of the caller's conversations with a stateful agent. Own session.",
    ),
]
SubjectHeader = Annotated[
    str | None,
    Header(
        alias="X-Pool-Subject",
        max_length=80,
        pattern=r"^[A-Za-z0-9._:-]{1,80}$",
        description=(
            "App-only callers: the end user this call is for, as an opaque id such as their "
            "object id. Requires the delegate role. Sessions are scoped to the caller and subject."
        ),
    ),
]
_SUBJECT_PATTERN = re.compile(r"^[A-Za-z0-9._:-]{1,80}$")

_SUCCESS: dict[int | str, dict[str, Any]] = {
    200: {
        "description": (
            "Responses agents: an `InvokeResponse`, or `text/event-stream` when the request "
            "sets `input.stream` (frames are Responses events; a failure after the stream has "
            "started is a final `event: error` frame with `error_code`, `title`, `detail`, "
            "`correlation_id` and, where relevant, `retry_after_seconds`). Invocations agents: "
            "the agent's own response with its own status code, content type and body, which "
            "may be any media type."
        ),
        "content": {
            "text/event-stream": {"schema": {"type": "string"}},
            "application/octet-stream": {"schema": {"type": "string", "format": "binary"}},
        },
    }
}
_ERRORS: dict[int | str, dict[str, Any]] = {
    code: {"model": ProblemDetails}
    for code in (401, 403, 404, 409, 413, 422, 429, 500, 502, 503, 504)
}


def _resolve_user(principal: Principal, supplied: str | None, container: Container) -> str:
    """The authenticated identity is authoritative. A body user_id exists for local development."""
    if supplied is None:
        return principal.user_id
    if container.settings.auth_mode != "development":
        raise ValidationFailedError("user_id must not be supplied; it is taken from the token.")
    if not is_valid_user_id(supplied):
        raise ValidationFailedError("user_id has an invalid format.")
    return supplied


def _resolve_identity(
    principal: Principal,
    supplied: str | None,
    container: Container,
    cfg: AgentConfig,
    subject: str | None,
) -> str:
    """Who the session belongs to.

    An app-only token names a service, not a person. For a stateful agent the caller must name
    the end user in ``X-Pool-Subject`` (which needs the delegate role), or every user behind the
    same service would share one session. ``POOL_APP_ONLY_POLICY=allow`` keeps the old behaviour.
    """
    user = _resolve_user(principal, supplied, container)
    settings = container.settings
    if subject is not None:
        if not principal.app_only:
            raise ValidationFailedError("X-Pool-Subject is only for app-only callers.")
        if not principal.has(settings.delegate_role):
            raise InsufficientScopeError(
                "The caller lacks the delegate role needed to act for an end user."
            )
        if _SUBJECT_PATTERN.match(subject) is None or not is_valid_user_id(f"{user}:{subject}"):
            raise ValidationFailedError("X-Pool-Subject has an invalid format.")
        return f"{user}:{subject}"
    if principal.app_only and cfg.stateful and settings.app_only_policy == "reject":
        raise SubjectRequiredError(
            "An app-only caller must name the end user in X-Pool-Subject for a stateful agent."
        )
    return user


def _conversation(cfg: AgentConfig, body_key: str | None, header_key: str | None) -> str | None:
    if body_key is not None and header_key is not None and body_key != header_key:
        raise ValidationFailedError("conversation_key and X-Conversation-Key differ.")
    key = body_key if body_key is not None else header_key
    if key is not None and not cfg.stateful:
        raise ValidationFailedError("conversation_key only applies to stateful agents.")
    return key


def _timeout(requested: float | None, container: Container) -> float:
    settings = container.settings
    return min(requested or settings.default_timeout_seconds, settings.max_timeout_seconds)


class _BoundedStreamingResponse(StreamingResponse):
    """A streaming response that cannot outlive the stream limit, even for a stalled client.

    The pool enforces the limit while it waits for the next upstream frame. A client that stops
    reading blocks the send instead, so the limit is also enforced here, with a short grace
    period so the in-band ``STREAM_TIMEOUT`` event normally arrives first.
    """

    def __init__(
        self,
        content: Any,
        *,
        limit_seconds: float,
        on_end: Callable[[], Awaitable[None]],
        media_type: str | None,
        headers: Mapping[str, str],
    ) -> None:
        super().__init__(content, media_type=media_type, headers=dict(headers))
        self._limit_seconds = limit_seconds
        self._on_end = on_end

    async def __call__(self, scope: Scope, receive: Receive, send: Send) -> None:
        try:
            with anyio.move_on_after(self._limit_seconds) as limit:
                await super().__call__(scope, receive, send)
            if limit.cancelled_caught:
                log_event(logger, "stream_limit_exceeded_by_client")
        finally:
            with anyio.CancelScope(shield=True):
                await self._on_end()


_STREAM_GRACE_SECONDS = 5.0


def _to_response(
    result: PoolResult,
    principal: Principal,
    container: Container,
    *,
    envelope: bool = False,
) -> Response:
    headers: dict[str, str] = {}
    settings = container.settings
    if settings.diagnostic_session_id_header and principal.has(settings.diagnostics_role):
        headers["X-Pool-Session-Id"] = result.session_id
    if result.raw is not None:
        if envelope:
            return JSONResponse(_envelope(result).model_dump(), status_code=200, headers=headers)
        return Response(
            content=result.raw,
            status_code=result.status_code,
            media_type=result.media_type,
            headers=headers,
        )
    if result.stream is not None:
        headers.update({"Cache-Control": "no-cache", "X-Accel-Buffering": "no"})
        return _BoundedStreamingResponse(
            result.stream,
            limit_seconds=settings.max_stream_seconds + _STREAM_GRACE_SECONDS,
            on_end=result.close,
            media_type=result.media_type,
            headers=headers,
        )
    return JSONResponse(
        {"request_id": result.request_id, "result": result.body or {}},
        status_code=result.status_code,
        headers=headers,
    )


def _envelope(result: PoolResult) -> InvocationEnvelope:
    raw = result.raw or b""
    media = result.media_type.split(";", 1)[0].strip().lower()
    envelope = InvocationEnvelope(
        request_id=result.request_id, status_code=result.status_code, content_type=result.media_type
    )
    if media == "application/json" or media.endswith("+json"):
        try:
            envelope.body_json = json.loads(raw)
            return envelope
        except ValueError:
            pass
    if media.startswith("text/"):
        try:
            envelope.body_text = raw.decode()
            return envelope
        except UnicodeDecodeError:
            pass
    envelope.body_base64 = base64.b64encode(raw).decode()
    return envelope


def _canonical(payload: Any) -> str:
    return json.dumps(payload, sort_keys=True, separators=(",", ":"), default=str)


async def _execute(
    request: Request,
    container: Container,
    principal: Principal,
    *,
    agent_name: str,
    payload: Any,
    stream: bool,
    timeout_seconds: float,
    user_id: str,
    idempotency_key: str | None,
    conversation_key: str | None = None,
    raw_body: bytes | None = None,
    content_type: str | None = None,
    envelope: bool = False,
    route: str = "invoke",
) -> Response:
    cfg = container.pool.agent_config(agent_name)  # 404 before any other work
    store = container.idempotency
    scope: str | None = None
    if idempotency_key and store is not None and store.enabled and not stream:
        scope = f"{user_id}\x00{cfg.name}\x00{idempotency_key}"
        outcome, stored = store.begin(
            scope,
            fingerprint(
                route,
                raw_body if raw_body is not None else _canonical(payload),
                content_type or "",
                conversation_key or "",
                "envelope" if envelope else "",
            ),
        )
        if outcome is Outcome.MISMATCH:
            scope = None  # the key belongs to the earlier request
            raise IdempotencyKeyReusedError(
                "This Idempotency-Key was already used with a different request."
            )
        if outcome is Outcome.IN_PROGRESS:
            scope = None
            raise IdempotencyInProgressError(
                "A request with this Idempotency-Key is still running.",
                retry_after_seconds=container.settings.retry_after_seconds,
            )
        if outcome is Outcome.REPLAY and stored is not None:
            return Response(
                content=stored.body,
                status_code=stored.status_code,
                media_type=stored.media_type,
                headers={"Idempotent-Replayed": "true"},
            )
    pool_request = PoolRequest(
        agent_name=agent_name,
        user_id=user_id,
        payload=payload,
        timeout_seconds=timeout_seconds,
        request_id=request.state.request_id,
        correlation_id=correlation_id_var.get() or uuid.uuid4().hex,
        idempotency_key=idempotency_key,
        stream=stream,
        conversation_key=conversation_key,
        raw_body=raw_body,
        content_type=content_type,
    )
    try:
        result = await run_until_disconnect(request, container.pool.execute(pool_request))
    except ClientDisconnectedError:
        if scope is not None and store is not None:
            store.abandon(scope)
        log_event(logger, "client_disconnected", agent_name=agent_name)
        return Response(status_code=499)
    except BaseException:
        if scope is not None and store is not None:
            store.abandon(scope)
        raise
    response = _to_response(result, principal, container, envelope=envelope)
    if scope is not None and store is not None:
        if result.stream is None:
            store.complete(
                scope,
                StoredResponse(
                    response.status_code,
                    response.media_type or "application/json",
                    bytes(response.body),
                ),
            )
        else:
            store.abandon(scope)  # a stream cannot be replayed
    return response


def _capabilities(cfg: AgentConfig, container: Container) -> AgentCapabilities:
    s = container.settings
    invocations = cfg.protocol is AgentProtocol.INVOCATIONS
    base = f"/v1/agents/{cfg.name}"
    store = container.idempotency
    return AgentCapabilities(
        name=cfg.name,
        protocol=cfg.protocol.value,
        stateful=cfg.stateful,
        streaming="agent" if invocations else "request",
        supports_conversation_key=cfg.stateful,
        endpoints=[f"{base}/invocations"] if invocations else [f"{base}/responses", f"{base}/chat"],
        request_content_types=["*/*"] if invocations else ["application/json"],
        queue_enabled=cfg.queue.enabled,
        queue_max_wait_seconds=cfg.queue.max_wait_seconds if cfg.queue.enabled else None,
        idempotency_ttl_seconds=store.ttl_seconds if store is not None and store.enabled else 0,
        agent_version=cfg.agent_version,
        limits=Limits(
            max_body_bytes=s.max_body_bytes,
            default_timeout_seconds=s.default_timeout_seconds,
            max_timeout_seconds=s.max_timeout_seconds,
            max_stream_seconds=s.max_stream_seconds,
            max_request_seconds=s.max_request_seconds,
            max_response_bytes=s.max_response_bytes if invocations else None,
        ),
    )


@router.get(
    "",
    response_model=list[AgentCapabilities],
    responses=_ERRORS,
    summary="List the agents a caller can use",
)
async def list_agents(_: InvokePrincipal, container: ContainerDep) -> list[AgentCapabilities]:
    return [
        _capabilities(container.pool.agent_config(n), container) for n in container.pool.agent_names
    ]


@router.get(
    "/{agent_name}",
    response_model=AgentCapabilities,
    responses=_ERRORS,
    summary="Describe one agent: protocol, streaming, limits and endpoints",
)
async def describe_agent(
    agent_name: AgentName, _: InvokePrincipal, container: ContainerDep
) -> AgentCapabilities:
    return _capabilities(container.pool.agent_config(agent_name), container)


def _require_protocol(cfg: AgentConfig, expected: AgentProtocol) -> None:
    if cfg.protocol is not expected:
        raise ValidationFailedError(
            f"This agent uses the {cfg.protocol.value} protocol. "
            f"Use /v1/agents/{cfg.name}/{cfg.protocol.value} instead."
        )


@router.post(
    "/{agent_name}/responses",
    response_model=InvokeResponse,
    responses={**_SUCCESS, **_ERRORS},
    summary="Call a Responses-protocol agent",
    description=(
        "`input` is the Responses payload. Returns an `InvokeResponse`, or server-sent events when "
        "`stream` is true."
    ),
)
async def responses(
    agent_name: AgentName,
    body: ResponsesRequest,
    request: Request,
    principal: InvokePrincipal,
    container: ContainerDep,
    idempotency_key: IdempotencyKey = None,
    subject: SubjectHeader = None,
    conversation_header: ConversationHeader = None,
) -> Response:
    cfg = container.pool.agent_config(agent_name)
    _require_protocol(cfg, AgentProtocol.RESPONSES)
    return await _execute(
        request,
        container,
        principal,
        agent_name=agent_name,
        payload=body.input,
        stream=body.stream,
        timeout_seconds=_timeout(body.timeout_seconds, container),
        user_id=_resolve_identity(principal, body.user_id, container, cfg, subject),
        idempotency_key=idempotency_key,
        conversation_key=_conversation(cfg, body.conversation_key, conversation_header),
        route="responses",
    )


@router.post(
    "/{agent_name}/invocations",
    response_model=None,
    responses={
        200: {
            "model": InvocationEnvelope,
            "description": (
                "The agent's own status code, content type and body (any media type), or an "
                "`InvocationEnvelope` when `envelope=true`. A `text/event-stream` response is "
                "relayed as a stream."
            ),
            "content": {
                "*/*": {"schema": {"type": "string", "format": "binary"}},
                "text/event-stream": {"schema": {"type": "string"}},
            },
        },
        **_ERRORS,
    },
    openapi_extra={
        "requestBody": {
            "required": False,
            "description": "Sent to the agent as received, with its content type.",
            "content": {"*/*": {"schema": {"type": "string", "format": "binary"}}},
        }
    },
    summary="Call an Invocations-protocol agent with any request body",
)
async def invocations(
    agent_name: AgentName,
    request: Request,
    principal: InvokePrincipal,
    container: ContainerDep,
    timeout_seconds: Annotated[float | None, Query(gt=0)] = None,
    envelope: Annotated[
        bool, Query(description="Wrap the response in an InvocationEnvelope (JSON).")
    ] = False,
    idempotency_key: IdempotencyKey = None,
    subject: SubjectHeader = None,
    conversation_header: ConversationHeader = None,
) -> Response:
    cfg = container.pool.agent_config(agent_name)
    _require_protocol(cfg, AgentProtocol.INVOCATIONS)
    body = await request.body()
    return await _execute(
        request,
        container,
        principal,
        agent_name=agent_name,
        payload=None,
        stream=False,  # the agent decides whether to stream
        timeout_seconds=_timeout(timeout_seconds, container),
        user_id=_resolve_identity(principal, None, container, cfg, subject),
        idempotency_key=idempotency_key,
        conversation_key=_conversation(cfg, None, conversation_header),
        raw_body=body,
        content_type=request.headers.get("content-type"),
        envelope=envelope,
        route="invocations",
    )


@router.post(
    "/{agent_name}/invoke",
    response_model=InvokeResponse,
    responses={**_SUCCESS, **_ERRORS},
    deprecated=True,
    summary="Invoke an agent (deprecated: use /responses or /invocations)",
    description=(
        "Kept for existing callers. The response shape depends on the agent's protocol, which "
        "generated clients cannot model. Use `/responses` for Responses agents and "
        "`/invocations` for Invocations agents."
    ),
)
async def invoke(
    agent_name: AgentName,
    body: InvokeRequest,
    request: Request,
    principal: InvokePrincipal,
    container: ContainerDep,
    idempotency_key: IdempotencyKey = None,
    subject: SubjectHeader = None,
    conversation_header: ConversationHeader = None,
) -> Response:
    cfg = container.pool.agent_config(agent_name)
    invocations_agent = cfg.protocol is AgentProtocol.INVOCATIONS
    response = await _execute(
        request,
        container,
        principal,
        agent_name=agent_name,
        payload=body.input,
        # An Invocations agent decides for itself whether to stream. The stream flag is a
        # Responses convention and is passed through to the agent unchanged.
        stream=bool(body.input.get("stream")) and not invocations_agent,
        timeout_seconds=_timeout(body.timeout_seconds, container),
        user_id=_resolve_identity(principal, body.user_id, container, cfg, subject),
        idempotency_key=idempotency_key,
        conversation_key=_conversation(cfg, body.conversation_key, conversation_header),
        route="invoke",
    )
    successor = "invocations" if invocations_agent else "responses"
    response.headers["Deprecation"] = "true"
    response.headers["Link"] = f'</v1/agents/{agent_name}/{successor}>; rel="successor-version"'
    return response


@router.post(
    "/{agent_name}/chat",
    response_model=InvokeResponse,
    responses={**_SUCCESS, **_ERRORS},
    summary="Chat with a Responses-protocol agent",
)
async def chat(
    agent_name: AgentName,
    body: ChatRequest,
    request: Request,
    principal: InvokePrincipal,
    container: ContainerDep,
    idempotency_key: IdempotencyKey = None,
    subject: SubjectHeader = None,
    conversation_header: ConversationHeader = None,
) -> Response:
    cfg = container.pool.agent_config(agent_name)
    user_id = _resolve_identity(principal, body.user_id, container, cfg, subject)
    if cfg.protocol is AgentProtocol.INVOCATIONS:
        raise ValidationFailedError(
            "This agent uses the Invocations protocol. "
            "Send its request body to the invocations endpoint."
        )
    payload: dict[str, Any] = {"input": body.message, "metadata": body.metadata}
    if body.previous_response_id:
        payload["previous_response_id"] = body.previous_response_id
    if body.conversation_id:
        payload["conversation"] = body.conversation_id
    return await _execute(
        request,
        container,
        principal,
        agent_name=agent_name,
        payload=payload,
        stream=body.stream,
        timeout_seconds=_timeout(body.timeout_seconds, container),
        user_id=user_id,
        idempotency_key=idempotency_key,
        conversation_key=_conversation(cfg, body.conversation_key, conversation_header),
        route="chat",
    )
