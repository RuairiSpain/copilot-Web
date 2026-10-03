"""The result of one synchronisation of an agent pool with Foundry."""

from __future__ import annotations

from dataclasses import dataclass, field


@dataclass
class ReconcileReport:
    agent_name: str
    outcome: str = "success"
    discovered: int = 0
    removed: int = 0
    failed_sessions: int = 0
    uncertain: int = 0
    warm_created: int = 0
    drained: int = 0
    skipped: bool = False
    duration_seconds: float = 0.0
    errors: list[str] = field(default_factory=list)
    attempted: set[str] = field(default_factory=set)
