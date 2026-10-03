"""Typed enumerations shared across the service."""

from __future__ import annotations

from enum import StrEnum


class AgentMode(StrEnum):
    STATEFUL = "stateful"
    STATELESS = "stateless"


class AffinityMode(StrEnum):
    USER = "user"
    NONE = "none"


class SchedulerStrategy(StrEnum):
    FIRST_AVAILABLE = "first_available"
    ROUND_ROBIN = "round_robin"
    OLDEST_IDLE = "oldest_idle"
    NEWEST_IDLE = "newest_idle"


class VersionDrain(StrEnum):
    """What to do with idle sessions that run an agent version other than the target."""

    NEVER = "never"  # leave them. They keep running the version they were created with.
    UNBOUND = "unbound"  # retire idle pooled sessions that no user owns
    IDLE = "idle"  # also retire idle user sessions. Their conversation state is lost.


class UserIsolation(StrEnum):
    """What per-user information is sent to Foundry on each invocation."""

    OFF = "off"  # nothing per user (one constant key, or none)
    KEY = "key"  # x-ms-user-isolation-key derived from the user id
    DELEGATED = "delegated"  # the key plus x-ms-user-identity: the acting user


class AgentProtocol(StrEnum):
    """The protocol a hosted agent container exposes."""

    RESPONSES = "responses"
    INVOCATIONS = "invocations"


class CircuitState(StrEnum):
    CLOSED = "closed"
    HALF_OPEN = "half_open"
    OPEN = "open"

    @property
    def level(self) -> int:
        """Numeric gauge value: closed 0, half open 1, open 2."""
        return {"closed": 0, "half_open": 1, "open": 2}[self.value]


class LocalSessionState(StrEnum):
    AVAILABLE = "available"
    LEASED = "leased"
    RETIRING = "retiring"
    UNAVAILABLE = "unavailable"


class FoundrySessionStatus(StrEnum):
    """Platform lifecycle status. ``UNKNOWN`` covers values added after this release."""

    CREATING = "creating"
    ACTIVE = "active"
    IDLE = "idle"
    UPDATING = "updating"
    FAILED = "failed"
    DELETING = "deleting"
    DELETED = "deleted"
    EXPIRED = "expired"
    UNKNOWN = "unknown"


# Statuses that mean the platform session no longer exists.
GONE_STATUSES = frozenset({FoundrySessionStatus.DELETED, FoundrySessionStatus.EXPIRED})
# Statuses that may serve requests once a local lease is held.
SCHEDULABLE_STATUSES = frozenset({FoundrySessionStatus.ACTIVE, FoundrySessionStatus.IDLE})
