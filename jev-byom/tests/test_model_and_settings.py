"""Unit tests for input handling, the model backends and configuration.

The Hugging Face backend is not loaded here — downloading weights would make
the suite slow and network-dependent — but everything around it is: text
extraction, batching contracts, the singleton, and the settings validation that
decides whether a container is allowed to start at all.
"""

from __future__ import annotations

import os

import numpy as np
import pytest

from app import model as model_module
from app.model import HashBackend, ModelInputError, extract_text
from config.settings import Settings, get_settings, reset_settings_cache


# ----------------------------------------------------------------------------
# Text extraction
# ----------------------------------------------------------------------------
def test_preferred_fields_win_in_order():
    fields = ("text", "prompt")
    assert extract_text({"text": "a", "prompt": "b"}, fields) == "a"
    assert extract_text({"prompt": "b", "other": "c"}, fields) == "b"


def test_blank_preferred_field_falls_through():
    assert extract_text({"text": "   ", "prompt": "b"}, ("text", "prompt")) == "b"


def test_structured_payloads_render_deterministically():
    payload = {"amount": 1200, "applicant": "Ada", "flagged": True}
    rendered = extract_text(payload, ("text",))
    assert rendered == "amount: 1200\napplicant: Ada\nflagged: true"
    # Key order in the request must not change the text the model sees.
    assert extract_text({"flagged": True, "applicant": "Ada", "amount": 1200}, ("text",)) == rendered


def test_empty_or_unscoreable_input_raises():
    with pytest.raises(ModelInputError):
        extract_text({}, ("text",))
    with pytest.raises(ModelInputError):
        extract_text({"nothing": None}, ("text",))
    with pytest.raises(ModelInputError):
        extract_text("not a dict", ("text",))  # type: ignore[arg-type]


# ----------------------------------------------------------------------------
# Hash backend
# ----------------------------------------------------------------------------
def test_hash_backend_is_deterministic_and_shaped():
    backend = HashBackend(num_labels=4)
    first = backend.logits([{"text": "hello"}, {"text": "world"}])
    second = backend.logits([{"text": "hello"}, {"text": "world"}])

    assert first.shape == (2, 4)
    assert np.array_equal(first, second)
    assert not np.array_equal(first[0], first[1])
    assert backend.num_labels == 4


def test_hash_backend_logits_stay_inside_the_scale():
    backend = HashBackend(num_labels=3, scale=2.0)
    logits = backend.logits([{"text": f"row {index}"} for index in range(200)])
    assert np.all(np.abs(logits) <= 2.0)
    assert logits.std() > 0.1, "a constant score would make calibration meaningless"


def test_logits_one_returns_a_single_row():
    assert HashBackend(num_labels=2).logits_one({"text": "x"}).shape == (1, 2)


# ----------------------------------------------------------------------------
# Singleton
# ----------------------------------------------------------------------------
def test_get_model_caches_and_set_model_replaces(settings: Settings):
    model_module.set_model(None)
    assert model_module.is_loaded() is False

    first = model_module.get_model(settings)
    assert model_module.is_loaded() is True
    assert model_module.get_model(settings) is first

    replacement = HashBackend(num_labels=2)
    model_module.set_model(replacement)
    assert model_module.get_model(settings) is replacement


def test_build_model_honours_the_backend_setting(settings: Settings):
    assert isinstance(model_module.build_model(settings), HashBackend)


# ----------------------------------------------------------------------------
# Settings
# ----------------------------------------------------------------------------
def test_settings_read_the_jev_prefix(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("JEV_NUMERIC_BINS", "17")
    monkeypatch.setenv("JEV_BOOLEAN_THRESHOLD", "0.75")
    reset_settings_cache()
    loaded = get_settings()
    assert loaded.numeric_bins == 17
    assert loaded.boolean_threshold == 0.75


def test_blob_settings_accept_the_deployment_variable_names(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.delenv("JEV_LOCAL_REGISTRY_DIR", raising=False)
    monkeypatch.setenv("BLOB_CONN_STR", "UseDevelopmentStorage=true")
    monkeypatch.setenv("BLOB_CONTAINER_NAME", "jev-calibration")
    reset_settings_cache()
    loaded = get_settings()
    assert loaded.blob_conn_str == "UseDevelopmentStorage=true"
    assert loaded.blob_container_name == "jev-calibration"


def test_a_container_with_no_store_configured_refuses_to_start(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.delenv("JEV_LOCAL_REGISTRY_DIR", raising=False)
    reset_settings_cache()
    with pytest.raises(ValueError, match="No calibration store configured"):
        get_settings()


def test_temperature_bounds_are_validated(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("JEV_TEMPERATURE_MIN", "5.0")
    monkeypatch.setenv("JEV_TEMPERATURE_MAX", "1.0")
    reset_settings_cache()
    with pytest.raises(ValueError, match="TEMPERATURE_MIN"):
        get_settings()


def test_temperature_init_must_sit_inside_the_bounds(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("JEV_TEMPERATURE_INIT", "50.0")
    reset_settings_cache()
    with pytest.raises(ValueError, match="TEMPERATURE_INIT"):
        get_settings()


def test_out_of_range_values_are_rejected(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("JEV_VALIDATION_SPLIT", "1.5")
    reset_settings_cache()
    with pytest.raises(ValueError):
        get_settings()


def test_settings_are_cached_until_reset(monkeypatch: pytest.MonkeyPatch):
    first = get_settings()
    monkeypatch.setenv("JEV_NUMERIC_BINS", "23")
    assert get_settings() is first
    reset_settings_cache()
    assert get_settings().numeric_bins == 23


# ----------------------------------------------------------------------------
# Hugging Face backend (opt-in: needs weights, so it is off by default)
# ----------------------------------------------------------------------------
#: Captured at import time: the autouse fixture clears every JEV_* variable
#: before a test body runs, which would otherwise hide this one.
HF_TEST_MODEL = os.environ.get("JEV_TEST_HF_MODEL")


@pytest.mark.skipif(
    not HF_TEST_MODEL,
    reason="set JEV_TEST_HF_MODEL to a checkpoint id to exercise the real backend",
)
def test_huggingface_backend_scores_a_batch(monkeypatch: pytest.MonkeyPatch):
    """Load a real checkpoint and check the contract the engine relies on.

    Run it against a tiny model, e.g.::

        JEV_TEST_HF_MODEL=hf-internal-testing/tiny-random-DistilBertForSequenceClassification \
            pytest tests/test_model_and_settings.py -k huggingface
    """
    monkeypatch.setenv("JEV_MODEL_BACKEND", "huggingface")
    monkeypatch.setenv("JEV_MODEL_NAME", HF_TEST_MODEL)
    monkeypatch.setenv("JEV_MODEL_NUM_LABELS", "3")
    monkeypatch.setenv("JEV_MODEL_BATCH_SIZE", "2")
    reset_settings_cache()

    backend = model_module.build_model(get_settings())
    logits = backend.logits([{"text": f"row {index}"} for index in range(5)])

    assert logits.shape == (5, backend.num_labels)
    assert logits.dtype == np.float64
    assert np.all(np.isfinite(logits))
    # Batching must not change the answer.
    assert np.allclose(logits[:2], backend.logits([{"text": "row 0"}, {"text": "row 1"}]))
