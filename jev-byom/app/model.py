"""The frozen base model and its logit extraction.

Two backends sit behind one interface:

* ``huggingface`` — a real sequence-classification checkpoint (point
  ``JEV_MODEL_NAME`` at your mini-jev-DistilBERT or OpenJev-E5 build). Runs on
  CPU by default, in ``eval()`` mode, under ``torch.inference_mode()``.
* ``hash`` — deterministic pseudo-logits derived from a hash of the input text.
  It downloads nothing and has no torch dependency, which is what makes the
  calibration and endpoint tests fast and hermetic, and what lets a smoke
  deployment answer before the real weights are in place.

Logits come back as a ``numpy`` array of shape ``[batch, num_labels]`` rather
than a torch tensor: everything downstream (calibration, metrics, JSON) is
numpy, and keeping torch inside this module is what lets the rest of the
service — and its tests — run without it.
"""

from __future__ import annotations

import hashlib
import json
import logging
import threading
from abc import ABC, abstractmethod
from collections.abc import Sequence
from typing import Any

import numpy as np

from config.settings import Settings, get_settings

logger = logging.getLogger(__name__)


class ModelInputError(ValueError):
    """The caller's input dict carries nothing the model can score."""


def extract_text(payload: dict[str, Any], text_fields: Sequence[str]) -> str:
    """Pull the text to score out of a caller-supplied input dict.

    The first key in ``text_fields`` holding a non-empty string wins. Failing
    that, every string-valued key is rendered as ``key: value`` in sorted order,
    so a structured payload such as ``{"applicant": "...", "amount": "..."}``
    still produces a stable, readable sequence instead of an error. Non-string
    values are JSON-encoded so numbers and booleans survive.
    """
    if not isinstance(payload, dict) or not payload:
        raise ModelInputError("input must be a non-empty object")

    for field_name in text_fields:
        value = payload.get(field_name)
        if isinstance(value, str) and value.strip():
            return value.strip()

    parts: list[str] = []
    for key in sorted(payload):
        value = payload[key]
        if isinstance(value, str):
            if value.strip():
                parts.append(f"{key}: {value.strip()}")
        elif isinstance(value, (int, float, bool)):
            parts.append(f"{key}: {json.dumps(value)}")
        elif value is not None:
            parts.append(f"{key}: {json.dumps(value, sort_keys=True, default=str)}")

    if not parts:
        raise ModelInputError(
            "input has no scoreable content; provide one of "
            f"{list(text_fields)} or any string-valued field"
        )
    return "\n".join(parts)


class BaseDecisionModel(ABC):
    """A frozen scorer: input dicts in, logits out."""

    @property
    @abstractmethod
    def num_labels(self) -> int:
        """Width of the logit vector."""

    @property
    @abstractmethod
    def name(self) -> str:
        """Identifier reported by /health."""

    @abstractmethod
    def logits(self, inputs: Sequence[dict[str, Any]]) -> np.ndarray:
        """Score a batch. Returns ``[len(inputs), num_labels]`` float64."""

    def logits_one(self, payload: dict[str, Any]) -> np.ndarray:
        """Score a single input. Returns ``[1, num_labels]``."""
        return self.logits([payload])


class HashBackend(BaseDecisionModel):
    """Deterministic stand-in: logits are a hash of the text, not a prediction.

    Same text always yields the same logits, different texts yield different
    ones, and the spread is controlled by ``scale`` — enough structure for
    calibration to have something to calibrate, with no weights to download.
    """

    def __init__(self, num_labels: int = 2, scale: float = 2.0, text_fields: Sequence[str] = ("text",)) -> None:
        self._num_labels = int(num_labels)
        self._scale = float(scale)
        self._text_fields = tuple(text_fields)

    @property
    def num_labels(self) -> int:
        return self._num_labels

    @property
    def name(self) -> str:
        return f"hash[{self._num_labels}]"

    def logits(self, inputs: Sequence[dict[str, Any]]) -> np.ndarray:
        rows = []
        for payload in inputs:
            text = extract_text(payload, self._text_fields)
            digest = hashlib.blake2b(text.encode("utf-8"), digest_size=8 * self._num_labels).digest()
            chunks = [
                int.from_bytes(digest[i * 8 : (i + 1) * 8], "big") for i in range(self._num_labels)
            ]
            # Map each 64-bit chunk to (-scale, scale).
            rows.append([(chunk / 2**63 - 1.0) * self._scale for chunk in chunks])
        return np.asarray(rows, dtype=np.float64)


