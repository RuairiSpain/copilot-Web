"""Read-only views of pools, sessions, events and controller reports.

These models are returned by ``Hack.reporting`` and ``Hack.admin`` and by the standalone
service's administration API.
"""

from __future__ import annotations

from datetime import datetime
from typing import Any

from pydantic import BaseModel, Field


class ConditionView(BaseModel):
    type: str
    status: str = Field(description="True, False or Unknown.")
    reason: str
    message: str
    last_transition_time: datetime


class EventView(BaseModel):
    agent_name: str
    object: str = Field(description="`pool`, or `session/<hashed id>`.")
    type: str = Field(description="Normal or Warning.")
    reason: str
    message: str
    count: int
    first_seen: datetime
    last_seen: datetime


class AgentAdminView(BaseModel):
    agent_name: str
    mode: str
    max_sessions: int
    max_active_sessions: int = Field(
        default=0, description="Most sessions that may hold compute at once."
    )
    sessions_counted: int = Field(
        default=0,
        description="Sessions assumed to hold compute: leased, starting, or in the idle window.",
    )
    sessions_total: int
    sessions_available: int
    sessions_leased: int
    sessions_unavailable: int
    sessions_retiring: int
    reserved_slots: int
    queue_depth: int
    queue_max_depth: int
    telemetry_enabled: bool
    circuit_state: str = Field(description="closed, half_open, open or disabled.")
    agent_version: str | None = Field(
        default=None, description="Version new sessions are pinned to; null means the latest."
    )
    version_drain: str = Field(
        default="never", description="never, unbound or idle: which idle old-version sessions go."
    )
    generation: int = Field(default=1, description="Changes when the pool's configuration changes.")
    observed_generation: int = Field(
        default=0, description="The generation the controllers have applied. Behind means pending."
    )
    conditions: list[ConditionView] = []


class SessionAdminView(BaseModel):
    session_id: str
    agent_name: str
    agent_version: str | None
    platform_status: str
    local_state: str
    leased: bool
    bound_to_user: bool
    restorable: bool = Field(
        description="Found after a restart and held for the user it belongs to to reclaim."
    )
    user: str | None = Field(description="Redacted user reference; raw only with diagnostics role.")
    conversation: str | None = Field(
        default=None, description="Redacted conversation key; raw only with diagnostics role."
    )
    generation: int = 1
    resource_version: int = 0
    finalizers: list[str] = Field(
        default=[], description="What must finish before the session can be removed."
    )
    deletion_timestamp: datetime | None = Field(
        default=None, description="Set when the session has been marked for deletion."
    )
    deletion_reason: str | None = None
    conditions: list[ConditionView] = []
    created_at: datetime
    last_accessed_at: datetime | None
    expires_at: datetime | None
    last_released_at: datetime | None


class PoolResourceView(BaseModel):
    """One agent pool: what is wanted (`spec`) and what is observed (`status`)."""

    agent_name: str
    generation: int
    observed_generation: int
    resource_version: int
    spec: dict[str, Any]
    ready_sessions: int
    leased_sessions: int
    provisioning_sessions: int
    deleting_sessions: int
    reserved_slots: int
    queued_requests: int
    conditions: list[ConditionView]
    events: list[EventView]


class ReloadResult(BaseModel):
    changed: dict[str, int] = Field(
        description="The agents whose settings changed, with their new generation."
    )


class SyncReportView(BaseModel):
    agent_name: str
    outcome: str
    discovered: int
    removed: int
    failed_sessions: int
    uncertain: int
    warm_created: int
    drained: int = 0
    duration_seconds: float
    errors: list[str] = Field(
        default_factory=list, description="Error codes that explain an incomplete or failed sync."
    )


class DeleteAccepted(BaseModel):
    status: str = "deferred"
    detail: str = "The session is in use and will be deleted when its lease ends."


class AgentInfo(BaseModel):
    """What a caller can do with one agent. Contains no administrative detail."""

    name: str
    protocol: str = Field(description="responses or invocations.")
    stateful: bool = Field(description="True when each user (and conversation) keeps a session.")
    streaming: str = Field(
        description="`request`: the caller asks with `stream`. `agent`: the agent decides."
    )
    supports_conversation_key: bool
    queue_enabled: bool
    queue_max_wait_seconds: float | None
    agent_version: str | None = Field(description="Pinned version for new sessions, or null.")
    default_timeout_seconds: float
    max_timeout_seconds: float
    max_stream_seconds: float


class RegionalView(BaseModel):
    """What every kit that shares the ledger reports for one subscription and region."""

    region_limit: int | None = Field(description="The subscription's concurrent session limit.")
    total_active: int = Field(description="Sum of the counts the live kits last published.")
    headroom: int | None = Field(
        description="Region limit minus total active, when the limit is known."
    )
    spare_size: int
    spare_used: int
    kits: dict[str, int] = Field(description="Kit id to the active sessions it last reported.")


class QuotaView(BaseModel):
    """This kit's use of the session quota."""

    kit_id: str
    counted: int = Field(description="Sessions this kit assumes hold compute, all agents.")
    budget: int | None = Field(description="The configured static budget, if any.")
    limit: int | None = Field(
        description="The limit now: the budget as lowered or raised by Foundry's answers, plus "
        "borrowed permits. Null when nothing limits the kit."
    )
    borrowed: int = Field(description="Spare-pool permits this kit holds.")
    refusals: int = Field(description="Times Foundry refused a session for quota.")
    evictions: int = Field(description="Idle sessions this kit stopped to make room.")
    regional: RegionalView | None = Field(
        default=None, description="The shared view, when a ledger is configured and reachable."
    )
