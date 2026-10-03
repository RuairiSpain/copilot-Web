"""Pure ASGI middleware: correlation ids, body size limit and last-resort error handling."""

from __future__ import annotations

import re
import uuid

from starlette.datastructures import Headers, MutableHeaders
from starlette.exceptions import HTTPException
from starlette.types import ASGIApp, Message, Receive, Scope, Send

from hosted_agent_kit.logging_config import correlation_id_var, request_id_var
from hosted_agent_kit.service.api.errors import problem_response, unhandled_response

_CORRELATION_PATTERN = re.compile(r"^[A-Za-z0-9._:-]{1,128}$")


class CorrelationMiddleware:
    """Assigns correlation and request ids, echoes them, and turns crashes into problem+json."""

    def __init__(self, app: ASGIApp) -> None:
        self.app = app

    async def __call__(self, scope: Scope, receive: Receive, send: Send) -> None:
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return
        incoming = Headers(scope=scope).get("x-correlation-id", "")
        correlation_id = incoming if _CORRELATION_PATTERN.match(incoming) else uuid.uuid4().hex
        request_id = f"req_{uuid.uuid4().hex[:24]}"
        scope["state"] = {**scope.get("state", {}), "request_id": request_id}
        tokens = (correlation_id_var.set(correlation_id), request_id_var.set(request_id))
        started = False

        async def send_with_headers(message: Message) -> None:
            nonlocal started
            if message["type"] == "http.response.start":
                started = True
                headers = MutableHeaders(scope=message)
                headers["X-Correlation-ID"] = correlation_id
                headers["X-Request-ID"] = request_id
            await send(message)

        try:
            await self.app(scope, receive, send_with_headers)
        except Exception as exc:
            if started:
                raise
            await unhandled_response(exc)(scope, receive, send_with_headers)
        finally:
            correlation_id_var.reset(tokens[0])
            request_id_var.reset(tokens[1])


class BodyLimitMiddleware:
    """Rejects bodies larger than ``max_bytes``, whether declared or streamed."""

    def __init__(self, app: ASGIApp, max_bytes: int) -> None:
        self.app = app
        self.max_bytes = max_bytes

    async def __call__(self, scope: Scope, receive: Receive, send: Send) -> None:
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return
        declared = Headers(scope=scope).get("content-length", "")
        if declared.isdigit() and int(declared) > self.max_bytes:
            response = problem_response(
                status=413,
                code="PAYLOAD_TOO_LARGE",
                title="Request body too large",
                detail="The request body exceeds the configured limit.",
                retry_safe=True,
            )
            await response(scope, receive, send)
            return
        received = 0

        async def limited_receive() -> Message:
            nonlocal received
            message = await receive()
            if message["type"] == "http.request":
                received += len(message.get("body", b""))
                if received > self.max_bytes:
                    # HTTPException is re-raised untouched by FastAPI's body parsing.
                    raise HTTPException(status_code=413)
            return message

        await self.app(scope, limited_receive, send)
