"""Tests for calibration descriptions, name conflicts and version pinning.

These three exist so an MCP client can browse what the engine knows, add to it
without trampling what is already there, and hold a caller on a known-good fit.
"""

from __future__ import annotations

from typing import Any

import pytest
from fastapi.testclient import TestClient

from app.descriptions import compose_description, summarise_training_data
from app.schemas import FitMetrics, TrainSample
from tests.test_endpoints import classification_payload, numeric_payload

TEXT_FIELDS = ("text", "input", "prompt")


def samples(rows: list[dict[str, Any]]) -> list[TrainSample]:
    return [TrainSample(**row) for row in rows]


# ----------------------------------------------------------------------------
# Generated descriptions
# ----------------------------------------------------------------------------
def test_enum_description_names_the_options_and_the_majority_share():
    rows = [{"input": {"text": f"q{i}"}, "label": ["low", "high"][i % 4 == 0]} for i in range(20)]
    facts = summarise_training_data(
        samples(rows), decision_type="enum", class_names=["low", "high"], text_fields=TEXT_FIELDS
    )
    text = compose_description(facts)

    assert "Chooses one of 2 options" in text
    assert "low" in text and "high" in text
    assert "20 labelled examples" in text
    # The majority share is the number that tells a reader whether it is useful.
    assert "75%" in text
    assert "no better than guessing" in text


def test_a_long_class_list_is_truncated_rather_than_dumped():
    names = [f"model-{i}" for i in range(18)]
    rows = [{"input": {"text": f"q{i}"}, "label": names[i % 18]} for i in range(180)]
    facts = summarise_training_data(
        samples(rows), decision_type="enum", class_names=names, text_fields=TEXT_FIELDS
    )
    text = compose_description(facts)
    assert "and 12 more" in text
    assert "model-17" not in text


def test_a_rare_class_is_called_out():
    rows = [{"input": {"text": f"q{i}"}, "label": "common"} for i in range(50)]
    rows += [{"input": {"text": "rare one"}, "label": "rare"}] * 3
    facts = summarise_training_data(
        samples(rows), decision_type="enum", class_names=None, text_fields=TEXT_FIELDS
    )
    text = compose_description(facts)
    assert "only 3 examples" in text
    assert "least trustworthy" in text


def test_numeric_description_gives_the_label_range():
    rows = [{"input": {"text": f"q{i}"}, "label": 100.0 + i} for i in range(10)]
    facts = summarise_training_data(
        samples(rows), decision_type="numeric", class_names=None, text_fields=TEXT_FIELDS
    )
    text = compose_description(facts)
    assert "Estimates a number" in text
    assert "100" in text and "109" in text


def test_metrics_are_folded_in_when_the_fit_reports_them():
    rows = [{"input": {"text": f"q{i}"}, "label": i % 2 == 0} for i in range(10)]
    facts = summarise_training_data(
        samples(rows), decision_type="boolean", class_names=None, text_fields=TEXT_FIELDS
    )
    text = compose_description(
        facts,
        FitMetrics(split="validation", num_samples=5, accuracy=0.82, expected_calibration_error=0.031),
    )
    assert "On the validation split" in text
    assert "accuracy 82%" in text
    assert "calibration error 0.031" in text


def test_the_example_input_is_quoted_and_trimmed():
    long_text = "word " * 200
    facts = summarise_training_data(
        samples([{"input": {"text": long_text}, "label": True}] * 4),
        decision_type="boolean",
        class_names=None,
        text_fields=TEXT_FIELDS,
    )
    text = compose_description(facts)
    assert "Example input:" in text
    assert "…" in text, "a 1000-character prompt must not be pasted whole into a listing"


def test_generated_description_reaches_the_listing(client: TestClient, model):
    client.post(
        "/posthoc_train",
        json={
            "decision_type": "boolean",
            "scenario": "described",
            "training_data": classification_payload("boolean", 2),
        },
    )
    row = client.get("/scenarios").json()["scenarios"][0]
    assert row["description"]
    assert "yes/no" in row["description"]
    assert row["num_versions"] == 1


