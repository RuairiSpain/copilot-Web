"""Blob-backed calibration registry with an in-memory read cache.

Layout inside the container (``JEV_BLOB_PREFIX`` defaults to ``calibration``)::

    calibration/<scenario>/<decision_type>/latest.json
    calibration/<scenario>/<decision_type>/versions/<version>/manifest.json
    calibration/<scenario>/<decision_type>/versions/<version>/temperature.json
    calibration/<scenario>/<decision_type>/versions/<version>/isotonic.json
    calibration/<scenario>/<decision_type>/versions/<version>/numeric.json

Every write goes to a fresh, immutable version directory; ``latest.json`` is a
one-field pointer flipped last, so a reader either sees the whole previous
version or the whole new one, and rolling back is a pointer write. Nothing is
pickled — artifacts are JSON, so a blob can never execute code on load.

Two stores implement the same interface: ``BlobObjectStore`` for Azure and
``LocalObjectStore`` (``JEV_LOCAL_REGISTRY_DIR``) for tests and laptops.
"""

from __future__ import annotations

import json
import logging
import os
import threading
import time
import uuid
from abc import ABC, abstractmethod
from collections.abc import Iterable
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from app.calibration import IsotonicCalibrator, NumericCalibrator, TemperatureScaler
from config.settings import Settings, get_settings

logger = logging.getLogger(__name__)

LATEST_BLOB = "latest.json"
MANIFEST_BLOB = "manifest.json"
TEMPERATURE_BLOB = "temperature.json"
ISOTONIC_BLOB = "isotonic.json"
NUMERIC_BLOB = "numeric.json"


class RegistryError(RuntimeError):
    """Base class for registry failures."""


class CalibrationMismatchError(RegistryError):
    """A stored artifact does not fit the running model (e.g. class count changed)."""


# ----------------------------------------------------------------------------
# Object stores
# ----------------------------------------------------------------------------
class ObjectStore(ABC):
    """The three operations the registry needs from a key/value blob store."""

    @abstractmethod
    def read_json(self, key: str) -> dict[str, Any] | None:
        """Return the parsed object at ``key``, or None when it does not exist."""

    @abstractmethod
    def write_json(self, key: str, payload: dict[str, Any]) -> None:
        """Write ``payload`` at ``key``, overwriting any existing object."""

    @abstractmethod
    def list_keys(self, prefix: str) -> Iterable[str]:
        """Yield every key beginning with ``prefix``."""

    @property
    @abstractmethod
    def description(self) -> str:
        """Human-readable location, used in logs and /health."""


class LocalObjectStore(ObjectStore):
    """Filesystem-backed store. Writes are atomic within a directory."""

    def __init__(self, root: str | os.PathLike[str]) -> None:
        self._root = Path(root)
        self._root.mkdir(parents=True, exist_ok=True)

    def _path(self, key: str) -> Path:
        path = (self._root / key).resolve()
        root = self._root.resolve()
        if not path.is_relative_to(root):
            raise RegistryError(f"key escapes the registry root: {key!r}")
        return path

    def read_json(self, key: str) -> dict[str, Any] | None:
        path = self._path(key)
        if not path.is_file():
            return None
        return json.loads(path.read_text(encoding="utf-8"))

    def write_json(self, key: str, payload: dict[str, Any]) -> None:
        path = self._path(key)
        path.parent.mkdir(parents=True, exist_ok=True)
        temporary = path.with_name(f"{path.name}.{uuid.uuid4().hex}.tmp")
        temporary.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")
        os.replace(temporary, path)

    def list_keys(self, prefix: str) -> Iterable[str]:
        root = self._root.resolve()
        for path in sorted(self._root.rglob("*.json")):
            key = str(path.resolve().relative_to(root)).replace(os.sep, "/")
            if key.startswith(prefix):
                yield key

    @property
    def description(self) -> str:
        return f"local:{self._root}"


