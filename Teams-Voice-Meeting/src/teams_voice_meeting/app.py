"""FastAPI service: receives ACS IncomingCall events and monitors the resulting Foundry call."""

from __future__ import annotations

import asyncio
import logging
import time
from contextlib import asynccontextmanager
from typing import Any

import httpx
from azure.identity import DefaultAzureCredential
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

from .config import Settings
from .events import INCOMING_CALL, as_event_list, parse_incoming_call, validation_code
from .foundry import Binding, FoundryVoice

log = logging.getLogger(__name__)

FOUNDRY_SCOPE = "https://ai.azure.com/.default"
CALL_LOOKUP_ATTEMPTS = 15


class CallTracker:
    """Tracks calls seen in this process so concurrent calls are not matched twice."""

    def __init__(self) -> None:
        self.calls: dict[str, dict[str, Any]] = {}

    def known_ids(self) -> set[str]:
        return set(self.calls)


async def forward_event(url: str, event: dict[str, Any]) -> None:
    """Relay one event to the binding's incoming_call_url (opt-in; see README)."""
    token = DefaultAzureCredential().get_token(FOUNDRY_SCOPE).token
    async with httpx.AsyncClient(timeout=15) as http:
        resp = await http.post(url, json=[event], headers={"Authorization": f"Bearer {token}"})
        resp.raise_for_status()


async def monitor_call(app: FastAPI, correlation_id: str | None, received_at: float) -> None:
    """Find the Foundry call created for an IncomingCall event, then watch it until it ends."""
    voice: FoundryVoice = app.state.voice
    tracker: CallTracker = app.state.tracker
    s: Settings = app.state.settings
    call = None
    for _ in range(CALL_LOOKUP_ATTEMPTS):
        call = await asyncio.to_thread(voice.find_new_call, tracker.known_ids(), received_at - 5)
        if call:
            break
        await asyncio.sleep(s.monitor_poll_seconds)
    if call is None:
        log.error("No Foundry call appeared for correlation %s", correlation_id)
        return
    tracker.calls[call.id] = {"correlation_id": correlation_id, "status": "in_progress"}
    log.info("Monitoring call %s (correlation %s)", call.id, correlation_id)
    final = await asyncio.to_thread(voice.wait_for_end, call.id, s.monitor_poll_seconds, s.max_call_seconds)
    tracker.calls[call.id]["status"] = str(final.status)
    tracker.calls[call.id]["end_reason"] = str(getattr(final, "end_reason", None))
    log.info("Call %s ended: %s (%s)", call.id, final.status, getattr(final, "end_reason", None))


def create_app(settings: Settings | None = None, voice: FoundryVoice | None = None) -> FastAPI:
    @asynccontextmanager
    async def lifespan(app: FastAPI):
        s = settings or Settings.from_env()
        app.state.settings = s
        app.state.voice = voice or FoundryVoice(s)
        app.state.tracker = CallTracker()
        app.state.tasks = set()
        binding: Binding = await asyncio.to_thread(app.state.voice.ensure_binding)
        app.state.binding = binding
        log.info("Point the Event Grid IncomingCall subscription at: %s", binding.incoming_call_url)
        yield
        for call_id, info in app.state.tracker.calls.items():
            if info["status"] == "in_progress":
                await asyncio.to_thread(app.state.voice.end_call, call_id)
        for task in list(app.state.tasks):
            task.cancel()

    app = FastAPI(title="Teams Voice Meeting", lifespan=lifespan)

    @app.get("/healthz")
    async def healthz() -> dict[str, str]:
        return {"status": "ok"}

    @app.get("/api/calls")
    async def calls() -> dict[str, Any]:
        return {"binding": app.state.binding.__dict__, "calls": app.state.tracker.calls}

    @app.post("/api/events")
    async def events(request: Request) -> JSONResponse:
        try:
            payload = await request.json()
        except ValueError:
            return JSONResponse({"error": "invalid JSON"}, status_code=400)
        received_at = time.time()
        for event in as_event_list(payload):
            code = validation_code(event)
            if code:  # Event Grid subscription handshake
                return JSONResponse({"validationResponse": code})
            if event.get("eventType") != INCOMING_CALL:
                continue
            info = parse_incoming_call(event)
            log.info("IncomingCall correlation=%s server_call=%s", info.correlation_id, info.server_call_id)
            if app.state.settings.forward_incoming_calls:
                await forward_event(app.state.binding.incoming_call_url, event)
            task = asyncio.create_task(monitor_call(app, info.correlation_id, received_at))
            app.state.tasks.add(task)
            task.add_done_callback(app.state.tasks.discard)
        return JSONResponse({"status": "accepted"})

    return app
