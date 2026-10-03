"""Metric recording: in-memory store, per-agent gating and Prometheus text rendering."""

from __future__ import annotations

from collections.abc import Callable, Iterable, Mapping
from typing import Any

from hosted_agent_kit.domain.enums import CircuitState
from hosted_agent_kit.ports.metrics import MetricsRecorder

BUCKETS: tuple[float, ...] = (
    0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0, 120.0, 300.0,
)  # fmt: skip

Labels = tuple[tuple[str, str], ...]


def _labels(**labels: str) -> Labels:
    return tuple(sorted(labels.items()))


class _Histogram:
    def __init__(self) -> None:
        self.count = 0
        self.total = 0.0
        self.buckets = [0] * len(BUCKETS)

    def observe(self, value: float) -> None:
        self.count += 1
        self.total += value
        for index, bound in enumerate(BUCKETS):
            if value <= bound:
                self.buckets[index] += 1


class InMemoryMetrics:
    """Backs ``GET /v1/admin/metrics`` and the optional Prometheus endpoint."""

    def __init__(self) -> None:
        self._counters: dict[tuple[str, str, Labels], float] = {}
        self._histograms: dict[tuple[str, str, Labels], _Histogram] = {}
        self._gauges: dict[tuple[str, str, Labels], float] = {}

    def _inc(self, name: str, agent: str, **labels: str) -> None:
        key = (name, agent, _labels(**labels))
        self._counters[key] = self._counters.get(key, 0.0) + 1

    def _observe(self, name: str, agent: str, value: float, **labels: str) -> None:
        self._histograms.setdefault((name, agent, _labels(**labels)), _Histogram()).observe(value)

    def request_completed(self, agent: str, outcome: str, duration_seconds: float) -> None:
        self._inc("pool_requests_total", agent, outcome=outcome)
        self._observe("pool_request_duration_seconds", agent, duration_seconds, outcome=outcome)

    def queue_wait(self, agent: str, seconds: float) -> None:
        self._observe("pool_queue_wait_seconds", agent, seconds)

    def queue_depth(self, agent: str, depth: int) -> None:
        self._gauges[("pool_queue_depth", agent, ())] = float(depth)

    def sessions(self, agent: str, counts: Mapping[tuple[str, str], int]) -> None:
        self._gauges = {k: v for k, v in self._gauges.items() if not _is_sessions_of(k, agent)}
        for (status, local_state), value in counts.items():
            key = ("pool_sessions", agent, _labels(status=status, local_state=local_state))
            self._gauges[key] = float(value)

    def session_created(self, agent: str) -> None:
        self._inc("pool_sessions_created_total", agent)

    def session_reused(self, agent: str) -> None:
        self._inc("pool_sessions_reused_total", agent)

    def session_failed(self, agent: str) -> None:
        self._inc("pool_failed_sessions_total", agent)

    def session_deleted(self, agent: str, reason: str, outcome: str) -> None:
        self._inc("pool_session_delete_total", agent, reason=reason, outcome=outcome)

    def reconcile(self, agent: str, outcome: str, duration_seconds: float) -> None:
        self._inc("pool_reconcile_total", agent, outcome=outcome)
        self._observe("pool_reconcile_duration_seconds", agent, duration_seconds)

    def circuit_state(self, agent: str, state: str) -> None:
        self._gauges[("pool_circuit_state", agent, ())] = float(CircuitState(state).level)

    def circuit_transition(self, agent: str, to_state: str) -> None:
        self._inc("pool_circuit_transitions_total", agent, to=to_state)

    def session_restored(self, agent: str) -> None:
        self._inc("pool_sessions_restored_total", agent)

    def snapshot(self) -> dict[str, Any]:
        """JSON operational snapshot grouped by agent name."""
        agents: dict[str, dict[str, Any]] = {}

        def bucket(agent: str) -> dict[str, Any]:
            return agents.setdefault(agent, {"counters": [], "histograms": [], "gauges": []})

        for (name, agent, labels), value in sorted(self._counters.items()):
            bucket(agent)["counters"].append({"name": name, "labels": dict(labels), "value": value})
        for (name, agent, labels), hist in sorted(self._histograms.items()):
            bucket(agent)["histograms"].append(
                {"name": name, "labels": dict(labels), "count": hist.count, "sum": hist.total}
            )
        for (name, agent, labels), value in sorted(self._gauges.items()):
            bucket(agent)["gauges"].append({"name": name, "labels": dict(labels), "value": value})
        return {"agents": agents}

    def prometheus_text(self) -> str:
        """Render the Prometheus text exposition format."""
        lines: list[str] = []
        for (name, agent, labels), value in sorted(self._counters.items()):
            lines.append(f"{name}{_render(agent, labels)} {value:g}")
        for (name, agent, labels), hist in sorted(self._histograms.items()):
            for bound, count in zip(BUCKETS, hist.buckets, strict=True):
                lines.append(f"{name}_bucket{_render(agent, labels, le=f'{bound:g}')} {count}")
            lines.append(f"{name}_bucket{_render(agent, labels, le='+Inf')} {hist.count}")
            lines.append(f"{name}_sum{_render(agent, labels)} {hist.total:g}")
            lines.append(f"{name}_count{_render(agent, labels)} {hist.count}")
        for (name, agent, labels), value in sorted(self._gauges.items()):
            lines.append(f"{name}{_render(agent, labels)} {value:g}")
        return "\n".join(lines) + "\n"


