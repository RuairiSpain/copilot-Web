"""RFC 7807 problem+json responses and exception handlers."""

from __future__ import annotations

import logging
from typing import Any, cast

from fastapi import FastAPI, Request
from fastapi.encoders import jsonable_encoder
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse, Response
from starlette.exceptions import HTTPException as StarletteHTTPException

from hosted_agent_kit.domain.errors import AppError
from hosted_agent_kit.logging_config import correlation_id_var, log_event, request_id_var

logger = logging.getLogger(__name__)
PROBLEM_JSON = "application/problem+json"

_GENERIC_STATUS_CODES = {
    404: ("NOT_FOUND", "Resource not found"),
    405: ("METHOD_NOT_ALLOWED", "Method not allowed"),
    413: ("PAYLOAD_TOO_LARGE", "Request body too large"),
}


def problem_response(
    *,
    status: int,
    code: str,
    title: str,
    detail: str,
    retry_after_seconds: int | None = None,
    headers: dict[str, str] | None = None,
    extra: dict[str, Any] | None = None,
    phase: str = "request",
    retry_safe: bool = False,
) -> JSONResponse:
    body: dict[str, Any] = {
        "type": f"urn:hosted-agent-kit:error:{code}",
        "title": title,
        "status": status,
        "detail": detail,
        "error_code": code,
        "correlation_id": correlation_id_var.get(),
        "request_id": request_id_var.get(),
        "phase": phase,
        "retry_safe": retry_safe,
    }
    if retry_after_seconds is not None:
        body["retry_after_seconds"] = retry_after_seconds
    if extra:
        body.update(extra)
    response_headers = dict(headers or {})
    if retry_after_seconds is not None:
        response_headers["Retry-After"] = str(retry_after_seconds)
    return JSONResponse(body, status_code=status, media_type=PROBLEM_JSON, headers=response_headers)


def _app_error(_: Request, exc: Exception) -> Response:
    exc = cast(AppError, exc)
    return problem_response(
        status=exc.status,
        code=exc.code,
        title=exc.title,
        detail=exc.detail,
        retry_after_seconds=exc.retry_after_seconds,
        headers=exc.headers,
        phase=exc.phase,
        retry_safe=exc.retry_safe,
    )


def _validation_error(_: Request, exc: Exception) -> Response:
    exc = cast(RequestValidationError, exc)
    # Only location, message and type are returned: never the submitted input values.
    errors = [
        {"loc": [str(part) for part in item["loc"]], "msg": item["msg"], "type": item["type"]}
        for item in exc.errors()
    ]
    return problem_response(
        status=422,
        code="VALIDATION_ERROR",
        title="Validation failed",
        detail="The request did not pass validation.",
        extra={"errors": jsonable_encoder(errors)},
        retry_safe=True,
    )


def _http_error(_: Request, exc: Exception) -> Response:
    exc = cast(StarletteHTTPException, exc)
    code, title = _GENERIC_STATUS_CODES.get(exc.status_code, ("HTTP_ERROR", "Request failed"))
    return problem_response(
        status=exc.status_code, code=code, title=title, detail=title, retry_safe=True
    )


def unhandled_response(exc: Exception) -> Response:
    log_event(
        logger,
        "unhandled_exception",
        level=logging.ERROR,
        error_type=type(exc).__name__,
    )
    return problem_response(
        status=500,
        code="INTERNAL_ERROR",
        title="Internal error",
        detail="An unexpected error occurred. Quote the correlation id when reporting it.",
    )


def install_error_handlers(app: FastAPI) -> None:
    app.add_exception_handler(AppError, _app_error)
    app.add_exception_handler(RequestValidationError, _validation_error)
    app.add_exception_handler(StarletteHTTPException, _http_error)