def test_a_supplied_description_wins_over_the_generated_one(client: TestClient, model):
    mine = "Decides whether an invoice needs a human to look at it."
    body = client.post(
        "/posthoc_train",
        json={
            "decision_type": "boolean",
            "scenario": "invoices",
            "description": mine,
            "training_data": classification_payload("boolean", 2),
        },
    ).json()
    assert body["description"] == mine
    assert client.get("/scenarios").json()["scenarios"][0]["description"] == mine


def test_no_llm_call_is_made_unless_one_is_configured(monkeypatch: pytest.MonkeyPatch, settings):
    """The default path must not touch the network."""
    import app.descriptions as descriptions

    def explode(*args, **kwargs):  # pragma: no cover - must not run
        raise AssertionError("descriptions must not call out when JEV_DESCRIBE_LLM_URL is unset")

    monkeypatch.setattr(descriptions.urllib.request, "urlopen", explode)
    text = descriptions.build_description(
        samples([{"input": {"text": "hello"}, "label": True}] * 4),
        decision_type="boolean",
        class_names=None,
        metrics=None,
        settings=settings,
    )
    assert text.startswith("Answers a yes/no question")


def test_a_failing_llm_leaves_the_computed_description_intact(
    monkeypatch: pytest.MonkeyPatch, settings
):
    import app.descriptions as descriptions

    monkeypatch.setattr(settings, "describe_llm_url", "http://127.0.0.1:9/chat")
    text = descriptions.build_description(
        samples([{"input": {"text": "hello"}, "label": True}] * 4),
        decision_type="boolean",
        class_names=None,
        metrics=None,
        settings=settings,
    )
    assert text.startswith("Answers a yes/no question")


# ----------------------------------------------------------------------------
# Name conflicts
# ----------------------------------------------------------------------------
def train(client: TestClient, scenario: str, **extra: Any) -> dict[str, Any]:
    response = client.post(
        "/posthoc_train",
        json={
            "decision_type": "boolean",
            "scenario": scenario,
            "training_data": classification_payload("boolean", 2),
            **extra,
        },
    )
    assert response.status_code == 200, response.text
    return response.json()


def test_default_conflict_policy_supersedes_and_keeps_the_old_version(client: TestClient, model):
    first = train(client, "taken")
    second = train(client, "taken")

    assert second["scenario"] == "taken"
    assert second["requested_scenario"] is None
    assert second["calibration_version"] != first["calibration_version"]

    versions = client.get("/scenarios/taken/boolean/versions").json()
    assert versions["current_version"] == second["calibration_version"]
    assert {v["version"] for v in versions["versions"]} == {
        first["calibration_version"],
        second["calibration_version"],
    }
    assert sum(v["is_current"] for v in versions["versions"]) == 1


def test_new_scenario_policy_increments_the_name_and_leaves_the_original_alone(
    client: TestClient, model
):
    first = train(client, "router")
    second = train(client, "router", on_conflict="new_scenario")
    third = train(client, "router", on_conflict="new_scenario")

    assert second["scenario"] == "router-2"
    assert second["requested_scenario"] == "router"
    assert third["scenario"] == "router-3"

    # The original still serves its own first version.
    versions = client.get("/scenarios/router/boolean/versions").json()
    assert versions["current_version"] == first["calibration_version"]
    assert len(versions["versions"]) == 1

    names = {row["scenario"] for row in client.get("/scenarios").json()["scenarios"]}
    assert names == {"router", "router-2", "router-3"}


def test_reject_policy_refuses_a_taken_name(client: TestClient, model):
    train(client, "once")
    response = client.post(
        "/posthoc_train",
        json={
            "decision_type": "boolean",
            "scenario": "once",
            "on_conflict": "reject",
            "training_data": classification_payload("boolean", 2),
        },
    )
    assert response.status_code == 409
    assert response.json()["error"] == "scenario_exists"


