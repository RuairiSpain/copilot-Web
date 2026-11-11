"""Environment-based settings."""

from __future__ import annotations

import os
import uuid
from dataclasses import dataclass

ORGID_PREFIX = "28:orgid:"


def normalize_resource_account_id(value: str) -> str:
    """Return the resource account object ID as a bare lowercase GUID.

    Foundry requires the GUID only. A Teams MRI such as ``28:orgid:<guid>`` is accepted and the
    prefix is removed.
    """
    raw = value.strip()
    if raw.lower().startswith(ORGID_PREFIX):
        raw = raw[len(ORGID_PREFIX):]
    try:
        return str(uuid.UUID(raw))
    except ValueError as exc:
        raise ValueError(f"RESOURCE_ACCOUNT_OBJECT_ID is not a GUID: {value!r}") from exc


def _required(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"Environment variable {name} is required")
    return value


def _flag(name: str, default: bool = False) -> bool:
    raw = os.environ.get(name)
    if raw is None or not raw.strip():
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


@dataclass(frozen=True)
class Settings:
    project_endpoint: str
    agent_name: str
    connection_name: str
    resource_account_object_id: str
    phone_number: str | None = None
    forward_incoming_calls: bool = False
    monitor_poll_seconds: float = 2.0
    max_call_seconds: int = 7200

    @classmethod
    def from_env(cls) -> "Settings":
        return cls(
            project_endpoint=_required("FOUNDRY_PROJECT_ENDPOINT"),
            agent_name=_required("VOICE_AGENT_NAME"),
            connection_name=_required("ACS_CONNECTION_NAME"),
            resource_account_object_id=normalize_resource_account_id(_required("RESOURCE_ACCOUNT_OBJECT_ID")),
            phone_number=os.environ.get("TEAMS_PHONE_NUMBER", "").strip() or None,
            forward_incoming_calls=_flag("FORWARD_INCOMING_CALLS"),
            monitor_poll_seconds=float(os.environ.get("MONITOR_POLL_SECONDS", "2")),
            max_call_seconds=int(os.environ.get("MAX_CALL_SECONDS", "7200")),
        )
