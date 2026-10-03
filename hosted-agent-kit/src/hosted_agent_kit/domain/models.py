"""Pydantic v2 domain models."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from pydantic import BaseModel, ConfigDict

from hosted_agent_kit.domain.enums import (
    AgentProtocol,
    FoundrySessionStatus,
    LocalSessionState,
)
from hosted_agent_kit.domain.resources import Condition, SessionKey


class SessionAffinityKey(BaseModel, frozen=True):
    """One stateful session: a user, an agent and optionally one of the user's conversations."""

    user_id: str
    agent_name: str
    conversation_key: str | None = None


class SessionRecord(BaseModel):
    session_id: str
    agent_name: str
    agent_version: str | None = None
    platform_status: FoundrySessionStatus
    local_state: LocalSessionState
    affinity_key: SessionAffinityKey | None = None
    created_at: datetime
    last_seen_at: datetime
    last_accessed_at: datetime | None = None
    expires_at: datetime | None = None
    lease_request_id: str | None = None
    lease_acquired_at: datetime | None = None
    last_released_at: datetime | None = None
    # Resource metadata, maintained by the store.
    generation: int = 1  # changes when the desired state (affinity binding, deletion) changes
    resource_version: int = 0  # changes on every stored update
    finalizers: list[str] = []
    deletion_timestamp: datetime | None = None
    conditions: list[Condition] = []
    deletion_reason: str | None = None  # why deletion was requested, for metrics and events
    # Set while a session this pool created is still becoming usable.
    provisioning_started_at: datetime | None = None

    @property
    def key(self) -> SessionKey:
        return SessionKey(agent_name=self.agent_name, session_id=self.session_id)


class AgentSummary(BaseModel):
    name: str
    state: str | None = None


class FoundrySession(BaseModel):
    """Normalised view of a Foundry session resource."""

    session_id: str
    agent_name: str
    agent_version: str | None = None
    status: FoundrySessionStatus
    created_at: datetime
    last_accessed_at: datetime | None = None
    expires_at: datetime | None = None


class InvokeContext(BaseModel):
    """Everything the adapter needs for one upstream invocation."""

    model_config = ConfigDict(frozen=True)

    agent_name: str
    session_id: str
    # A JSON object for Responses. Invocations may send any JSON value, or raw bytes.
    payload: Any = None
    raw_body: bytes | None = None
    content_type: str | None = None
    timeout_seconds: float
    request_id: str
    correlation_id: str
    stream: bool = False
    protocol: AgentProtocol = AgentProtocol.RESPONSES


class AffinityEntry(BaseModel, frozen=True):
    """Affinity state for a key: bound to a session, or pending creation."""

    session_id: str | None = None
    pending_request_id: str | None = None


class AgentSnapshot(BaseModel):
    agent_name: str
    mode: str
    max_sessions: int
    sessions_total: int
    sessions_available: int
    sessions_leased: int
    sessions_unavailable: int
    sessions_retiring: int
    reserved_slots: int
    queue_depth: int
    queue_max_depth: int
    telemetry_enabled: bool
