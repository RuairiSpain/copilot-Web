"""Metrics, telemetry gating, OpenTelemetry sink and structured logging."""

from __future__ import annotations

import json
import logging
import sys
import types

import pytest
from opentelemetry import metrics as otel_metrics
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import InMemoryMetricReader

from hosted_agent_kit.adapters import otel
from hosted_agent_kit.logging_config import (
    JsonFormatter,
    configure_logging,
    correlation_id_var,
    hash_identifier,
    log_event,
    request_id_var,
)
from hosted_agent_kit.services.metrics import BUCKETS, GatedMetrics, InMemoryMetrics
from tests.conftest import make_harness, make_request


def drive(m: GatedMetrics | InMemoryMetrics, agent: str = "a") -> None:
    m.request_completed(agent, "ok", 0.3)
    m.queue_wait(agent, 0.02)
    m.queue_depth(agent, 4)
    m.sessions(agent, {("active", "available"): 2, ("active", "leased"): 1})
    m.session_created(agent)
    m.session_reused(agent)
    m.session_failed(agent)
    m.session_deleted(agent, "failed", "success")
    m.reconcile(agent, "success", 1.5)


def test_in_memory_metrics_cover_every_required_series() -> None:
    m = InMemoryMetrics()
    drive(m)
    snap = m.snapshot()["agents"]["a"]
    names = {x["name"] for k in ("counters", "histograms", "gauges") for x in snap[k]}
    assert names == {
        "pool_requests_total",
        "pool_request_duration_seconds",
        "pool_queue_wait_seconds",
        "pool_queue_depth",
        "pool_sessions",
        "pool_sessions_created_total",
        "pool_sessions_reused_total",
        "pool_failed_sessions_total",
        "pool_session_delete_total",
        "pool_reconcile_duration_seconds",
        "pool_reconcile_total",
    }


def test_counters_accumulate_and_histograms_bucket_correctly() -> None:
    m = InMemoryMetrics()
    for value in (0.003, 0.2, 400.0):
        m.queue_wait("a", value)
    m.session_created("a")
    m.session_created("a")
    snap = m.snapshot()["agents"]["a"]
    hist = snap["histograms"][0]
    assert hist["count"] == 3 and hist["sum"] == pytest.approx(400.203)
    assert snap["counters"][0]["value"] == 2
    text = m.prometheus_text()
    assert 'pool_queue_wait_seconds_bucket{agent="a",le="0.005"} 1' in text
    assert 'pool_queue_wait_seconds_bucket{agent="a",le="0.25"} 2' in text
    assert 'pool_queue_wait_seconds_bucket{agent="a",le="+Inf"} 3' in text
    assert len(BUCKETS) == 15


def test_sessions_gauge_replaces_previous_series_for_the_agent_only() -> None:
    m = InMemoryMetrics()
    m.sessions("a", {("active", "available"): 3})
    m.sessions("b", {("idle", "available"): 1})
    m.sessions("a", {("active", "leased"): 1})
    gauges = {
        (agent, x["labels"]["local_state"]): x["value"]
        for agent, data in m.snapshot()["agents"].items()
        for x in data["gauges"]
    }
    assert gauges == {("a", "leased"): 1.0, ("b", "available"): 1.0}


def test_prometheus_text_format_and_label_escaping() -> None:
    m = InMemoryMetrics()
    m.request_completed('we"ird\\agent', "ok", 1.0)
    m.queue_depth("a", 2)
    text = m.prometheus_text()
    assert 'pool_requests_total{agent="we\\"ird\\\\agent",outcome="ok"} 1' in text
    assert 'pool_queue_depth{agent="a"} 2' in text
    assert text.endswith("\n")


async def test_no_identity_ever_becomes_a_metric_dimension() -> None:
    h = make_harness({"coding-agent": {"mode": "stateful"}}, defaults={})
    await h.pool.execute(make_request("coding-agent", "secret-user-42"))
    rendered = json.dumps(h.metrics.snapshot()) + h.metrics.prometheus_text()
    assert "secret-user-42" not in rendered
    assert h.fake.created[0] not in rendered
    labels = {
        key
        for series in json.loads(json.dumps(h.metrics.snapshot()))["agents"][
            "coding-agent"
        ].values()
        for x in series
        for key in x["labels"]
    }
    assert labels <= {"outcome", "status", "local_state", "reason"}


def test_disabled_agent_emits_no_business_metrics() -> None:
    """A11."""
    store = InMemoryMetrics()
    gated = GatedMetrics([store], lambda agent: agent != "quiet")
    drive(gated, "quiet")
    drive(gated, "loud")
    assert set(store.snapshot()["agents"]) == {"loud"}


def test_gated_metrics_fan_out_to_every_sink() -> None:
    first, second = InMemoryMetrics(), InMemoryMetrics()
    drive(GatedMetrics([first, second], lambda _: True))
    assert first.snapshot() == second.snapshot() != {"agents": {}}


async def test_disabled_telemetry_keeps_operational_logs(caplog: pytest.LogCaptureFixture) -> None:
    caplog.set_level(logging.INFO, logger="hosted_agent_kit")
    h = make_harness({"a": {"mode": "stateless", "telemetry": {"enabled": False}}}, defaults={})
    await h.pool.execute(make_request("a"))
    assert h.metrics.snapshot()["agents"] == {}
    assert any(r.getMessage() == "session_created" for r in caplog.records)