def test_a_free_name_is_unaffected_by_the_conflict_policy(client: TestClient, model):
    body = train(client, "fresh", on_conflict="new_scenario")
    assert body["scenario"] == "fresh"
    assert body["requested_scenario"] is None


def test_the_same_name_under_a_different_decision_type_is_not_a_conflict(
    client: TestClient, model
):
    train(client, "shared")
    response = client.post(
        "/posthoc_train",
        json={
            "decision_type": "numeric",
            "scenario": "shared",
            "on_conflict": "reject",
            "training_data": numeric_payload(),
        },
    )
    assert response.status_code == 200, response.text
    assert response.json()["scenario"] == "shared"


# ----------------------------------------------------------------------------
# Version pinning
# ----------------------------------------------------------------------------
def test_a_decision_can_be_pinned_to_an_older_version(client: TestClient, model):
    first = train(client, "pinned")
    second = train(client, "pinned")
    probe = {"decision_type": "boolean", "scenario": "pinned", "data": {"text": "case number 3"}}

    current = client.post("/decision", json=probe).json()
    older = client.post("/decision", json={**probe, "calibration_version": first["calibration_version"]}).json()

    assert current["calibration_version"] == second["calibration_version"]
    assert older["calibration_version"] == first["calibration_version"]
    assert older["calibrated"] is True


def test_pinning_a_version_that_was_never_written_is_an_error_not_a_fallback(
    client: TestClient, model
):
    train(client, "pinned")
    response = client.post(
        "/decision",
        json={
            "decision_type": "boolean",
            "scenario": "pinned",
            "data": {"text": "x"},
            "calibration_version": "20200101T000000Z-deadbeef",
        },
    )
    assert response.status_code == 503
    assert "no version" in response.json()["detail"]


def test_versions_endpoint_404s_for_an_unknown_calibration(client: TestClient, model):
    response = client.get("/scenarios/nope/boolean/versions")
    assert response.status_code == 404
    assert response.json()["error"] == "calibration_not_found"


# ----------------------------------------------------------------------------
# The MCP layer's own guard
# ----------------------------------------------------------------------------
def test_the_mcp_decide_tool_refuses_an_uncalibrated_scenario(monkeypatch: pytest.MonkeyPatch):
    """The HTTP API may fall back to the raw model; the agent-facing tool must not.

    A human reading `calibrated: false` in a response will notice. An agent
    that asked for a named calibration will not, and will treat the number as
    if it meant something.
    """
    from mcp_server import server as mcp

    monkeypatch.setattr(
        mcp,
        "call",
        lambda *a, **k: {
            "value": True, "probability": 0.91, "calibrated": False,
            "calibration_version": None, "latency_ms": 1.0, "label": None,
        },
    )
    with pytest.raises(mcp.EngineError, match="no calibration"):
        mcp.decide("never-trained", "boolean", "anything")


def test_the_mcp_decide_tool_passes_a_calibrated_answer_through(monkeypatch: pytest.MonkeyPatch):
    from mcp_server import server as mcp

    monkeypatch.setattr(
        mcp,
        "call",
        lambda *a, **k: {
            "value": True, "probability": 0.91, "calibrated": True,
            "calibration_version": "v1", "latency_ms": 1.234, "label": None,
        },
    )
    out = mcp.decide("trained", "boolean", "anything")
    assert out["value"] is True
    assert out["calibration_version"] == "v1"
    assert out["latency_ms"] == 1.2


def test_the_mcp_train_tool_surfaces_a_clamped_fit_as_a_warning(monkeypatch: pytest.MonkeyPatch):
    """A fit with no signal has to be visible to the agent that just made it."""
    from mcp_server import server as mcp

    monkeypatch.setattr(
        mcp,
        "call",
        lambda *a, **k: {
            "scenario": "s", "decision_type": "boolean", "calibration_version": "v1",
            "num_samples": 50, "temperature_clamped": True, "metrics_after": {},
            "description": "d",
        },
    )
    out = mcp.train_calibration("s", "boolean", [])
    assert "little signal" in out["warning"]
