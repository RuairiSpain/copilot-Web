"""Environment-based settings."""

from __future__ import annotations

import os
import uuid
from dataclasses import dataclass

ORGID_PREFIX = "28:orgid:"


def normalize_resource_account_id(value: str) -> str:
    """Return the resource account object ID as a bare lowercase GUID (Foundry rejects the MRI prefix)."""
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


@dataclass(frozen=True)
class Settings:
    project_endpoint: str
    agent_name: str
    connection_name: str
    resource_account_object_id: str
    phone_number: str | None = None
    model: str = "gpt-realtime"
    instructions: str = "You are a helpful voice assistant in a Microsoft Teams call. Keep answers short."
    max_call_seconds: int = 7200

    @classmethod
    def from_env(cls) -> "Settings":
        d = cls.__dataclass_fields__
        return cls(
            project_endpoint=_required("FOUNDRY_PROJECT_ENDPOINT"),
            agent_name=_required("VOICE_AGENT_NAME"),
            connection_name=_required("ACS_CONNECTION_NAME"),
            resource_account_object_id=normalize_resource_account_id(_required("RESOURCE_ACCOUNT_OBJECT_ID")),
            phone_number=os.environ.get("TEAMS_PHONE_NUMBER", "").strip() or None,
            model=os.environ.get("VOICE_MODEL") or d["model"].default,
            instructions=os.environ.get("VOICE_INSTRUCTIONS") or d["instructions"].default,
            max_call_seconds=int(os.environ.get("MAX_CALL_SECONDS", "7200")),
        )