def test_otel_metrics_record_through_the_global_provider(monkeypatch: pytest.MonkeyPatch) -> None:
    reader = InMemoryMetricReader()
    provider = MeterProvider(metric_readers=[reader])
    monkeypatch.setattr(otel_metrics, "get_meter", lambda name: provider.get_meter(name))
    sink = otel.OtelMetrics()
    drive(sink)  # type: ignore[arg-type]
    data = reader.get_metrics_data()
    assert data is not None
    exported = {
        metric.name
        for resource in data.resource_metrics
        for scope in resource.scope_metrics
        for metric in scope.metrics
    }
    assert {
        "pool_requests_total",
        "pool_request_duration_seconds",
        "pool_queue_wait_seconds",
        "pool_queue_depth",
        "pool_sessions",
        "pool_sessions_created_total",
        "pool_sessions_reused_total",
        "pool_failed_sessions_total",
        "pool_session_delete_total",
        "pool_reconcile_duration_seconds",
        "pool_reconcile_total",
    } <= exported


def test_azure_monitor_is_skipped_without_connection_string() -> None:
    assert otel.configure_azure_monitor(None) is False
    assert otel.configure_azure_monitor("") is False


def test_azure_monitor_missing_extra_is_reported(
    monkeypatch: pytest.MonkeyPatch, caplog: pytest.LogCaptureFixture
) -> None:
    monkeypatch.setitem(sys.modules, "azure.monitor.opentelemetry", None)
    assert otel.configure_azure_monitor("InstrumentationKey=x") is False
    assert any(r.getMessage() == "azure_monitor_unavailable" for r in caplog.records)


def test_azure_monitor_is_configured_when_available(monkeypatch: pytest.MonkeyPatch) -> None:
    calls: list[str] = []
    module = types.ModuleType("azure.monitor.opentelemetry")
    module.configure_azure_monitor = lambda connection_string: calls.append(connection_string)  # type: ignore[attr-defined]
    monkeypatch.setitem(sys.modules, "azure.monitor.opentelemetry", module)
    assert otel.configure_azure_monitor("InstrumentationKey=x") is True
    assert calls == ["InstrumentationKey=x"]


# ------------------------------------------------------------------ logging


def make_record(msg: str = "event", **fields: object) -> logging.LogRecord:
    record = logging.LogRecord("hosted_agent_kit.x", logging.INFO, __file__, 1, msg, (), None)
    record.fields = fields  # type: ignore[attr-defined]
    return record


def test_json_formatter_emits_required_fields() -> None:
    token_c = correlation_id_var.set("corr-1")
    token_r = request_id_var.set("req-1")
    try:
        payload = json.loads(JsonFormatter().format(make_record("session_failed", agent_name="a")))
    finally:
        correlation_id_var.reset(token_c)
        request_id_var.reset(token_r)
    assert payload["event"] == "session_failed"
    assert payload["correlation_id"] == "corr-1" and payload["request_id"] == "req-1"
    assert payload["agent_name"] == "a" and payload["level"] == "info"
    assert payload["timestamp"].endswith("+00:00")


def test_secrets_and_content_are_redacted() -> None:
    record = make_record(
        authorization="Bearer abc",
        access_token="t",
        prompt="tell me secrets",
        response="42",
        safe="ok",
    )
    payload = json.loads(JsonFormatter().format(record))
    assert payload["authorization"] == payload["access_token"] == "[redacted]"
    assert payload["prompt"] == payload["response"] == "[redacted]"
    assert payload["safe"] == "ok"


def test_reserved_field_names_cannot_overwrite_core_fields() -> None:
    payload = json.loads(JsonFormatter().format(make_record("e", event="spoofed", level="x")))
    assert payload["event"] == "e" and payload["field_event"] == "spoofed"
    assert payload["level"] == "info" and payload["field_level"] == "x"


def test_exceptions_are_serialised() -> None:
    try:
        raise ValueError("boom")
    except ValueError:
        record = logging.LogRecord("x", logging.ERROR, __file__, 1, "failed", (), sys.exc_info())
    payload = json.loads(JsonFormatter().format(record))
    assert payload["exception_type"] == "ValueError" and "boom" in payload["exception"]


def test_records_without_extra_fields_are_valid() -> None:
    record = logging.LogRecord("x", logging.INFO, __file__, 1, "plain", (), None)
    assert json.loads(JsonFormatter().format(record))["event"] == "plain"


def test_hash_identifier_is_stable_truncated_and_not_the_input() -> None:
    value = hash_identifier("session-123")
    assert value == hash_identifier("session-123") and len(value) == 12
    assert "session-123" not in value and value != hash_identifier("session-124")


def test_configure_logging_installs_json_handler_without_propagation() -> None:
    configure_logging("debug")
    logger = logging.getLogger("hosted_agent_kit")
    assert isinstance(logger.handlers[0].formatter, JsonFormatter)
    assert logger.level == logging.DEBUG and logger.propagate is False
    logger.propagate = True  # restore for caplog in other tests
    logger.handlers = []
    logger.setLevel(logging.NOTSET)


def test_log_event_passes_fields() -> None:
    records: list[logging.LogRecord] = []

    class Capture(logging.Handler):
        def emit(self, record: logging.LogRecord) -> None:
            records.append(record)

    logger = logging.getLogger("capture-test")
    logger.addHandler(Capture())
    logger.setLevel(logging.INFO)
    log_event(logger, "hello", level=logging.WARNING, a=1)
    assert records[0].levelno == logging.WARNING and records[0].fields == {"a": 1}  # type: ignore[attr-defined]
