"""End-to-end tests over the HTTP surface.

Each decision type is trained through ``POST /posthoc_train`` and then queried
through ``POST /decision`` against the same running app, so the request
schemas, the engine, the registry serialisation and the response contract are
all exercised together. The base model is the deterministic ``hash`` backend,
which is what makes the assertions repeatable.
"""

from __future__ import annotations

from collections.abc import Iterator
from typing import Any

import numpy as np
import pytest
from fastapi.testclient import TestClient

from app import model as model_module
from config.settings import reset_settings_cache

SAMPLE_COUNT = 120


def _inputs(count: int = SAMPLE_COUNT, prefix: str = "case") -> list[dict[str, Any]]:
    return [{"text": f"{prefix} number {index}"} for index in range(count)]


def _model_argmax(payloads: list[dict[str, Any]], num_classes: int) -> np.ndarray:
    """What the frozen model would predict, so labels can be made to correlate."""
    logits = model_module.get_model().logits(payloads)[:, :num_classes]
    return logits.argmax(axis=1)


def classification_payload(
    decision_type: str,
    num_classes: int,
    *,
    noise: float = 0.2,
    seed: int = 0,
    label_names: list[str] | None = None,
    invert: bool = False,
    prefix: str = "case",
) -> list[dict[str, Any]]:
    """Training rows whose labels track the model, with a fraction flipped.

    Perfectly separable labels would make calibration trivial (and isotonic
    degenerate); a noisy majority is the realistic case and the one where
    temperature scaling has something to do.
    """
    payloads = _inputs(prefix=prefix)
    targets = _model_argmax(payloads, num_classes)
    rng = np.random.default_rng(seed)
    rows = []
    for payload, target in zip(payloads, targets, strict=True):
        index = int(target)
        if rng.random() < noise:
            index = int(rng.integers(0, num_classes))
        if invert:
            index = num_classes - 1 - index
        label: Any = bool(index) if decision_type == "boolean" else index
        if label_names is not None:
            label = label_names[index]
        rows.append({"input": payload, "label": label})
    return rows


def numeric_payload(*, seed: int = 0, prefix: str = "quote") -> list[dict[str, Any]]:
    """Numeric labels driven by the model's own score, plus noise."""
    payloads = _inputs(prefix=prefix)
    logits = model_module.get_model().logits(payloads)
    probs = np.exp(logits - logits.max(axis=1, keepdims=True))
    probs /= probs.sum(axis=1, keepdims=True)
    score = probs @ np.arange(probs.shape[1]) / (probs.shape[1] - 1)
    rng = np.random.default_rng(seed)
    # Deliberately non-linear in the score: a linear relationship would be
    # captured perfectly by the uncalibrated min-max baseline, which is exactly
    # the case binned calibration is not needed for.
    values = 100.0 + 50.0 * score**3 + rng.normal(0.0, 2.0, size=score.shape)
    return [
        {"input": payload, "label": float(value)} for payload, value in zip(payloads, values, strict=True)
    ]


@pytest.fixture
def trained_boolean(client: TestClient, model) -> dict[str, Any]:
    response = client.post(
        "/posthoc_train",
        json={
            "decision_type": "boolean",
            "scenario": "loan-approval",
            "training_data": classification_payload("boolean", 2),
        },
    )
    assert response.status_code == 200, response.text
    return response.json()


# ----------------------------------------------------------------------------
# Boolean
# ----------------------------------------------------------------------------
def test_boolean_train_reports_what_it_fitted(trained_boolean: dict[str, Any]):
    body = trained_boolean
    assert body["status"] == "ok"
    assert body["decision_type"] == "boolean"
    assert body["num_classes"] == 2
    assert body["num_samples"] == SAMPLE_COUNT
    assert body["num_train_samples"] + body["num_validation_samples"] == SAMPLE_COUNT
    assert body["num_validation_samples"] > 0
    assert body["temperature"] > 0
    assert set(body["artifacts"]) == {"temperature.json", "isotonic.json"}
    assert body["metrics_after"]["split"] == "validation"
    assert body["duration_ms"] >= 0


