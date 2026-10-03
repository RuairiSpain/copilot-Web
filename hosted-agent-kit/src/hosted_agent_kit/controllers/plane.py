"""The control plane: what the controllers share.

It holds the controller manager, the event recorder and the report of any sync in progress. The
request path and the controllers meet here and in the session store, nowhere else.
"""

from __future__ import annotations

from hosted_agent_kit.config.holder import ConfigHolder
from hosted_agent_kit.controllers.reports import ReconcileReport
from hosted_agent_kit.controllers.runtime import ControllerManager
from hosted_agent_kit.ports.registry import SessionRegistry
from hosted_agent_kit.services.clock import Clock
from hosted_agent_kit.services.events import EventRecorder
from hosted_agent_kit.services.poolstore import PoolStore


class ControlPlane:
    def __init__(
        self,
        *,
        registry: SessionRegistry,
        clock: Clock,
        holder: ConfigHolder,
        events: EventRecorder | None = None,
        manager: ControllerManager | None = None,
    ) -> None:
        self.registry = registry
        self.clock = clock
        self.events = events or EventRecorder(clock)
        self.manager = manager or ControllerManager(clock, registry)
        self.pools = PoolStore(holder, clock.now, self.events)
        self._reports: dict[str, ReconcileReport] = {}

    def begin_sync(self, report: ReconcileReport) -> None:
        self._reports[report.agent_name] = report

    def end_sync(self, agent_name: str) -> None:
        self._reports.pop(agent_name, None)

    def report(self, agent_name: str) -> ReconcileReport | None:
        """The report of the sync running for this agent, if any."""
        return self._reports.get(agent_name)
