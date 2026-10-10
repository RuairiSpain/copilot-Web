"""Decision log and Prometheus metrics. Label values come only from the catalog, so cardinality is bounded."""
from __future__ import annotations

import atexit
import hashlib
import json
import logging
import logging.handlers
import queue
from pathlib import Path
from typing import Any

from prometheus_client import CollectorRegistry, Counter, Histogram, generate_latest

log = logging.getLogger("decision_router")
_LATENCY_BUCKETS_MS = (25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000, 60000, 120000)


class Telemetry:
    def __init__(self, decision_log_path: Path | None = None, log_prompts: bool = False):
        self.log_prompts = log_prompts
        self.registry = CollectorRegistry()
        self.requests = Counter("router_requests", "Requests by mode and outcome",
                                ["mode", "outcome"], registry=self.registry)
        self.decisions = Counter("router_decisions", "Top-ranked model chosen by Decision-1",
                                 ["mode", "model"], registry=self.registry)
        self.served = Counter("router_served", "Model that produced the response",
                              ["mode", "model", "fallback"], registry=self.registry)
        self.decision_latency = Histogram("router_decision_latency_ms", "Decision-1 latency",
                                          ["mode"], buckets=_LATENCY_BUCKETS_MS, registry=self.registry)
        self.total_latency = Histogram("router_total_latency_ms", "End-to-end latency",
                                       ["mode"], buckets=_LATENCY_BUCKETS_MS, registry=self.registry)
        self.tokens = Counter("router_tokens", "Tokens reported by the served model",
                              ["model", "kind"], registry=self.registry)

        self._decision_log = logging.getLogger("decision_router.decisions")
        self._decision_log.setLevel(logging.INFO)
        self._listener: logging.handlers.QueueListener | None = None
        self._queue_handler: logging.Handler | None = None
        if decision_log_path:
            decision_log_path.parent.mkdir(parents=True, exist_ok=True)
            file_handler = logging.FileHandler(decision_log_path, encoding="utf-8")
            file_handler.setFormatter(logging.Formatter("%(message)s"))
            records: queue.SimpleQueue = queue.SimpleQueue()
            # The file is written on a background thread so the event loop never blocks on disk.
            self._queue_handler = logging.handlers.QueueHandler(records)
            self._decision_log.addHandler(self._queue_handler)
            self._listener = logging.handlers.QueueListener(records, file_handler)
            self._listener.start()
            atexit.register(self.close)

    @staticmethod
    def prompt_fingerprint(body: dict[str, Any]) -> str:
        text = json.dumps(body.get("messages", []), sort_keys=True, ensure_ascii=False)
        return hashlib.sha256(text.encode("utf-8")).hexdigest()

    def record(self, event: dict[str, Any], body: dict[str, Any]) -> None:
        """Write one decision-log line and update metrics. Prompt text is left out unless configured."""
        mode = event.get("routing_mode") or "unknown"
        self.requests.labels(mode, event.get("outcome", "unknown")).inc()
        decision = event.get("decision") or {}
        if decision.get("ranking"):
            self.decisions.labels(mode, decision["ranking"][0]).inc()
            self.decision_latency.labels(mode).observe(decision.get("latency_ms") or 0.0)
            decision_usage = decision.get("usage") or {}
            decision_tokens = decision_usage.get("prompt_tokens", decision_usage.get("input_tokens"))
            if isinstance(decision_tokens, int):
                self.tokens.labels("decision-1", "prompt_tokens").inc(decision_tokens)
        if event.get("served_model"):
            self.served.labels(mode, event["served_model"], str(bool(event.get("fallback_used"))).lower()).inc()
            usage = event.get("usage") or {}
            for kind in ("prompt_tokens", "completion_tokens"):
                if usage.get(kind):
                    self.tokens.labels(event["served_model"], kind).inc(int(usage[kind]))
        if event.get("total_latency_ms") is not None:
            self.total_latency.labels(mode).observe(event["total_latency_ms"])

        line = dict(event, event="routing_decision", messages_sha256=self.prompt_fingerprint(body))
        if self.log_prompts:
            line["messages"] = body.get("messages")
        text = json.dumps(line, separators=(",", ":"), sort_keys=True, ensure_ascii=False)
        self._decision_log.info(text)

    def render(self) -> bytes:
        return generate_latest(self.registry)

    def close(self) -> None:
        if self._queue_handler is not None:
            self._decision_log.removeHandler(self._queue_handler)
            self._queue_handler = None
        if self._listener is not None:
            self._listener.stop()
            self._listener = None
