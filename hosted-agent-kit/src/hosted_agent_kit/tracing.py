"""OpenTelemetry spans for the steps of a call. Without an SDK configured they cost nothing.

Spans: ``hack.call`` (the whole call), ``hack.schedule`` (finding a session, queue wait
included), ``hack.create_session`` and ``hack.invoke``. Attributes name the agent and protocol,
never a user or a message. The W3C ``traceparent`` is sent to Foundry with each invocation, so the
agent's own trace joins the caller's.
"""

from __future__ import annotations

from collections.abc import Mapping
from contextlib import AbstractContextManager
from typing import Any

from opentelemetry import trace
from opentelemetry.trace import Span
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

_tracer = trace.get_tracer("hosted_agent_kit")
_propagator = TraceContextTextMapPropagator()


def span(name: str, attributes: Mapping[str, Any] | None = None) -> AbstractContextManager[Span]:
    """A span that is current inside the block. Exceptions are recorded and re-raised."""
    return _tracer.start_as_current_span(name, attributes=dict(attributes or {}))


def inject_trace_context(headers: dict[str, str]) -> None:
    """Add ``traceparent`` (and ``tracestate``) for the current span. Never adds baggage."""
    _propagator.inject(headers)
