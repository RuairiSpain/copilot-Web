"""Resource vocabulary shared by the store, the controllers and the API.

The model follows Kubernetes control-plane conventions: every resource has a key, a
``generation`` that changes when its desired state changes, a ``resource_version`` that changes on
every stored update, ``finalizers`` that hold a resource until external cleanup is confirmed, and
status ``conditions`` that say what is true of it and why.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from enum import StrEnum
from typing import TYPE_CHECKING

from pydantic import BaseModel

from hosted_agent_kit.domain.enums import FoundrySessionStatus, LocalSessionState

if TYPE_CHECKING:
    from hosted_agent_kit.domain.models import SessionRecord

# Held on a session until Foundry confirms that the remote session is gone.
FINALIZER_SESSION_CLEANUP = "foundry-session-cleanup"
# Held while Foundry itself reports the session as deleting. The observer removes it once the
# session is gone, so the record is not dropped (and the session re-adopted) in between.
FINALIZER_PLATFORM_DELETION = "foundry-platform-deletion"


class ConditionStatus(StrEnum):
    TRUE = "True"
    FALSE = "False"
    UNKNOWN = "Unknown"


class Condition(BaseModel, frozen=True):
    """One observed fact about a resource: ``type`` is true, false or unknown, and why."""

    type: str
    status: ConditionStatus
    reason: str
    message: str = ""
    last_transition_time: datetime


def get_condition(conditions: list[Condition], type_: str) -> Condition | None:
    return next((c for c in conditions if c.type == type_), None)


def set_condition(
    conditions: list[Condition],
    type_: str,
    status: ConditionStatus,
    reason: str,
    message: str = "",
    *,
    now: datetime,
) -> list[Condition]:
    """Return the conditions with ``type_`` set. The transition time moves only on a change."""
    current = get_condition(conditions, type_)
    if (
        current is not None
        and current.status is status
        and current.reason == reason
        and current.message == message
    ):
        return conditions
    changed_status = current is None or current.status is not status
    updated = Condition(
        type=type_,
        status=status,
        reason=reason,
        message=message,
        last_transition_time=now
        if changed_status
        else (current.last_transition_time if current else now),
    )
    return [*[c for c in conditions if c.type != type_], updated]


def drop_condition(conditions: list[Condition], type_: str) -> list[Condition]:
    return [c for c in conditions if c.type != type_]


class SessionKey(BaseModel, frozen=True):
    """A session id is unique only within one agent endpoint, so the key has both parts."""

    agent_name: str
    session_id: str

    def __str__(self) -> str:
        return f"{self.agent_name}/{self.session_id}"


class ResourceKind(StrEnum):
    AGENT_POOL = "AgentPool"
    AGENT_SESSION = "AgentSession"


@dataclass(frozen=True)
class ResourceKey:
    """Identity of one resource in a controller work queue."""

    kind: ResourceKind
    agent_name: str
    name: str = ""  # the session id for an AgentSession, empty for an AgentPool

    def __str__(self) -> str:
        return f"{self.kind.value}/{self.agent_name}" + (f"/{self.name}" if self.name else "")


def pool_key(agent_name: str) -> ResourceKey:
    return ResourceKey(ResourceKind.AGENT_POOL, agent_name)


def session_key(agent_name: str, session_id: str) -> ResourceKey:
    return ResourceKey(ResourceKind.AGENT_SESSION, agent_name, session_id)


# Condition types on an AgentSession.
SESSION_PROVISIONED = "Provisioned"
SESSION_READY = "Ready"
SESSION_LEASED = "Leased"
SESSION_DELETION_PENDING = "DeletionPending"
SESSION_DELETION_FAILED = "DeletionFailed"
SESSION_RECOVERABLE = "Recoverable"

# Condition types on an AgentPool.
POOL_READY = "Ready"
POOL_INITIAL_SYNC_COMPLETE = "InitialSyncComplete"
POOL_CAPACITY_AVAILABLE = "CapacityAvailable"
POOL_FOUNDRY_REACHABLE = "FoundryReachable"
POOL_CIRCUIT_OPEN = "CircuitOpen"
POOL_CONFIGURATION_VALID = "ConfigurationValid"
POOL_RECOVERY_BLOCKED = "RecoveryBlocked"

_ACTIVE = (FoundrySessionStatus.ACTIVE, FoundrySessionStatus.IDLE)
_PROVISIONING = (FoundrySessionStatus.CREATING, FoundrySessionStatus.UPDATING)


def derive_session_conditions(record: SessionRecord, now: datetime) -> list[Condition]:
    """Recompute the conditions that follow from a session's state. Others are kept."""
    conditions = list(record.conditions)
    status = record.platform_status
    if status in _ACTIVE:
        provisioned = (ConditionStatus.TRUE, "Active", "")
    elif status in _PROVISIONING:
        provisioned = (ConditionStatus.FALSE, "Provisioning", f"Foundry reports {status.value}.")
    elif status is FoundrySessionStatus.FAILED:
        provisioned = (ConditionStatus.FALSE, "Failed", "Foundry reports the session failed.")
    else:
        provisioned = (ConditionStatus.FALSE, "NotActive", f"Foundry reports {status.value}.")
    conditions = set_condition(
        conditions, SESSION_PROVISIONED, provisioned[0], provisioned[1], provisioned[2], now=now
    )

    deleting = record.deletion_timestamp is not None
    usable = (
        status in _ACTIVE
        and not deleting
        and record.local_state in (LocalSessionState.AVAILABLE, LocalSessionState.LEASED)
    )
    if usable:
        ready = (ConditionStatus.TRUE, "Schedulable", "")
    elif deleting:
        ready = (ConditionStatus.FALSE, "Deleting", "The session is being deleted.")
    else:
        ready = (
            ConditionStatus.FALSE,
            "NotSchedulable",
            f"Local state is {record.local_state.value}, Foundry reports {status.value}.",
        )
    conditions = set_condition(conditions, SESSION_READY, ready[0], ready[1], ready[2], now=now)

    leased = record.lease_request_id is not None
    conditions = set_condition(
        conditions,
        SESSION_LEASED,
        ConditionStatus.TRUE if leased else ConditionStatus.FALSE,
        "InUse" if leased else "Idle",
        now=now,
    )
    conditions = set_condition(
        conditions,
        SESSION_DELETION_PENDING,
        ConditionStatus.TRUE if deleting else ConditionStatus.FALSE,
        "DeletionRequested" if deleting else "NotRequested",
        now=now,
    )
    if not deleting:
        conditions = drop_condition(conditions, SESSION_DELETION_FAILED)
    return conditions