class BlobObjectStore(ObjectStore):
    """Azure Blob Storage store, by connection string or managed identity."""

    def __init__(
        self,
        *,
        container_name: str,
        connection_string: str | None = None,
        account_url: str | None = None,
        create_container: bool = True,
    ) -> None:
        from azure.core.exceptions import ResourceExistsError
        from azure.storage.blob import BlobServiceClient

        if connection_string:
            service = BlobServiceClient.from_connection_string(connection_string)
        elif account_url:
            from azure.identity import DefaultAzureCredential

            service = BlobServiceClient(
                account_url=account_url, credential=DefaultAzureCredential()
            )
        else:
            raise RegistryError("BlobObjectStore needs a connection string or an account URL")

        self._container = service.get_container_client(container_name)
        self._container_name = container_name
        self._account_url = account_url or service.url
        if create_container:
            try:
                self._container.create_container()
                logger.info("created blob container %s", container_name)
            except ResourceExistsError:
                pass

    def read_json(self, key: str) -> dict[str, Any] | None:
        from azure.core.exceptions import ResourceNotFoundError

        try:
            downloader = self._container.download_blob(key, encoding="utf-8")
            return json.loads(downloader.readall())
        except ResourceNotFoundError:
            return None

    def write_json(self, key: str, payload: dict[str, Any]) -> None:
        body = json.dumps(payload, indent=2, sort_keys=True).encode("utf-8")
        self._container.upload_blob(
            name=key,
            data=body,
            overwrite=True,
            content_type="application/json",
        )

    def list_keys(self, prefix: str) -> Iterable[str]:
        for blob in self._container.list_blobs(name_starts_with=prefix):
            yield blob.name

    @property
    def description(self) -> str:
        return f"blob:{self._account_url}/{self._container_name}"


def build_object_store(settings: Settings) -> ObjectStore:
    """Pick the store the settings describe."""
    if settings.local_registry_dir:
        return LocalObjectStore(settings.local_registry_dir)
    return BlobObjectStore(
        container_name=settings.blob_container_name,
        connection_string=settings.blob_conn_str,
        account_url=settings.blob_account_url,
        create_container=settings.registry_create_container,
    )


# ----------------------------------------------------------------------------
# Bundle
# ----------------------------------------------------------------------------
@dataclass(frozen=True)
class CalibrationBundle:
    """Everything needed to calibrate one (scenario, decision_type) pair."""

    scenario: str
    decision_type: str
    version: str
    temperature: TemperatureScaler
    num_classes: int | None = None
    class_names: tuple[str, ...] | None = None
    isotonic: tuple[IsotonicCalibrator | None, ...] | None = None
    numeric: NumericCalibrator | None = None
    created_at: str | None = None
    metadata: dict[str, Any] = field(default_factory=dict)

    def label_for(self, index: int) -> str:
        if self.class_names and 0 <= index < len(self.class_names):
            return self.class_names[index]
        return str(index)


def new_version() -> str:
    """A sortable, unique version id: ``20260919T143012Z-1a2b3c4d``."""
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    return f"{stamp}-{uuid.uuid4().hex[:8]}"


