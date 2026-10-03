"""Administration and operations API. Requires the admin role; user ids are redacted."""

from __future__ import annotations

from typing import Annotated, Any

from fastapi import APIRouter, Path, Query
from fastapi.responses import JSONResponse, PlainTextResponse, Response

from hosted_agent_kit.administration import Administration
from hosted_agent_kit.config.models import AGENT_NAME_PATTERN
from hosted_agent_kit.reporting import Reporting
from hosted_agent_kit.service.api.deps import AdminPrincipal, ContainerDep
from hosted_agent_kit.service.api.schemas import ProblemDetails
from hosted_agent_kit.service.container import Container
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

router = APIRouter(prefix="/v1/admin", tags=["admin"])
metrics_router = APIRouter(tags=["admin"])

AgentName = Annotated[str, Path(pattern=AGENT_NAME_PATTERN.pattern, max_length=128)]
SessionId = Annotated[str, Path(pattern=r"^[A-Za-z0-9._:-]{1,256}$")]

_ERRORS: dict[int | str, dict[str, Any]] = {
    code: {"model": ProblemDetails} for code in (401, 403, 404, 409, 422, 500, 502)
}


def _reporting(container: Container) -> Reporting:
    return Reporting(container.runtime)


@router.get("/agents", response_model=list[AgentAdminView], responses=_ERRORS)
async def list_agents(_: AdminPrincipal, container: ContainerDep) -> list[AgentAdminView]:
    return await _reporting(container).agents()


@router.get("/agents/{agent_name}", response_model=PoolResourceView, responses=_ERRORS)
async def get_agent(
    agent_name: AgentName, _: AdminPrincipal, container: ContainerDep
) -> PoolResourceView:
    return _reporting(container).pool(agent_name)


@router.get("/events", response_model=list[EventView], responses=_ERRORS)
async def list_events(
    _: AdminPrincipal,
    container: ContainerDep,
    agent_name: Annotated[str | None, Query(pattern=AGENT_NAME_PATTERN.pattern)] = None,
    limit: Annotated[int, Query(ge=1, le=500)] = 100,
) -> list[EventView]:
    """What the controllers did and why, newest first. Not state: nothing reads it."""
    return _reporting(container).events(agent_name, limit=limit)


@router.post("/config/reload", response_model=ReloadResult, responses=_ERRORS)
async def reload_config(_: AdminPrincipal, container: ContainerDep) -> ReloadResult:
    """Re-read the agent pool document and apply it. Adding or removing agents needs a restart."""
    return Administration(container.runtime).reload_config()


@router.get(
    "/agents/{agent_name}/sessions", response_model=list[SessionAdminView], responses=_ERRORS
)
async def list_sessions(
    agent_name: AgentName, principal: AdminPrincipal, container: ContainerDep
) -> list[SessionAdminView]:
    reveal = principal.has(container.settings.diagnostics_role)
    return await _reporting(container).sessions(agent_name, reveal_identities=reveal)


@router.get(
    "/agents/{agent_name}/sessions/{session_id}",
    response_model=SessionAdminView,
    responses=_ERRORS,
)
async def get_session(
    agent_name: AgentName,
    session_id: SessionId,
    principal: AdminPrincipal,
    container: ContainerDep,
) -> SessionAdminView:
    reveal = principal.has(container.settings.diagnostics_role)
    return await _reporting(container).session(agent_name, session_id, reveal_identities=reveal)


@router.delete(
    "/agents/{agent_name}/sessions/{session_id}",
    status_code=204,
    responses={202: {"model": DeleteAccepted}, **_ERRORS},
)
async def delete_session(
    agent_name: AgentName, session_id: SessionId, _: AdminPrincipal, container: ContainerDep
) -> Response:
    deleted = await Administration(container.runtime).delete_session(agent_name, session_id)
    if deleted:
        return Response(status_code=204)
    return JSONResponse(DeleteAccepted().model_dump(), status_code=202)


@router.post("/agents/{agent_name}/sync", response_model=SyncReportView, responses=_ERRORS)
async def sync_agent(
    agent_name: AgentName, _: AdminPrincipal, container: ContainerDep
) -> SyncReportView:
    return await Administration(container.runtime).sync(agent_name)


@router.get("/quota", response_model=QuotaView, responses=_ERRORS)
async def quota(_: AdminPrincipal, container: ContainerDep) -> QuotaView:
    """This instance's use of the session quota, and the regional view when a ledger is shared."""
    return await _reporting(container).quota()


@router.get("/metrics", responses=_ERRORS)
async def metrics_snapshot(_: AdminPrincipal, container: ContainerDep) -> dict[str, Any]:
    return _reporting(container).metrics()


@metrics_router.get("/metrics", response_class=PlainTextResponse, responses=_ERRORS)
async def prometheus(_: AdminPrincipal, container: ContainerDep) -> PlainTextResponse:
    return PlainTextResponse(
        _reporting(container).prometheus(),
        media_type="text/plain; version=0.0.4; charset=utf-8",
    )
