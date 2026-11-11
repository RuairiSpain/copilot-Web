"""Voice agent, telephony binding and call operations (azure-ai-projects, voice agents preview)."""

from __future__ import annotations

import logging
import time
from dataclasses import dataclass
from typing import Any

from azure.ai.projects import AIProjectClient
from azure.ai.projects.models import (
    CreateTeamsPhoneExtensibilityTelephonyBindingRequest,
    TelephonyProvider,
    VoiceAgentDefinition,
)
from azure.core.exceptions import ResourceNotFoundError
from azure.identity import DefaultAzureCredential

from .config import Settings

log = logging.getLogger(__name__)

TEAMS = TelephonyProvider.TEAMS_PHONE_EXTENSIBILITY
IN_PROGRESS = "in_progress"


def val(x: Any) -> Any:
    """Plain string for an SDK enum. str(Enum) returns 'Class.MEMBER', so never compare with str()."""
    return getattr(x, "value", x)


@dataclass(frozen=True)
class Binding:
    id: str
    incoming_call_url: str
    status: str


class FoundryVoice:
    """``client`` is any AIProjectClient-compatible object (tests pass a fake)."""

    def __init__(self, settings: Settings, client: Any | None = None) -> None:
        self.settings = settings
        self._client = client or AIProjectClient(
            endpoint=settings.project_endpoint,
            credential=DefaultAzureCredential(),
            allow_preview=True,  # voice agents are a preview feature
        )
        self._telephony = self._client.beta.voice_agents.telephony

    def ensure_agent(self) -> str:
        """Create the voice agent if it does not exist. Returns 'exists' or 'created'."""
        s = self.settings
        try:
            self._client.agents.get(s.agent_name)
            return "exists"
        except ResourceNotFoundError:
            pass
        self._client.agents.create_version(
            agent_name=s.agent_name,
            definition=VoiceAgentDefinition(model_type="managed", model=s.model, instructions=s.instructions),
        )
        return "created"

    def ensure_binding(self) -> Binding:
        """Return the Teams binding for the resource account, creating it when missing."""
        s = self.settings
        for item in self._telephony.list_bindings(s.agent_name, provider=TEAMS):
            if getattr(item, "resource_account_object_id", None) == s.resource_account_object_id:
                return Binding(item.id, item.incoming_call_url, val(item.status))
        created = self._telephony.create_binding(
            s.agent_name,
            CreateTeamsPhoneExtensibilityTelephonyBindingRequest(
                connection_name=s.connection_name,
                resource_account_object_id=s.resource_account_object_id,
                phone_number=s.phone_number,
                label="teams",
            ),
        )
        return Binding(created.id, created.incoming_call_url, val(created.status))

    def calls(self, in_progress_only: bool = True) -> list[Any]:
        kwargs = {"status": IN_PROGRESS} if in_progress_only else {}
        return list(self._telephony.list_calls(self.settings.agent_name, provider=TEAMS, **kwargs))

    def end_call(self, call_id: str) -> Any:
        return self._telephony.end_call(self.settings.agent_name, call_id)

    def watch(self, poll_seconds: float = 3.0, iterations: int | None = None,
              sleep=time.sleep, clock=time.monotonic) -> None:
        """Log call start and end. End any call that runs longer than MAX_CALL_SECONDS."""
        limit = self.settings.max_call_seconds
        seen: dict[str, float] = {}
        while True:
            active = {c.id: c for c in self.calls()}
            for cid in active.keys() - seen.keys():
                seen[cid] = clock()
                log.info("call started %s from %s", cid, getattr(active[cid], "caller_number", None))
            for cid in list(seen.keys() - active.keys()):
                final = self._telephony.get_call(self.settings.agent_name, cid)
                log.info("call ended %s status=%s reason=%s", cid, val(final.status), val(final.end_reason))
                del seen[cid]
            if limit > 0:
                for cid, t0 in seen.items():
                    if cid in active and clock() - t0 > limit:
                        log.warning("call %s exceeded %ss, ending it", cid, limit)
                        self.end_call(cid)
            if iterations is not None:
                iterations -= 1
                if iterations <= 0:
                    return
            sleep(poll_seconds)
