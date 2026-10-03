"""Liveness and readiness probes (unauthenticated, no sensitive detail)."""

from __future__ import annotations

import asyncio
import logging

from fastapi import APIRouter
from fastapi.responses import JSONResponse

from hosted_agent_kit.domain.errors import FoundryError
from hosted_agent_kit.logging_config import log_event
from hosted_agent_kit.service.api.deps import ContainerDep
from hosted_agent_kit.service.container import Container

logger = logging.getLogger(__name__)
router = APIRouter(prefix="/health", tags=["health"])

PROBE_TIMEOUT_SECONDS = 5.0


async def _probe_foundry(container: Container) -> bool:
    async def first_agent() -> None:
        async for _ in container.adapter.list_agents():
            return

    try:
        await asyncio.wait_for(first_agent(), timeout=PROBE_TIMEOUT_SECONDS)
    except (FoundryError, TimeoutError) as exc:
        log_event(
            logger, "readiness_probe_failed", level=logging.WARNING, error_type=type(exc).__name__
        )
        return False
    return True


@router.get("/live")
async def live() -> dict[str, str]:
    return {"status": "alive"}


@router.get("/ready")
async def ready(container: ContainerDep) -> JSONResponse:
    checks = {
        "configuration": True,
        "identity": container.ready,
        "workers": container.reconciler.workers_started,
        "initial_sync": container.reconciler.initial_sync_done,
    }
    if container.settings.readiness_probe_foundry:
        checks["foundry"] = await _probe_foundry(container)
    healthy = all(checks.values())
    body = {"status": "ready" if healthy else "not_ready", "checks": checks}
    return JSONResponse(body, status_code=200 if healthy else 503)
