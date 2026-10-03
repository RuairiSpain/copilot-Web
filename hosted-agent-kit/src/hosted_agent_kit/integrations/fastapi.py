"""FastAPI helpers: lifecycle, error responses, streaming and ready-made routers.

Typical use::

    kit = Hack.from_yaml("scheduler.yaml")
    app = FastAPI()
    install(app, kit)                     # start/stop with the app, RFC 7807 errors

    @app.post("/ask")
    async def ask(body: Ask, kit: KitDep):
        return (await kit.ask("support-bot", body.text, user_id=body.user)).json()
"""

from __future__ import annotations

import logging
from collections.abc import AsyncIterator, Mapping, Sequence
from contextlib import AsyncExitStack, asynccontextmanager
from typing import Annotated, Any

import anyio
from fastapi import APIRouter, Depends, FastAPI, Path, Query, Request
from fastapi.responses import JSONResponse, PlainTextResponse, Response, StreamingResponse
from starlette.types import Receive, Scope, Send

from hosted_agent_kit.administration import Administration
from hosted_agent_kit.config.models import AGENT_NAME_PATTERN
from hosted_agent_kit.kit import Hack
from hosted_agent_kit.reporting import Reporting
from hosted_agent_kit.results import AgentResult
from hosted_agent_kit.service.api.errors import install_error_handlers
from hosted_agent_kit.views import (
    AgentAdminView,
    DeleteAccepted,
    EventView,
    PoolResourceView,
    QuotaView,
    ReloadResult,
    SessionAdminView,
    SyncReportView,
)

logger = logging.getLogger(__name__)

__all__ = [
    "KitDep",
    "admin_router",
    "get_kit",
    "install",
    "install_error_handlers",
    "reporting_router",
    "response_for",
    "sse_response",
]

_STATE_KEY = "hack"


def install(app: FastAPI, kit: Hack, *, error_handlers: bool = True) -> Hack:
    """Tie ``kit`` to ``app``: start it at startup, stop it at shutdown, make it injectable.

    The kit starts before the app's own startup code runs, so your lifespan can already call
    it. ``error_handlers=True`` answers kit errors (unknown agent, queue full, timeout, ...)
    with RFC 7807 ``application/problem+json`` and the right HTTP status.
    """
    app.state.hack = kit
    if error_handlers:
        install_error_handlers(app)
    inner = app.router.lifespan_context

    @asynccontextmanager
    async def lifespan(scope_app: Any) -> AsyncIterator[Any]:
        async with AsyncExitStack() as stack:
            await stack.enter_async_context(kit)
            state = await stack.enter_async_context(inner(scope_app))
            yield state

    app.router.lifespan_context = lifespan
    return kit


def get_kit(request: Request) -> Hack:
    """FastAPI dependency returning the kit that ``install`` attached."""
    kit: Hack | None = getattr(request.app.state, _STATE_KEY, None)
    if kit is None:
        raise RuntimeError("no Hack on this app: call install(app, kit)")
    return kit


KitDep = Annotated[Hack, Depends(get_kit)]


class _ClosingStream(StreamingResponse):
    """A streaming response that always returns the session to the pool when it ends."""

    def __init__(self, result: AgentResult, headers: Mapping[str, str] | None) -> None:
        super().__init__(result.chunks(), media_type=result.media_type, headers=dict(headers or {}))
        self._result = result

    async def __call__(self, scope: Scope, receive: Receive, send: Send) -> None:
        try:
            await super().__call__(scope, receive, send)
        finally:
            with anyio.CancelScope(shield=True):
                await self._result.aclose()


def sse_response(result: AgentResult, *, headers: Mapping[str, str] | None = None) -> Response:
    """Send a streamed result to your caller as server-sent events."""
    merged = {"Cache-Control": "no-cache", "X-Accel-Buffering": "no", **(headers or {})}
    return _ClosingStream(result, merged)


def response_for(result: AgentResult, *, headers: Mapping[str, str] | None = None) -> Response:
    """Relay a result as it is: a stream as SSE, anything else with the agent's status and type."""
    if result.is_stream:
        return sse_response(result, headers=headers)
    return Response(
        content=result.content,
        status_code=result.status_code,
        media_type=result.media_type,
        headers=dict(headers or {}),
    )


def _reporting(kit: KitDep) -> Reporting:
    return kit.reporting


def _administration(kit: KitDep) -> Administration:
    return kit.admin


