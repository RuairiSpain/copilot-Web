"""Miscellaneous sample 3: start the kit inside your own lifespan.

Run:   uvicorn hosted_agent_kit.samples.miscellaneous.custom_lifespan:app --reload

``install(app, kit)`` is the shortest way. If you already manage startup yourself, use the kit as
an async context manager. ``async with kit`` connects to Foundry, runs the first sync and starts
the controllers; leaving the block stops admitting calls, waits up to ``shutdown_grace_seconds``
for running ones, and closes the Foundry client. This sample also exposes a readiness probe.
"""

from __future__ import annotations

from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from typing import Any

from fastapi import FastAPI, Request, Response

from hosted_agent_kit.errors import HackError
from hosted_agent_kit.integrations.fastapi import install_error_handlers
from hosted_agent_kit.samples._common import UserDep, load_kit

kit = load_kit("agents/support-bot/scheduler.yaml")


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    # open your own resources here: database pools, caches, ...
    async with kit:
        yield
    # ... and close them here


app = FastAPI(title="Miscellaneous sample: custom lifespan", lifespan=lifespan)
install_error_handlers(app)  # optional: RFC 7807 responses for kit errors


@app.get("/ready")
async def ready(response: Response) -> dict[str, object]:
    """Point your orchestrator's readiness probe here."""
    report = kit.reporting.health()
    response.status_code = 200 if report["status"] == "ready" else 503
    return report


@app.post("/ask")
async def ask(message: str, user: UserDep, request: Request) -> Any:
    try:
        return (await kit.ask("support-bot", message, user_id=user)).json()
    except HackError:
        raise  # handled by install_error_handlers
