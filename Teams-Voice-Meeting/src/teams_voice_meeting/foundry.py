"""Thin wrapper over the azure-ai-projects voice agent telephony operations (preview)."""

from __future__ import annotations

import logging
import time
from dataclasses import dataclass
from typing import Any

from azure.ai.projects import AIProjectClient
from azure.ai.projects.models import (
    CreateTeamsPhoneExtensibilityTelephonyBindingRequest,
    TelephonyProvider,
)
from azure.identity import DefaultAzureCredential

from .config import Settings

log = logging.getLogger(__name__)

TEAMS = TelephonyProvider.TEAMS_PHONE_EXTENSIBILITY
IN_PROGRESS = "in_progress"


@dataclass(frozen=True)
class Binding:
    id: str
    incoming_call_url: str
    status: str


class FoundryVoice:
    """Operations on one voice agent. ``client`` is any AIProjectClient-compatible object."""

    def __init__(self, settings: Settings, client: Any | None = None) -> None:
        self.settings = settings
        if client is None:
            # DefaultAzureCredential uses az login / VS Code / env vars locally and managed identity in Azure.
            client = AIProjectClient(
                endpoint=settings.project_endpoint,
                credential=DefaultAzureCredential(),
                allow_preview=True,  # voice agents are a preview feature
            )
        self._telephony = client.beta.voice_agents.telephony

    def ensure_binding(self) -> Binding:
        """Return the Teams binding for the resource account, creating it when missing."""
        s = self.settings
        for item in self._telephony.list_bindings(s.agent_name, provider=TEAMS):
            if getattr(item, "resource_account_object_id", None) == s.resource_account_object_id:
                log.info("Reusing telephony binding %s", item.id)
                return Binding(item.id, item.incoming_call_url, str(item.status))
        created = self._telephony.create_binding(
            s.agent_name,
            CreateTeamsPhoneExtensibilityTelephonyBindingRequest(
                connection_name=s.connection_name,
                resource_account_object_id=s.resource_account_object_id,
                phone_number=s.phone_number,
                label="teams-meeting",
            ),
        )
        log.info("Created telephony binding %s", created.id)
        return Binding(created.id, created.incoming_call_url, str(created.status))

    def active_calls(self) -> list[Any]:
        return list(self._telephony.list_calls(self.settings.agent_name, provider=TEAMS, status=IN_PROGRESS))

    def get_call(self, call_id: str) -> Any:
        return self._telephony.get_call(self.settings.agent_name, call_id)

    def end_call(self, call_id: str) -> Any:
        return self._telephony.end_call(self.settings.agent_name, call_id)

    def find_new_call(self, known_ids: set[str], since: float) -> Any | None:
        """Newest in-progress call that is not in ``known_ids`` and started at or after ``since``."""
        fresh = [
            c for c in self.active_calls()
            if c.id not in known_ids and (getattr(c, "started_at", None) or 0) >= since
        ]
        return max(fresh, key=lambda c: getattr(c, "started_at", 0) or 0, default=None)

    def wait_for_end(self, call_id: str, poll_seconds: float, max_seconds: int) -> Any:
        """Poll until the call leaves ``in_progress``. End it if it exceeds ``max_seconds``."""
        deadline = time.monotonic() + max_seconds if max_seconds > 0 else None
        while True:
            call = self.get_call(call_id)
            if str(call.status) != IN_PROGRESS:
                return call
            if deadline is not None and time.monotonic() >= deadline:
                log.warning("Call %s exceeded %ss, ending it", call_id, max_seconds)
                return self.end_call(call_id)
            time.sleep(poll_seconds)
