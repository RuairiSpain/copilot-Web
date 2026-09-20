"""Shared fixtures.

Every test runs against the ``hash`` backend and a filesystem registry, so the
suite needs no weights, no network and no Azure account — and still exercises
the real calibration math, the real registry serialisation and the real
FastAPI wiring.
"""

from __future__ import annotations

import os
from collections.abc import Iterator
from pathlib import Path
from typing import Any

import numpy as np
import pytest

from app import model as model_module
from app.calibration_registry import CalibrationRegistry, LocalObjectStore
from config.settings import Settings, get_settings, reset_settings_cache

NUM_CLASSES = 3


@pytest.fixture(autouse=True)
def _clean_environment(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> Iterator[None]:
    """Point every setting at hermetic defaults and reset the caches."""
    for key in list(os.environ):
        if key.startswith("JEV_") or key.startswith("BLOB"):
            monkeypatch.delenv(key, raising=False)
    monkeypatch.setenv("JEV_MODEL_BACKEND", "hash")
    monkeypatch.setenv("JEV_MODEL_NUM_LABELS", str(NUM_CLASSES))
    monkeypatch.setenv("JEV_LOCAL_REGISTRY_DIR", str(tmp_path / "registry"))
    monkeypatch.setenv("JEV_TRAIN_MIN_SAMPLES", "8")
    monkeypatch.setenv("JEV_LOG_JSON", "false")
    reset_settings_cache()
    model_module.set_model(None)
    yield
    model_module.set_model(None)
    reset_settings_cache()


@pytest.fixture
def settings() -> Settings:
    return get_settings()


@pytest.fixture
def registry(settings: Settings, tmp_path: Path) -> CalibrationRegistry:
    return CalibrationRegistry(LocalObjectStore(tmp_path / "registry"), settings)


@pytest.fixture
def model(settings: Settings) -> model_module.BaseDecisionModel:
    built = model_module.build_model(settings)
    model_module.set_model(built)
    return built


@pytest.fixture
def client(settings: Settings) -> Iterator[Any]:
    """A TestClient with the app's lifespan run, so state matches production."""
    from fastapi.testclient import TestClient

    from app.server import app

    with TestClient(app) as test_client:
        yield test_client


# ----------------------------------------------------------------------------
# Synthetic data
# ----------------------------------------------------------------------------
def make_overconfident_logits(
    num_samples: int, num_classes: int, *, seed: int = 0, scale: float = 6.0, noise: float = 2.5
) -> tuple[np.ndarray, np.ndarray]:
    """Logits that are right most of the time but far too sure of themselves.

    This is the situation temperature scaling exists for: the argmax is usually
    correct, so accuracy is high, but the softmax probabilities sit near 1.0
    regardless, so NLL and ECE are poor until T > 1 spreads them out.
    """
    rng = np.random.default_rng(seed)
    labels = rng.integers(0, num_classes, size=num_samples)
    logits = rng.normal(0.0, 0.5, size=(num_samples, num_classes))
    flip = rng.random(num_samples) < 0.25
    for index, label in enumerate(labels):
        target = int(rng.integers(0, num_classes)) if flip[index] else int(label)
        logits[index, target] += scale + rng.normal(0.0, noise)
    return logits, labels


def make_training_payload(
    num_samples: int,
    label_fn,
    *,
    prefix: str = "sample",
) -> list[dict[str, Any]]:
    """Training rows for the hash backend: distinct text, caller-chosen labels."""
    return [
        {"input": {"text": f"{prefix} {index}"}, "label": label_fn(index)}
        for index in range(num_samples)
    ]
