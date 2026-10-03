"""OpenTelemetry metrics sink, compatible with Application Insights."""

from __future__ import annotations

import importlib
import logging
from collections.abc import Mapping

from opentelemetry import metrics

from hosted_agent_kit.domain.enums import CircuitState
from hosted_agent_kit.logging_config import log_event

logger = logging.getLogger(__name__)


class OtelMetrics:
    """Records the PRD metrics through the global OpenTelemetry meter provider."""

    def __init__(self, meter_name: str = "hosted_agent_kit") -> None:
        meter = metrics.get_meter(meter_name)
        self._requests = meter.create_counter("pool_requests_total")
        self._duration = meter.create_histogram("pool_request_duration_seconds", unit="s")
        self._queue_wait = meter.create_histogram("pool_queue_wait_seconds", unit="s")
        self._queue_depth = meter.create_gauge("pool_queue_depth")
        self._sessions = meter.create_gauge("pool_sessions")
        self._created = meter.create_counter("pool_sessions_created_total")
        self._reused = meter.create_counter("pool_sessions_reused_total")
        self._failed = meter.create_counter("pool_failed_sessions_total")
        self._deleted = meter.create_counter("pool_session_delete_total")
        self._reconcile_duration = meter.create_histogram(
            "pool_reconcile_duration_seconds", unit="s"
        )
        self._reconcile_total = meter.create_counter("pool_reconcile_total")
        self._circuit_state = meter.create_gauge("pool_circuit_state")
        self._circuit_transitions = meter.create_counter("pool_circuit_transitions_total")
        self._restored = meter.create_counter("pool_sessions_restored_total")
        self._quota_refused = meter.create_counter("pool_quota_refused_total")
        self._evicted = meter.create_counter("pool_sessions_evicted_total")
        self._counted = meter.create_gauge("pool_sessions_counted")

    def request_completed(self, agent: str, outcome: str, duration_seconds: float) -> None:
        attributes = {"agent": agent, "outcome": outcome}
        self._requests.add(1, attributes)
        self._duration.record(duration_seconds, attributes)

    def queue_wait(self, agent: str, seconds: float) -> None:
        self._queue_wait.record(seconds, {"agent": agent})

    def queue_depth(self, agent: str, depth: int) -> None:
        self._queue_depth.set(depth, {"agent": agent})

    def sessions(self, agent: str, counts: Mapping[tuple[str, str], int]) -> None:
        for (status, local_state), value in counts.items():
            self._sessions.set(
                value, {"agent": agent, "status": status, "local_state": local_state}
            )

    def session_created(self, agent: str) -> None:
        self._created.add(1, {"agent": agent})

    def session_reused(self, agent: str) -> None:
        self._reused.add(1, {"agent": agent})

    def session_failed(self, agent: str) -> None:
        self._failed.add(1, {"agent": agent})

    def session_deleted(self, agent: str, reason: str, outcome: str) -> None:
        self._deleted.add(1, {"agent": agent, "reason": reason, "outcome": outcome})

    def reconcile(self, agent: str, outcome: str, duration_seconds: float) -> None:
        self._reconcile_total.add(1, {"agent": agent, "outcome": outcome})
        self._reconcile_duration.record(duration_seconds, {"agent": agent})

    def circuit_state(self, agent: str, state: str) -> None:
        self._circuit_state.set(CircuitState(state).level, {"agent": agent})

    def circuit_transition(self, agent: str, to_state: str) -> None:
        self._circuit_transitions.add(1, {"agent": agent, "to": to_state})

    def session_restored(self, agent: str) -> None:
        self._restored.add(1, {"agent": agent})

    def quota_refused(self, agent: str, scope: str) -> None:
        self._quota_refused.add(1, {"agent": agent, "scope": scope})

    def session_evicted(self, agent: str) -> None:
        self._evicted.add(1, {"agent": agent})

    def sessions_counted(self, agent: str, count: int) -> None:
        self._counted.set(count, {"agent": agent})


def configure_azure_monitor(connection_string: str | None) -> bool:
    """Enable the Azure Monitor exporter when a connection string and the extra are present."""
    if not connection_string:
        return False
    try:
        module = importlib.import_module("azure.monitor.opentelemetry")
    except ImportError:
        log_event(
            logger,
            "azure_monitor_unavailable",
            level=logging.WARNING,
            hint="install the azure-monitor extra to export to Application Insights",
        )
        return False
    module.configure_azure_monitor(connection_string=connection_string)
    return True