ReportingDep = Annotated[Reporting, Depends(_reporting)]
AdminDep = Annotated[Administration, Depends(_administration)]

AgentName = Annotated[str, Path(pattern=AGENT_NAME_PATTERN.pattern, max_length=128)]
SessionId = Annotated[str, Path(pattern=r"^[A-Za-z0-9._:-]{1,256}$")]


def _guarded_router(
    tags: Sequence[str], dependencies: Sequence[Any] | None, allow_unauthenticated: bool, what: str
) -> APIRouter:
    if not dependencies and not allow_unauthenticated:
        raise ValueError(
            f"{what} exposes pool internals. Pass dependencies=[Depends(...)] that check the "
            "caller, or allow_unauthenticated=True if something else in front of it does."
        )
    return APIRouter(tags=list(tags), dependencies=list(dependencies or []))


def reporting_router(
    *,
    dependencies: Sequence[Any] | None = None,
    allow_unauthenticated: bool = False,
    reveal_identities: bool = False,
) -> APIRouter:
    """Read-only endpoints. ``dependencies`` must check the caller, or say there is no check.

    User ids and conversation keys are hashed unless ``reveal_identities`` is True.
    """
    router = _guarded_router(
        ["hack-reporting"], dependencies, allow_unauthenticated, "reporting_router()"
    )

    @router.get("/agents", response_model=list[AgentAdminView])
    async def agents(rep: ReportingDep) -> list[AgentAdminView]:
        return await rep.agents()

    @router.get("/agents/{agent_name}", response_model=PoolResourceView)
    async def agent(agent_name: AgentName, rep: ReportingDep) -> PoolResourceView:
        return rep.pool(agent_name)

    @router.get("/agents/{agent_name}/sessions", response_model=list[SessionAdminView])
    async def sessions(agent_name: AgentName, rep: ReportingDep) -> list[SessionAdminView]:
        return await rep.sessions(agent_name, reveal_identities=reveal_identities)

    @router.get("/events", response_model=list[EventView])
    async def events(
        rep: ReportingDep,
        agent_name: Annotated[str | None, Query(pattern=AGENT_NAME_PATTERN.pattern)] = None,
        limit: Annotated[int, Query(ge=1, le=500)] = 100,
    ) -> list[EventView]:
        return rep.events(agent_name, limit=limit)

    @router.get("/quota", response_model=QuotaView)
    async def quota(rep: ReportingDep) -> QuotaView:
        return await rep.quota()

    @router.get("/metrics")
    async def metrics(rep: ReportingDep) -> dict[str, Any]:
        return rep.metrics()

    @router.get("/metrics/prometheus", response_class=PlainTextResponse)
    async def prometheus(rep: ReportingDep) -> PlainTextResponse:
        return PlainTextResponse(
            rep.prometheus(), media_type="text/plain; version=0.0.4; charset=utf-8"
        )

    @router.get("/health")
    async def health(rep: ReportingDep) -> JSONResponse:
        body = rep.health()
        return JSONResponse(body, status_code=200 if body["status"] == "ready" else 503)

    return router


def admin_router(
    *, dependencies: Sequence[Any] | None = None, allow_unauthenticated: bool = False
) -> APIRouter:
    """Endpoints that change the pool. ``dependencies`` must check that the caller is an admin."""
    router = _guarded_router(["hack-admin"], dependencies, allow_unauthenticated, "admin_router()")

    @router.delete(
        "/agents/{agent_name}/sessions/{session_id}",
        status_code=204,
        responses={202: {"model": DeleteAccepted}},
    )
    async def delete_session(
        agent_name: AgentName, session_id: SessionId, admin: AdminDep
    ) -> Response:
        if await admin.delete_session(agent_name, session_id):
            return Response(status_code=204)
        return JSONResponse(DeleteAccepted().model_dump(), status_code=202)

    @router.post("/agents/{agent_name}/sync", response_model=SyncReportView)
    async def sync(agent_name: AgentName, admin: AdminDep) -> SyncReportView:
        return await admin.sync(agent_name)

    @router.post("/agents/{agent_name}/warm")
    async def warm(agent_name: AgentName, admin: AdminDep) -> dict[str, bool]:
        return {"created": await admin.provision_warm(agent_name)}

    @router.post("/config/reload", response_model=ReloadResult)
    async def reload_config(admin: AdminDep) -> ReloadResult:
        return admin.reload_config()

    return router
