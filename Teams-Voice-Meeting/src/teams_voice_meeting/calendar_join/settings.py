from __future__ import annotations

import os
from dataclasses import dataclass


def _int(name: str, default: int) -> int:
    raw = os.environ.get(name, "").strip()
    return int(raw) if raw else default


@dataclass(frozen=True)
class CalendarSettings:
    agent_mailbox: str                 # UPN or object ID of the agent's mailbox
    tenant_id: str | None = None       # needed to join short (/meet/<id>) URLs
    callback_url: str | None = None    # public HTTPS URL Graph posts call notifications to
    poll_seconds: int = 30
    join_lead_seconds: int = 60        # join this long before the meeting starts
    leave_after_end_seconds: int = 300 # leave this long after the scheduled end
    max_join_attempts: int = 3
    allow_external_meetings: bool = False  # join meetings organised in another tenant
    state_file: str = ".calendar-state.json"
    listen: str = "0.0.0.0:8080"       # where the notification receiver listens

    @classmethod
    def from_env(cls) -> "CalendarSettings":
        mailbox = os.environ.get("AGENT_MAILBOX", "").strip()
        if not mailbox:
            raise RuntimeError("Environment variable AGENT_MAILBOX is required")
        return cls(
            agent_mailbox=mailbox,
            tenant_id=os.environ.get("GRAPH_TENANT_ID") or os.environ.get("AZURE_TENANT_ID") or None,
            callback_url=os.environ.get("CALLBACK_URL", "").strip() or None,
            poll_seconds=_int("POLL_SECONDS", 30),
            join_lead_seconds=_int("JOIN_LEAD_SECONDS", 60),
            leave_after_end_seconds=_int("LEAVE_AFTER_END_SECONDS", 300),
            max_join_attempts=_int("MAX_JOIN_ATTEMPTS", 3),
            allow_external_meetings=os.environ.get("ALLOW_EXTERNAL_MEETINGS", "").strip().lower() in {"1", "true", "yes"},
            state_file=os.environ.get("CALENDAR_STATE_FILE", ".calendar-state.json"),
            listen=os.environ.get("LISTEN", "0.0.0.0:8080"),
        )
