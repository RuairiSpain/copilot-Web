"""Structured JSON logging with correlation context and identifier redaction."""

from __future__ import annotations

import hashlib
import json
import logging
from contextvars import ContextVar
from datetime import UTC, datetime
from typing import Any

correlation_id_var: ContextVar[str | None] = ContextVar("correlation_id", default=None)
request_id_var: ContextVar[str | None] = ContextVar("request_id", default=None)

_REDACTED_KEYS = frozenset(
    {
        "authorization",
        "token",
        "access_token",
        "secret",
        "password",
        "api_key",
        "prompt",
        "response",
    }
)
_RESERVED = {"event", "timestamp", "level", "logger", "correlation_id", "request_id"}


def hash_identifier(value: str) -> str:
    """Return a short, stable, non-reversible token for session or user identifiers."""
    return hashlib.sha256(value.encode("utf-8")).hexdigest()[:12]


class JsonFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        payload: dict[str, Any] = {
            "event": record.getMessage(),
            "timestamp": datetime.fromtimestamp(record.created, UTC).isoformat(),
            "level": record.levelname.lower(),
            "logger": record.name,
            "correlation_id": correlation_id_var.get(),
            "request_id": request_id_var.get(),
        }
        fields = getattr(record, "fields", {})
        for key, value in fields.items():
            if key in _RESERVED:
                key = f"field_{key}"
            payload[key] = "[redacted]" if key.lower() in _REDACTED_KEYS else value
        if record.exc_info and record.exc_info[0] is not None:
            payload["exception_type"] = record.exc_info[0].__name__
            payload["exception"] = self.formatException(record.exc_info)
        return json.dumps(payload, default=str, separators=(",", ":"))


def configure_logging(level: str = "INFO") -> None:
    handler = logging.StreamHandler()
    handler.setFormatter(JsonFormatter())
    root = logging.getLogger("hosted_agent_kit")
    root.handlers = [handler]
    root.setLevel(level.upper())
    root.propagate = False


def log_event(
    logger: logging.Logger, event: str, *, level: int = logging.INFO, **fields: Any
) -> None:
    """Emit a structured event. Session identifiers must be passed as ``session_id_hash``."""
    logger.log(level, event, extra={"fields": fields})