def test_boolean_decision_is_typed_and_bounded(client: TestClient, trained_boolean):
    response = client.post(
        "/decision",
        json={
            "decision_type": "boolean",
            "scenario": "loan-approval",
            "data": {"text": "case number 7"},
        },
    )
    assert response.status_code == 200, response.text
    body = response.json()
    assert isinstance(body["value"], bool)
    assert 0.0 <= body["probability"] <= 1.0
    assert body["calibrated"] is True
    assert body["calibration_version"] == trained_boolean["calibration_version"]
    assert body["latency_ms"] >= 0


def test_boolean_confidence_is_never_below_a_half(client: TestClient, trained_boolean):
    """With the default 'confidence' semantics, the reported probability backs the value."""
    for index in range(10):
        body = client.post(
            "/decision",
            json={
                "decision_type": "boolean",
                "scenario": "loan-approval",
                "data": {"text": f"case number {index}"},
            },
        ).json()
        assert body["probability"] >= 0.5 - 1e-9


def test_boolean_threshold_override_moves_the_decision(client: TestClient, trained_boolean):
    payload = {
        "decision_type": "boolean",
        "scenario": "loan-approval",
        "data": {"text": "case number 3"},
    }
    always_true = client.post("/decision", json={**payload, "threshold": 0.0}).json()
    always_false = client.post("/decision", json={**payload, "threshold": 1.0}).json()
    assert always_true["value"] is True
    assert always_false["value"] is False


def test_wrapped_and_flat_inputs_agree(client: TestClient, trained_boolean):
    flat = client.post(
        "/decision",
        json={"decision_type": "boolean", "scenario": "loan-approval", "data": {"text": "case number 5"}},
    ).json()
    wrapped = client.post(
        "/decision",
        json={
            "decision_type": "boolean",
            "scenario": "loan-approval",
            "data": {"input": {"text": "case number 5"}},
        },
    ).json()
    assert flat["value"] == wrapped["value"]
    assert flat["probability"] == pytest.approx(wrapped["probability"])


# ----------------------------------------------------------------------------
# Enum
# ----------------------------------------------------------------------------
def test_enum_end_to_end_with_class_names(client: TestClient, model):
    names = ["reject", "review", "approve"]
    train = client.post(
        "/posthoc_train",
        json={
            "decision_type": "enum",
            "scenario": "triage",
            "class_names": names,
            "training_data": classification_payload("enum", 3, label_names=names, seed=1),
        },
    )
    assert train.status_code == 200, train.text
    assert train.json()["class_names"] == names
    assert train.json()["num_classes"] == 3

    response = client.post(
        "/decision",
        json={
            "decision_type": "enum",
            "scenario": "triage",
            "data": {"text": "case number 11"},
            "include_probabilities": True,
        },
    )
    assert response.status_code == 200, response.text
    body = response.json()
    assert isinstance(body["value"], int) and 0 <= body["value"] < 3
    assert body["label"] in names
    assert body["label"] == names[body["value"]]
    assert sum(body["probabilities"].values()) == pytest.approx(1.0)
    assert body["probability"] == pytest.approx(max(body["probabilities"].values()))


def test_enum_accepts_integer_labels(client: TestClient, model):
    response = client.post(
        "/posthoc_train",
        json={
            "decision_type": "enum",
            "scenario": "severity",
            "training_data": classification_payload("enum", 3, seed=2),
        },
    )
    assert response.status_code == 200, response.text
    assert response.json()["class_names"] is None


def test_enum_rejects_more_classes_than_the_model_exposes(client: TestClient, model):
    names = [f"class{index}" for index in range(5)]  # model exposes 3 logits
    response = client.post(
        "/posthoc_train",
        json={
            "decision_type": "enum",
            "scenario": "too-wide",
            "class_names": names,
            "training_data": [
                {"input": {"text": f"row {index}"}, "label": names[index % 5]} for index in range(40)
            ],
        },
    )
    assert response.status_code == 409
    assert response.json()["error"] == "model_mismatch"


