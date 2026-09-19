"""Structured logging.

One JSON object per line by default (``JEV_LOG_JSON=false`` for a readable
console format). Whatever a call site passes as ``extra=`` is merged into the
object, which is how ``scenario``, ``decision_type`` and ``calibration_version``
reach the log without a formatter change per field.
"""

from __future__ import annotations

import json
import logging
import sys
from typing import Any

#: Attributes ``logging`` puts on every record; anything else came from `extra`.
_RESERVED = set(
    vars(logging.LogRecord("", 0, "", 0, "", (), None)).keys()
) | {"asctime", "message", "taskName"}


class JsonFormatter(logging.Formatter):
    """Render a record, plus its `extra` fields, as a single JSON object."""

    def __init__(self, service: str) -> None:
        super().__init__()
        self._service = service

    def format(self, record: logging.LogRecord) -> str:
        payload: dict[str, Any] = {
            "timestamp": self.formatTime(record, "%Y-%m-%dT%H:%M:%S%z"),
            "level": record.levelname,
            "logger": record.name,
            "service": self._service,
            "message": record.getMessage(),
        }
        for key, value in record.__dict__.items():
            if key not in _RESERVED and not key.startswith("_"):
                payload[key] = value
        if record.exc_info:
            payload["exception"] = self.formatException(record.exc_info)
        return json.dumps(payload, default=str)


def configure_logging(level: str = "INFO", json_output: bool = True, service: str = "jev-byom") -> None:
    """Install the formatter on the root logger, replacing any existing handlers."""
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(
        JsonFormatter(service)
        if json_output
        else logging.Formatter("%(asctime)s %(levelname)-8s %(name)s :: %(message)s")
    )
    root = logging.getLogger()
    root.handlers = [handler]
    root.setLevel(level.upper())
    # uvicorn installs its own handlers; route them through ours instead.
    for name in ("uvicorn", "uvicorn.error", "uvicorn.access"):
        uvicorn_logger = logging.getLogger(name)
        uvicorn_logger.handlers = []
        uvicorn_logger.propagate = True
