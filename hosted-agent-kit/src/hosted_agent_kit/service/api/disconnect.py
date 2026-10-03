"""Run work while watching for the client going away (PRD section 8.3)."""

from __future__ import annotations

import asyncio
from collections.abc import Awaitable

from starlette.requests import Request

from hosted_agent_kit.domain.errors import ClientDisconnectedError


async def _wait_for_disconnect(request: Request) -> None:
    while True:
        message = await request.receive()
        if message["type"] == "http.disconnect":
            return


async def run_until_disconnect[T](request: Request, work: Awaitable[T]) -> T:
    """Await ``work``; if the client disconnects first, cancel it and raise.

    Cancelling the work triggers the pool's cleanup, which releases the lease and
    removes any queue waiter.
    """
    task = asyncio.ensure_future(work)
    watcher = asyncio.ensure_future(_wait_for_disconnect(request))
    try:
        done, _ = await asyncio.wait({task, watcher}, return_when=asyncio.FIRST_COMPLETED)
        if task in done:
            return task.result()
        raise ClientDisconnectedError
    finally:
        task.cancel()
        watcher.cancel()
        await asyncio.gather(task, watcher, return_exceptions=True)