def _is_sessions_of(key: tuple[str, str, Labels], agent: str) -> bool:
    return key[0] == "pool_sessions" and key[1] == agent


def _escape(value: str) -> str:
    return value.replace("\\", "\\\\").replace('"', '\\"').replace("\n", "\\n")


def _render(agent: str, labels: Labels, **extra: str) -> str:
    pairs = [("agent", agent), *labels, *extra.items()]
    return "{" + ",".join(f'{k}="{_escape(v)}"' for k, v in pairs) + "}"


class GatedMetrics:
    """Fan-out to sinks, dropping business metrics for agents with telemetry disabled."""

    def __init__(self, sinks: Iterable[MetricsRecorder], enabled: Callable[[str], bool]) -> None:
        self._sinks = list(sinks)
        self._enabled = enabled

    def _emit(self, agent: str, call: Callable[[MetricsRecorder], None]) -> None:
        if self._enabled(agent):
            for sink in self._sinks:
                call(sink)

    def request_completed(self, agent: str, outcome: str, duration_seconds: float) -> None:
        self._emit(agent, lambda s: s.request_completed(agent, outcome, duration_seconds))

    def queue_wait(self, agent: str, seconds: float) -> None:
        self._emit(agent, lambda s: s.queue_wait(agent, seconds))

    def queue_depth(self, agent: str, depth: int) -> None:
        self._emit(agent, lambda s: s.queue_depth(agent, depth))

    def sessions(self, agent: str, counts: Mapping[tuple[str, str], int]) -> None:
        self._emit(agent, lambda s: s.sessions(agent, counts))

    def session_created(self, agent: str) -> None:
        self._emit(agent, lambda s: s.session_created(agent))

    def session_reused(self, agent: str) -> None:
        self._emit(agent, lambda s: s.session_reused(agent))

    def session_failed(self, agent: str) -> None:
        self._emit(agent, lambda s: s.session_failed(agent))

    def session_deleted(self, agent: str, reason: str, outcome: str) -> None:
        self._emit(agent, lambda s: s.session_deleted(agent, reason, outcome))

    def reconcile(self, agent: str, outcome: str, duration_seconds: float) -> None:
        self._emit(agent, lambda s: s.reconcile(agent, outcome, duration_seconds))

    def circuit_state(self, agent: str, state: str) -> None:
        self._emit(agent, lambda s: s.circuit_state(agent, state))

    def circuit_transition(self, agent: str, to_state: str) -> None:
        self._emit(agent, lambda s: s.circuit_transition(agent, to_state))

    def session_restored(self, agent: str) -> None:
        self._emit(agent, lambda s: s.session_restored(agent))