def test_enum_rejects_mixed_label_styles(client: TestClient, model):
    rows = classification_payload("enum", 3, seed=3)
    rows[0]["label"] = "approve"
    response = client.post(
        "/posthoc_train",
        json={"decision_type": "enum", "scenario": "mixed", "training_data": rows},
    )
    assert response.status_code == 400
    assert "names or all indices" in response.json()["detail"]


# ----------------------------------------------------------------------------
# Numeric
# ----------------------------------------------------------------------------
def test_numeric_end_to_end(client: TestClient, model):
    train = client.post(
        "/posthoc_train",
        json={
            "decision_type": "numeric",
            "scenario": "price-estimate",
            "training_data": numeric_payload(),
            "numeric_tolerance": 0.05,
        },
    )
    assert train.status_code == 200, train.text
    body = train.json()
    assert set(body["artifacts"]) == {"temperature.json", "numeric.json"}
    assert body["metrics_after"]["mean_absolute_error"] is not None
    assert 0.0 <= body["metrics_after"]["coverage"] <= 1.0

    response = client.post(
        "/decision",
        json={
            "decision_type": "numeric",
            "scenario": "price-estimate",
            "data": {"text": "quote number 4"},
        },
    )
    assert response.status_code == 200, response.text
    decision = response.json()
    assert isinstance(decision["value"], float)
    assert 80.0 <= decision["value"] <= 170.0, "value must land in the trained label range"
    assert 0.0 <= decision["probability"] <= 1.0
    assert decision["calibrated"] is True


def test_numeric_calibration_beats_the_uncalibrated_baseline(client: TestClient, model):
    body = client.post(
        "/posthoc_train",
        json={
            "decision_type": "numeric",
            "scenario": "price-mae",
            "training_data": numeric_payload(seed=5),
        },
    ).json()
    assert body["metrics_after"]["mean_absolute_error"] <= body["metrics_before"]["mean_absolute_error"]


def test_numeric_decision_requires_calibration(client: TestClient, model):
    response = client.post(
        "/decision",
        json={"decision_type": "numeric", "scenario": "untrained", "data": {"text": "x"}},
    )
    assert response.status_code == 424
    assert response.json()["error"] == "calibration_not_found"


# ----------------------------------------------------------------------------
# Scenario switching
# ----------------------------------------------------------------------------
def threshold_payload(positive_quantile: float, *, prefix: str = "case") -> list[dict[str, Any]]:
    """Boolean labels that call the top ``1 - q`` of the model's scores True.

    Two scenarios built this way share a base model and differ only in how
    demanding they are — which is what scenario-specific calibration is for.
    """
    payloads = _inputs(prefix=prefix)
    logits = model_module.get_model().logits(payloads)[:, :2]
    scores = logits[:, 1] - logits[:, 0]
    cutoff = float(np.quantile(scores, positive_quantile))
    return [
        {"input": payload, "label": bool(score >= cutoff)}
        for payload, score in zip(payloads, scores, strict=True)
    ]