# ----------------------------------------------------------------------------
# Registry
# ----------------------------------------------------------------------------
class CalibrationRegistry:
    """Reads and writes calibration artifacts, with a TTL cache in front."""

    def __init__(self, store: ObjectStore, settings: Settings | None = None) -> None:
        self._store = store
        self._settings = settings or get_settings()
        self._lock = threading.RLock()
        self._cache: dict[tuple[str, str], tuple[float, CalibrationBundle | None]] = {}

    # -- key helpers -----------------------------------------------------
    def _scenario_prefix(self, scenario: str, decision_type: str) -> str:
        return f"{self._settings.blob_prefix}/{scenario}/{decision_type}"

    def _version_prefix(self, scenario: str, decision_type: str, version: str) -> str:
        return f"{self._scenario_prefix(scenario, decision_type)}/versions/{version}"

    # -- writes ----------------------------------------------------------
    def save_temperature(
        self,
        scenario: str,
        decision_type: str,
        version: str,
        temperature: TemperatureScaler | float,
    ) -> str:
        """Write the temperature artifact and return its key."""
        scaler = (
            temperature
            if isinstance(temperature, TemperatureScaler)
            else TemperatureScaler(float(temperature))
        )
        key = f"{self._version_prefix(scenario, decision_type, version)}/{TEMPERATURE_BLOB}"
        self._store.write_json(key, scaler.to_dict())
        return key

    def save_isotonic(
        self,
        scenario: str,
        decision_type: str,
        version: str,
        calibrators: list[IsotonicCalibrator | None],
        class_names: list[str] | None = None,
    ) -> str:
        """Write the per-class isotonic artifact and return its key."""
        key = f"{self._version_prefix(scenario, decision_type, version)}/{ISOTONIC_BLOB}"
        self._store.write_json(
            key,
            {
                "num_classes": len(calibrators),
                "class_names": list(class_names) if class_names else None,
                "calibrators": [c.to_dict() if c is not None else None for c in calibrators],
            },
        )
        return key

    def save_numeric(
        self,
        scenario: str,
        decision_type: str,
        version: str,
        calibrator: NumericCalibrator,
    ) -> str:
        """Write the numeric bin artifact and return its key."""
        key = f"{self._version_prefix(scenario, decision_type, version)}/{NUMERIC_BLOB}"
        self._store.write_json(key, calibrator.to_dict())
        return key

    def commit_version(
        self,
        scenario: str,
        decision_type: str,
        version: str,
        *,
        artifacts: list[str],
        num_classes: int | None = None,
        class_names: list[str] | None = None,
        metadata: dict[str, Any] | None = None,
    ) -> str:
        """Write the manifest, then flip ``latest.json`` to this version.

        The pointer is written last: until it is, readers keep seeing the
        previous version, so a half-written fit is never served.
        """
        manifest = {
            "scenario": scenario,
            "decision_type": decision_type,
            "version": version,
            "created_at": datetime.now(timezone.utc).isoformat(),
            "num_classes": num_classes,
            "class_names": list(class_names) if class_names else None,
            "artifacts": sorted(key.rsplit("/", 1)[-1] for key in artifacts),
            "metadata": metadata or {},
        }
        manifest_key = f"{self._version_prefix(scenario, decision_type, version)}/{MANIFEST_BLOB}"
        self._store.write_json(manifest_key, manifest)
        self._store.write_json(
            f"{self._scenario_prefix(scenario, decision_type)}/{LATEST_BLOB}",
            {"version": version, "updated_at": manifest["created_at"]},
        )
        return manifest_key

    # -- reads -----------------------------------------------------------
    def load_calibration(
        self,
        scenario: str,
        decision_type: str,
        num_classes: int | None = None,
    ) -> CalibrationBundle | None:
        """Return the latest calibration for a scenario, or None if untrained.

        ``num_classes``, when given, is checked against the stored artifact: a
        mismatch means the base model changed since the fit, which would produce
        silently wrong probabilities, so it raises rather than guessing.
        """
        cache_key = (scenario, decision_type)
        ttl = self._settings.registry_cache_ttl_seconds
        now = time.monotonic()

        if ttl > 0:
            with self._lock:
                cached = self._cache.get(cache_key)
                if cached is not None and cached[0] > now:
                    bundle = cached[1]
                    self._check_classes(bundle, num_classes)
                    return bundle

        bundle = self._load_uncached(scenario, decision_type)

        if ttl > 0:
            with self._lock:
                if len(self._cache) >= self._settings.registry_cache_max_entries:
                    # Evict the entry closest to expiry — approximately LRU for
                    # a uniform TTL, and bounded work on a small dict.
                    oldest = min(self._cache, key=lambda k: self._cache[k][0])
                    self._cache.pop(oldest, None)
                self._cache[cache_key] = (now + ttl, bundle)

        self._check_classes(bundle, num_classes)
        return bundle

    @staticmethod
    def _check_classes(bundle: CalibrationBundle | None, num_classes: int | None) -> None:
        if bundle is None or num_classes is None or bundle.num_classes is None:
            return
        if bundle.num_classes != num_classes:
            raise CalibrationMismatchError(
                f"calibration for {bundle.scenario}/{bundle.decision_type} was fitted "
                f"for {bundle.num_classes} classes but the request expects {num_classes}; "
                "retrain the scenario via POST /posthoc_train"
            )

    def _load_uncached(self, scenario: str, decision_type: str) -> CalibrationBundle | None:
        prefix = self._scenario_prefix(scenario, decision_type)
        pointer = self._store.read_json(f"{prefix}/{LATEST_BLOB}")
        if not pointer or not pointer.get("version"):
            return None
        version = str(pointer["version"])
        version_prefix = f"{prefix}/versions/{version}"

        manifest = self._store.read_json(f"{version_prefix}/{MANIFEST_BLOB}") or {}
        temperature_payload = self._store.read_json(f"{version_prefix}/{TEMPERATURE_BLOB}")
        if temperature_payload is None:
            raise RegistryError(
                f"{prefix}/{LATEST_BLOB} points at version {version}, whose temperature "
                "artifact is missing; the store is inconsistent"
            )
        temperature = TemperatureScaler.from_dict(temperature_payload)

        isotonic: tuple[IsotonicCalibrator | None, ...] | None = None
        class_names = manifest.get("class_names")
        num_classes = manifest.get("num_classes")

        isotonic_payload = self._store.read_json(f"{version_prefix}/{ISOTONIC_BLOB}")
        if isotonic_payload is not None:
            isotonic = tuple(
                IsotonicCalibrator.from_dict(item) if item is not None else None
                for item in isotonic_payload.get("calibrators", [])
            )
            num_classes = isotonic_payload.get("num_classes", num_classes) or len(isotonic)
            class_names = isotonic_payload.get("class_names") or class_names

        numeric_payload = self._store.read_json(f"{version_prefix}/{NUMERIC_BLOB}")
        numeric = NumericCalibrator.from_dict(numeric_payload) if numeric_payload else None

        return CalibrationBundle(
            scenario=scenario,
            decision_type=decision_type,
            version=version,
            temperature=temperature,
            num_classes=int(num_classes) if num_classes else None,
            class_names=tuple(class_names) if class_names else None,
            isotonic=isotonic,
            numeric=numeric,
            created_at=manifest.get("created_at"),
            metadata=manifest.get("metadata") or {},
        )

    def list_scenarios(self) -> list[dict[str, Any]]:
        """List every trained (scenario, decision_type) pair with its manifest."""
        prefix = f"{self._settings.blob_prefix}/"
        rows: list[dict[str, Any]] = []
        for key in self._store.list_keys(prefix):
            if not key.endswith(f"/{LATEST_BLOB}"):
                continue
            parts = key[len(prefix) :].split("/")
            if len(parts) != 3:
                continue
            scenario, decision_type, _ = parts
            pointer = self._store.read_json(key) or {}
            version = pointer.get("version")
            if not version:
                continue
            manifest = (
                self._store.read_json(
                    f"{self._version_prefix(scenario, decision_type, version)}/{MANIFEST_BLOB}"
                )
                or {}
            )
            metadata = manifest.get("metadata") or {}
            rows.append(
                {
                    "scenario": scenario,
                    "decision_type": decision_type,
                    "calibration_version": version,
                    "num_classes": manifest.get("num_classes"),
                    "class_names": manifest.get("class_names"),
                    "num_samples": metadata.get("num_samples"),
                    "trained_at": manifest.get("created_at") or pointer.get("updated_at"),
                }
            )
        rows.sort(key=lambda row: (row["scenario"], row["decision_type"]))
        return rows

    # -- cache -----------------------------------------------------------
    def clear_cache(self, scenario: str | None = None, decision_type: str | None = None) -> int:
        """Drop cached bundles. Returns how many entries were removed.

        Called after every fit so the next ``/decision`` reads the new version
        instead of waiting out the TTL.
        """
        with self._lock:
            if scenario is None:
                removed = len(self._cache)
                self._cache.clear()
                return removed
            keys = [
                key
                for key in self._cache
                if key[0] == scenario and (decision_type is None or key[1] == decision_type)
            ]
            for key in keys:
                self._cache.pop(key, None)
            return len(keys)

    @property
    def cached_entries(self) -> int:
        with self._lock:
            return len(self._cache)

    @property
    def description(self) -> str:
        return self._store.description
