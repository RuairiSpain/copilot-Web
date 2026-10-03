"""Miscellaneous sample 1: turn kit errors into your own responses.

Run:   uvicorn hosted_agent_kit.samples.miscellaneous.error_handling:app --reload

``install(app, kit)`` already answers with RFC 7807 problem documents. This sample turns that off
and handles ``HackError`` itself. Every error has ``status``, ``code``, ``phase`` (request, auth,
queue, create, invoke, stream), ``retry_safe`` (a retry cannot repeat work the agent already did)
and, for capacity and throttling, ``retry_after_seconds``.

Common codes: AGENT_NOT_CONFIGURED (404), QUEUE_FULL / POOL_CAPACITY_EXCEEDED (429),
UPSTREAM_THROTTLED (429), QUEUE_WAIT_TIMEOUT / REQUEST_TIMEOUT (504), FOUNDRY_CIRCUIT_OPEN and
SERVICE_SHUTTING_DOWN (503).
"""

from __future__ import annotations

from typing import Any

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

from hosted_agent_kit.errors import HackError, QueueFullError
from hosted_agent_kit.integrations.fastapi import KitDep, install
from hosted_agent_kit.samples._common import UserDep, load_kit

kit = load_kit("agents/support-bot/scheduler.yaml")
app = FastAPI(title="Miscellaneous sample: error handling")
install(app, kit, error_handlers=False)


@app.exception_handler(HackError)
async def handle(_: Request, exc: HackError) -> JSONResponse:
    headers = {"Retry-After": str(exc.retry_after_seconds)} if exc.retry_after_seconds else {}
    return JSONResponse(
        {
            "problem": exc.code,
            "message": exc.detail,
            "where": exc.phase,
            "safe_to_retry": exc.retry_safe,
        },
        status_code=exc.status,
        headers=headers,
    )


@app.post("/ask/{agent}")
async def ask(agent: str, message: str, user: UserDep, kit: KitDep) -> Any:
    try:
        result = await kit.ask(agent, message, user_id=user)
    except QueueFullError:
        # Catch a specific error when you want to do something other than report it.
        return {"answer": "We are very busy. Please try again in a minute."}
    return result.json()
