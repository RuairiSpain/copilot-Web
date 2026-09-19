"""Unit tests for the blob-backed registry, against the filesystem store.

``LocalObjectStore`` and ``BlobObjectStore`` implement the same three
operations, so everything here — versioning, the latest pointer, the cache,
the mismatch guard — is the behaviour that runs in Azure too. The blob client
itself is not re-tested; that is Azure's code.
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import numpy as np
import pytest

from app.calibration import (
    IsotonicCalibrator,
    TemperatureScaler,
    fit_numeric_bins,
)
from app.calibration_registry import (
    CalibrationMismatchError,
    CalibrationRegistry,
    LocalObjectStore,
    RegistryError,
    new_version,
)
from config.settings import Settings, get_settings


class CountingStore(LocalObjectStore):
    """A local store that counts reads, so cache behaviour is observable."""

    def __init__(self, root: Path) -> None:
        super().__init__(root)
        self.reads = 0

    def read_json(self, key: str) -> dict[str, Any] | None:
        self.reads += 1
        return super().read_json(key)


@pytest.fixture
def store(tmp_path: Path) -> CountingStore:
    return CountingStore(tmp_path / "store")


@pytest.fixture
def counting_registry(store: CountingStore, settings: Settings) -> CalibrationRegistry:
    return CalibrationRegistry(store, settings)


def _save_classification(
    registry: CalibrationRegistry,
    scenario: str = "loan",
    decision_type: str = "boolean",
    *,
    temperature: float = 1.5,
    num_classes: int = 2,
    class_names: list[str] | None = None,
) -> str:
    version = new_version()
    calibrators = [IsotonicCalibrator(x=(0.0, 1.0), y=(0.0, 1.0))] * num_classes
    artifacts = [
        registry.save_temperature(scenario, decision_type, version, TemperatureScaler(temperature)),
        registry.save_isotonic(scenario, decision_type, version, list(calibrators), class_names),
    ]
    registry.commit_version(
        scenario,
        decision_type,
        version,
        artifacts=artifacts,
        num_classes=num_classes,
        class_names=class_names,
        metadata={"num_samples": 42},
    )
    return version


# ----------------------------------------------------------------------------
# Round-trips
# ----------------------------------------------------------------------------
def test_save_and_load_a_classification_bundle(registry: CalibrationRegistry):
    version = _save_classification(registry, class_names=None)
    bundle = registry.load_calibration("loan", "boolean")

    assert bundle is not None
    assert bundle.version == version
    assert bundle.temperature.temperature == pytest.approx(1.5)
    assert bundle.num_classes == 2
    assert bundle.isotonic is not None and len(bundle.isotonic) == 2
    assert bundle.metadata["num_samples"] == 42


def test_save_and_load_a_numeric_bundle(registry: CalibrationRegistry):
    scores = np.linspace(0.0, 1.0, 50)
    calibrator = fit_numeric_bins(scores, 3.0 + 2.0 * scores, num_bins=5, tolerance=0.1)
    version = new_version()
    artifacts = [
        registry.save_temperature("price", "numeric", version, 2.0),
        registry.save_numeric("price", "numeric", version, calibrator),
    ]
    registry.commit_version("price", "numeric", version, artifacts=artifacts, num_classes=3)

    bundle = registry.load_calibration("price", "numeric")
    assert bundle is not None and bundle.numeric is not None
    assert bundle.numeric.predict(0.5) == pytest.approx(calibrator.predict(0.5))


def test_load_returns_none_for_an_unknown_scenario(registry: CalibrationRegistry):
    assert registry.load_calibration("never-trained", "boolean") is None


def test_class_names_survive_the_round_trip(registry: CalibrationRegistry):
    _save_classification(
        registry, "triage", "enum", num_classes=3, class_names=["low", "medium", "high"]
    )
    bundle = registry.load_calibration("triage", "enum")
    assert bundle is not None
    assert bundle.class_names == ("low", "medium", "high")
    assert bundle.label_for(2) == "high"
    assert bundle.label_for(99) == "99"


def test_artifacts_are_plain_json_not_pickles(registry: CalibrationRegistry, tmp_path: Path):
    version = _save_classification(registry)
    written = list((tmp_path / "registry").rglob("*.json"))
    assert written, "expected artifacts on disk"
    for path in written:
        json.loads(path.read_text(encoding="utf-8"))  # raises if it is not JSON
    assert any(version in str(path) for path in written)


# ----------------------------------------------------------------------------
# Versioning
# ----------------------------------------------------------------------------
def test_latest_pointer_moves_to_the_newest_version(registry: CalibrationRegistry):
    first = _save_classification(registry, temperature=1.0)
    registry.clear_cache()
    second = _save_classification(registry, temperature=3.0)
    registry.clear_cache()

    bundle = registry.load_calibration("loan", "boolean")
    assert bundle is not None
    assert bundle.version == second != first
    assert bundle.temperature.temperature == pytest.approx(3.0)


def test_older_versions_stay_readable_for_rollback(registry: CalibrationRegistry, tmp_path: Path):
    first = _save_classification(registry, temperature=1.0)
    registry.clear_cache()
    _save_classification(registry, temperature=3.0)

    root = tmp_path / "registry" / "calibration" / "loan" / "boolean"
    assert (root / "versions" / first / "temperature.json").is_file()

    # Rolling back is a pointer write, nothing else.
    (root / "latest.json").write_text(json.dumps({"version": first}), encoding="utf-8")
    registry.clear_cache()
    bundle = registry.load_calibration("loan", "boolean")
    assert bundle is not None and bundle.temperature.temperature == pytest.approx(1.0)


def test_new_version_ids_are_unique_and_sortable():
    versions = sorted(new_version() for _ in range(5))
    assert len(set(versions)) == 5
    assert all(version[:4].isdigit() for version in versions)


def test_a_dangling_pointer_is_reported_not_ignored(registry: CalibrationRegistry, tmp_path: Path):
    _save_classification(registry)
    pointer = tmp_path / "registry" / "calibration" / "loan" / "boolean" / "latest.json"
    pointer.write_text(json.dumps({"version": "does-not-exist"}), encoding="utf-8")
    registry.clear_cache()
    with pytest.raises(RegistryError):
        registry.load_calibration("loan", "boolean")


# ----------------------------------------------------------------------------
# Cache
# ----------------------------------------------------------------------------
def test_second_load_is_served_from_cache(counting_registry: CalibrationRegistry, store: CountingStore):
    _save_classification(counting_registry)
    counting_registry.clear_cache()

    counting_registry.load_calibration("loan", "boolean")
    after_first = store.reads
    counting_registry.load_calibration("loan", "boolean")
    assert store.reads == after_first, "cached load must not touch the store"


def test_clear_cache_forces_a_reread(counting_registry: CalibrationRegistry, store: CountingStore):
    _save_classification(counting_registry)
    counting_registry.load_calibration("loan", "boolean")
    before = store.reads
    assert counting_registry.clear_cache("loan", "boolean") == 1
    counting_registry.load_calibration("loan", "boolean")
    assert store.reads > before


def test_clear_cache_is_scoped_by_scenario(counting_registry: CalibrationRegistry):
    _save_classification(counting_registry, "loan", "boolean")
    _save_classification(counting_registry, "triage", "enum", num_classes=3)
    counting_registry.clear_cache()
    counting_registry.load_calibration("loan", "boolean")
    counting_registry.load_calibration("triage", "enum")

    assert counting_registry.cached_entries == 2
    assert counting_registry.clear_cache("loan") == 1
    assert counting_registry.cached_entries == 1


def test_missing_scenarios_are_cached_too(counting_registry: CalibrationRegistry, store: CountingStore):
    assert counting_registry.load_calibration("absent", "boolean") is None
    before = store.reads
    assert counting_registry.load_calibration("absent", "boolean") is None
    assert store.reads == before


def test_ttl_zero_disables_the_cache(store: CountingStore, monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("JEV_REGISTRY_CACHE_TTL_SECONDS", "0")
    from config.settings import reset_settings_cache

    reset_settings_cache()
    registry = CalibrationRegistry(store, get_settings())
    _save_classification(registry)

    registry.load_calibration("loan", "boolean")
    before = store.reads
    registry.load_calibration("loan", "boolean")
    assert store.reads > before
    assert registry.cached_entries == 0


def test_cache_is_bounded(store: CountingStore, monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("JEV_REGISTRY_CACHE_MAX_ENTRIES", "2")
    from config.settings import reset_settings_cache

    reset_settings_cache()
    registry = CalibrationRegistry(store, get_settings())
    for index in range(4):
        _save_classification(registry, f"scenario{index}")
    registry.clear_cache()
    for index in range(4):
        registry.load_calibration(f"scenario{index}", "boolean")
    assert registry.cached_entries <= 2


# ----------------------------------------------------------------------------
# Guards and listing
# ----------------------------------------------------------------------------
def test_class_count_mismatch_raises(registry: CalibrationRegistry):
    _save_classification(registry, "triage", "enum", num_classes=3)
    with pytest.raises(CalibrationMismatchError):
        registry.load_calibration("triage", "enum", num_classes=5)
    # The matching count is fine.
    assert registry.load_calibration("triage", "enum", num_classes=3) is not None


def test_list_scenarios_reports_every_trained_pair(registry: CalibrationRegistry):
    _save_classification(registry, "loan", "boolean")
    _save_classification(registry, "triage", "enum", num_classes=3, class_names=["a", "b", "c"])

    rows = registry.list_scenarios()
    assert [(row["scenario"], row["decision_type"]) for row in rows] == [
        ("loan", "boolean"),
        ("triage", "enum"),
    ]
    assert rows[1]["class_names"] == ["a", "b", "c"]
    assert rows[0]["num_samples"] == 42


def test_list_scenarios_is_empty_before_any_training(registry: CalibrationRegistry):
    assert registry.list_scenarios() == []


def test_local_store_rejects_keys_escaping_the_root(tmp_path: Path):
    local = LocalObjectStore(tmp_path / "root")
    with pytest.raises(RegistryError):
        local.write_json("../escape.json", {"a": 1})
    with pytest.raises(RegistryError):
        local.read_json("../../etc/passwd")


def test_local_store_writes_are_atomic(tmp_path: Path):
    local = LocalObjectStore(tmp_path / "root")
    local.write_json("a/b.json", {"value": 1})
    local.write_json("a/b.json", {"value": 2})
    assert local.read_json("a/b.json") == {"value": 2}
    assert not list((tmp_path / "root" / "a").glob("*.tmp"))


def test_local_store_lists_only_matching_keys(tmp_path: Path):
    local = LocalObjectStore(tmp_path / "root")
    local.write_json("calibration/a/boolean/latest.json", {"version": "v"})
    local.write_json("other/thing.json", {})
    assert list(local.list_keys("calibration/")) == ["calibration/a/boolean/latest.json"]