def _median_score_text() -> str:
    """The input sitting in the middle of the model's score distribution."""
    payloads = _inputs()
    logits = model_module.get_model().logits(payloads)[:, :2]
    scores = logits[:, 1] - logits[:, 0]
    return payloads[int(np.argsort(scores)[len(scores) // 2])]["text"]


def test_scenarios_calibrate_independently(client: TestClient, model):
    """The same input, the same model, two scenarios, two different answers."""
    versions = {}
    for scenario, quantile in (("lenient", 0.1), ("strict", 0.9)):
        response = client.post(
            "/posthoc_train",
            json={
                "decision_type": "boolean",
                "scenario": scenario,
                "training_data": threshold_payload(quantile),
            },
        )
        assert response.status_code == 200, response.text
        versions[scenario] = response.json()["calibration_version"]

    probe = _median_score_text()

    def ask(scenario: str) -> dict[str, Any]:
        return client.post(
            "/decision",
            json={"decision_type": "boolean", "scenario": scenario, "data": {"text": probe}},
        ).json()

    lenient, strict = ask("lenient"), ask("strict")
    assert lenient["calibration_version"] != strict["calibration_version"]
    assert lenient["value"] is True
    assert strict["value"] is False


def test_calibration_cannot_reverse_the_base_model(client: TestClient, model):
    """Anti-correlated labels collapse to the base rate rather than flipping.

    Isotonic regression is fitted as a monotone *increasing* map, so post-hoc
    calibration can move probabilities but never re-rank them. A scenario whose
    labels contradict the model is a modelling problem, and the calibrated
    probabilities say so by sitting near the base rate instead of pretending.
    """
    rows = threshold_payload(0.5)
    for row in rows:
        row["label"] = not row["label"]
    assert (
        client.post(
            "/posthoc_train",
            json={"decision_type": "boolean", "scenario": "contradictory", "training_data": rows},
        ).status_code
        == 200
    )
    body = client.post(
        "/decision",
        json={
            "decision_type": "boolean",
            "scenario": "contradictory",
            "data": {"text": _median_score_text()},
        },
    ).json()
    assert 0.4 <= body["probability"] <= 0.75, "an unlearnable scenario must not look confident"


def test_the_same_scenario_name_can_hold_two_decision_types(client: TestClient, model):
    assert (
        client.post(
            "/posthoc_train",
            json={
                "decision_type": "boolean",
                "scenario": "shared",
                "training_data": classification_payload("boolean", 2, seed=6),
            },
        ).status_code
        == 200
    )
    assert (
        client.post(
            "/posthoc_train",
            json={
                "decision_type": "enum",
                "scenario": "shared",
                "training_data": classification_payload("enum", 3, seed=7),
            },
        ).status_code
        == 200
    )
    rows = client.get("/scenarios").json()["scenarios"]
    assert {row["decision_type"] for row in rows if row["scenario"] == "shared"} == {
        "boolean",
        "enum",
    }


def test_retraining_replaces_the_served_version(client: TestClient, trained_boolean):
    """The cache must not keep serving the previous fit after a retrain."""
    second = client.post(
        "/posthoc_train",
        json={
            "decision_type": "boolean",
            "scenario": "loan-approval",
            "training_data": classification_payload("boolean", 2, invert=True, noise=0.05),
        },
    ).json()
    assert second["calibration_version"] != trained_boolean["calibration_version"]

    decision = client.post(
        "/decision",
        json={"decision_type": "boolean", "scenario": "loan-approval", "data": {"text": "case number 1"}},
    ).json()
    assert decision["calibration_version"] == second["calibration_version"]


def test_calibration_survives_a_restart(client: TestClient, trained_boolean, settings):
    """A fresh process reads the artifacts back off the store, not out of memory."""
    from app.server import app

    model_module.set_model(None)
    with TestClient(app) as restarted:
        body = restarted.post(
            "/decision",
            json={
                "decision_type": "boolean",
                "scenario": "loan-approval",
                "data": {"text": "case number 7"},
            },
        ).json()
    assert body["calibration_version"] == trained_boolean["calibration_version"]
    assert body["calibrated"] is True


# ----------------------------------------------------------------------------
# Errors and guards
# ----------------------------------------------------------------------------
def test_uncalibrated_boolean_falls_back_to_the_raw_model(client: TestClient, model):
    response = client.post(
        "/decision",
        json={"decision_type": "boolean", "scenario": "fresh", "data": {"text": "hello"}},
    )
    assert response.status_code == 200
    body = response.json()
    assert body["calibrated"] is False
    assert body["calibration_version"] is None


def test_require_calibration_turns_the_fallback_into_an_error(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("JEV_REQUIRE_CALIBRATION", "true")
    reset_settings_cache()
    from app.server import app

    with TestClient(app) as strict:
        response = strict.post(
            "/decision",
            json={"decision_type": "boolean", "scenario": "fresh", "data": {"text": "hello"}},
        )
    assert response.status_code == 424
    assert response.json()["error"] == "calibration_not_found"


@pytest.mark.parametrize(
    "body,expected_fragment",
    [
        ({"decision_type": "sideways", "scenario": "s", "data": {"text": "x"}}, "decision_type"),
        ({"decision_type": "boolean", "scenario": "", "data": {"text": "x"}}, "scenario"),
        ({"decision_type": "boolean", "scenario": "s", "data": {}}, "data"),
        ({"decision_type": "boolean", "scenario": "../etc", "data": {"text": "x"}}, "scenario"),
    ],
)
def test_invalid_decision_requests_are_rejected(
    client: TestClient, body: dict[str, Any], expected_fragment: str
):
    response = client.post("/decision", json=body)
    assert response.status_code == 422
    assert response.json()["error"] == "validation_error"
    assert expected_fragment in response.json()["detail"]


def test_training_below_the_minimum_is_rejected(client: TestClient, model):
    response = client.post(
        "/posthoc_train",
        json={
            "decision_type": "boolean",
            "scenario": "tiny",
            "training_data": [{"input": {"text": f"r{i}"}, "label": i % 2} for i in range(4)],
        },
    )
    assert response.status_code == 422
    assert response.json()["error"] == "insufficient_data"


def test_training_on_one_class_is_rejected(client: TestClient, model):
    response = client.post(
        "/posthoc_train",
        json={
            "decision_type": "boolean",
            "scenario": "one-sided",
            "training_data": [{"input": {"text": f"r{i}"}, "label": True} for i in range(20)],
        },
    )
    assert response.status_code == 422
    assert "single class" in response.json()["detail"]


def test_unscoreable_input_is_rejected(client: TestClient, trained_boolean):
    response = client.post(
        "/decision",
        json={"decision_type": "boolean", "scenario": "loan-approval", "data": {"nothing": None}},
    )
    assert response.status_code == 422
    assert response.json()["error"] == "invalid_input"


# ----------------------------------------------------------------------------
# Auth, health, listing
# ----------------------------------------------------------------------------
@pytest.fixture
def secured_client(monkeypatch: pytest.MonkeyPatch) -> Iterator[TestClient]:
    monkeypatch.setenv("JEV_API_KEY", "s3cret")
    reset_settings_cache()
    from app.server import app

    with TestClient(app) as secured:
        yield secured


def test_api_key_is_required_when_configured(secured_client: TestClient):
    body = {"decision_type": "boolean", "scenario": "any", "data": {"text": "x"}}
    assert secured_client.post("/decision", json=body).status_code == 401
    assert (
        secured_client.post("/decision", json=body, headers={"x-api-key": "wrong"}).status_code == 401
    )
    assert (
        secured_client.post("/decision", json=body, headers={"x-api-key": "s3cret"}).status_code == 200
    )
    assert (
        secured_client.post(
            "/decision", json=body, headers={"authorization": "Bearer s3cret"}
        ).status_code
        == 200
    )


def test_health_reports_model_and_registry(client: TestClient):
    body = client.get("/health").json()
    assert body["status"] == "ok"
    assert body["model_loaded"] is True
    assert body["model_backend"] == "hash"
    assert body["registry"].startswith("local:")


def test_scenarios_lists_what_has_been_trained(client: TestClient, trained_boolean):
    rows = client.get("/scenarios").json()["scenarios"]
    assert len(rows) == 1
    assert rows[0]["scenario"] == "loan-approval"
    assert rows[0]["decision_type"] == "boolean"
    assert rows[0]["calibration_version"] == trained_boolean["calibration_version"]
    assert rows[0]["num_samples"] == SAMPLE_COUNT
    assert rows[0]["trained_at"]


def test_openapi_document_is_served(client: TestClient):
    schema = client.get("/openapi.json").json()
    assert "/decision" in schema["paths"]
    assert "/posthoc_train" in schema["paths"]
