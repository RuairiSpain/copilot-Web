"""Result types."""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True)
class Limits:
    max_body_bytes: int
    default_timeout_seconds: float
    max_timeout_seconds: float
    max_stream_seconds: float
    max_request_seconds: float | None
    max_response_bytes: int | None


@dataclass(frozen=True)
class AgentInfo:
    name: str
    protocol: str
    stateful: bool
    streaming: str
    supports_conversation_key: bool
    endpoints: list[str]
    request_content_types: list[str]
    queue_enabled: bool
    queue_max_wait_seconds: float | None
    idempotency_ttl_seconds: float
    agent_version: str | None
    limits: Limits

    @classmethod
    def from_json(cls, data: dict[str, Any]) -> AgentInfo:
        values = {k: v for k, v in data.items() if k != "limits"}
        return cls(**values, limits=Limits(**data["limits"]))


@dataclass(frozen=True)
class ResponsesResult:
    request_id: str
    result: dict[str, Any]
    correlation_id: str | None = None
    replayed: bool = False  # True when the service replayed an earlier response for this key


@dataclass(frozen=True)
class InvocationResult:
    status_code: int
    content_type: str
    body: bytes
    request_id: str | None = None
    correlation_id: str | None = None
    replayed: bool = False
    headers: dict[str, str] = field(default_factory=dict)

    def text(self) -> str:
        return self.body.decode()

    def json(self) -> Any:
        return json.loads(self.body)