class HuggingFaceBackend(BaseDecisionModel):
    """A transformers sequence-classification checkpoint."""

    def __init__(self, settings: Settings) -> None:
        import torch
        from transformers import AutoModelForSequenceClassification, AutoTokenizer

        self._torch = torch
        self._settings = settings
        self._name = settings.model_name
        self._device = torch.device(settings.model_device)
        self._lock = threading.Lock()

        logger.info(
            "loading base model", extra={"model_name": settings.model_name, "device": settings.model_device}
        )
        self._tokenizer = AutoTokenizer.from_pretrained(
            settings.model_name,
            revision=settings.model_revision,
            cache_dir=settings.model_cache_dir,
        )
        self._model = AutoModelForSequenceClassification.from_pretrained(
            settings.model_name,
            revision=settings.model_revision,
            cache_dir=settings.model_cache_dir,
            num_labels=settings.model_num_labels,
            # A base checkpoint's head rarely has the requested label count; a
            # fresh head of the right width is exactly what post-hoc calibration
            # is then fitted on top of.
            ignore_mismatched_sizes=True,
        )
        self._model.to(self._device)
        self._model.eval()
        self._num_labels = int(self._model.config.num_labels)
        if self._num_labels != settings.model_num_labels:
            logger.warning(
                "checkpoint exposes %d labels, JEV_MODEL_NUM_LABELS asked for %d; using the checkpoint",
                self._num_labels,
                settings.model_num_labels,
            )

    @property
    def num_labels(self) -> int:
        return self._num_labels

    @property
    def name(self) -> str:
        return self._name

    def logits(self, inputs: Sequence[dict[str, Any]]) -> np.ndarray:
        texts = [extract_text(payload, self._settings.model_text_fields) for payload in inputs]
        if not texts:
            return np.zeros((0, self._num_labels), dtype=np.float64)

        batch_size = self._settings.model_batch_size
        chunks: list[np.ndarray] = []
        # transformers models are not documented as thread-safe for concurrent
        # forward passes on one module; the service is IO-bound, so serialising
        # here costs little and removes the question.
        with self._lock:
            for start in range(0, len(texts), batch_size):
                batch = texts[start : start + batch_size]
                encoded = self._tokenizer(
                    batch,
                    padding=True,
                    truncation=True,
                    max_length=self._settings.model_max_length,
                    return_tensors="pt",
                )
                encoded = {key: value.to(self._device) for key, value in encoded.items()}
                with self._torch.inference_mode():
                    output = self._model(**encoded)
                chunks.append(output.logits.detach().to("cpu", dtype=self._torch.float64).numpy())
        return np.concatenate(chunks, axis=0)


_model_lock = threading.Lock()
_model: BaseDecisionModel | None = None


def build_model(settings: Settings | None = None) -> BaseDecisionModel:
    """Construct the backend the settings name. Does not touch the singleton."""
    settings = settings or get_settings()
    if settings.model_backend == "hash":
        return HashBackend(
            num_labels=settings.model_num_labels,
            text_fields=settings.model_text_fields,
        )
    return HuggingFaceBackend(settings)


def get_model(settings: Settings | None = None) -> BaseDecisionModel:
    """Return the process-wide model, loading it on first use."""
    global _model
    if _model is None:
        with _model_lock:
            if _model is None:
                _model = build_model(settings)
    return _model


def set_model(model: BaseDecisionModel | None) -> None:
    """Install (or clear) the process-wide model. Used by startup and tests."""
    global _model
    with _model_lock:
        _model = model


def is_loaded() -> bool:
    return _model is not None


def extract_logits(payload: dict[str, Any]) -> np.ndarray:
    """Score one input dict with the process-wide model. Returns ``[1, C]``."""
    return get_model().logits_one(payload)


def extract_logits_batch(payloads: Sequence[dict[str, Any]]) -> np.ndarray:
    """Score many input dicts with the process-wide model. Returns ``[N, C]``."""
    return get_model().logits(payloads)
