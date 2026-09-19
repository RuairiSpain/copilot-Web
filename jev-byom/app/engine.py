"""Training and inference orchestration.

``server.py`` owns HTTP concerns — auth, status codes, logging — and this
module owns the pipeline: labels in, artifacts out; logits in, a typed decision
out. Keeping them apart is what lets the pipeline be unit-tested without an
HTTP client and reused from a script or a batch job.
"""

from __future__ import annotations

import logging
import time
from collections.abc import Sequence
from dataclasses import dataclass
from typing import Any

import numpy as np

from app import calibration as cal
from app.calibration_registry import CalibrationBundle, CalibrationRegistry, new_version
from app.model import BaseDecisionModel
from app.schemas import (
    DecisionRequest,
    DecisionResponse,
    FitMetrics,
    PosthocTrainRequest,
    PosthocTrainResponse,
    TrainSample,
)
from config.settings import Settings

logger = logging.getLogger(__name__)

_TRUE_STRINGS = {"true", "yes", "1", "y", "t"}
_FALSE_STRINGS = {"false", "no", "0", "n", "f"}


class EngineError(RuntimeError):
    """A request cannot be served as asked. Carries an HTTP status hint."""

    def __init__(self, message: str, *, status_code: int = 400, error: str = "bad_request") -> None:
        super().__init__(message)
        self.status_code = status_code
        self.error = error


class CalibrationMissingError(EngineError):
    """No calibration exists for the requested scenario."""

    def __init__(self, scenario: str, decision_type: str) -> None:
        super().__init__(
            f"no calibration found for scenario {scenario!r} ({decision_type}); "
            "train it first with POST /posthoc_train",
            status_code=424,
            error="calibration_not_found",
        )


# ----------------------------------------------------------------------------
# Labels
# ----------------------------------------------------------------------------
def coerce_boolean_label(label: Any) -> int:
    """Map a boolean label onto {0, 1}."""
    if isinstance(label, bool):
        return int(label)
    if isinstance(label, (int, float)):
        if float(label) in (0.0, 1.0):
            return int(label)
        raise EngineError(f"boolean label must be 0 or 1, got {label!r}")
    text = str(label).strip().lower()
    if text in _TRUE_STRINGS:
        return 1
    if text in _FALSE_STRINGS:
        return 0
    raise EngineError(f"cannot read {label!r} as a boolean label")


def resolve_enum_labels(
    samples: Sequence[TrainSample], class_names: list[str] | None
) -> tuple[np.ndarray, list[str] | None, int]:
    """Map enum labels to indices, returning ``(indices, class_names, num_classes)``.

    String labels resolve against ``class_names`` when given; otherwise the
    distinct strings are sorted to define a stable ordering. Integer labels are
    used as indices directly, and the two styles may not be mixed — a payload
    that does is a caller bug worth reporting rather than guessing at.
    """
    raw = [sample.label for sample in samples]
    has_strings = any(isinstance(label, str) for label in raw)
    has_numbers = any(isinstance(label, (int, float)) and not isinstance(label, bool) for label in raw)
    if has_strings and has_numbers:
        raise EngineError("enum labels must be all names or all indices, not a mix")

    if has_strings:
        names = list(class_names) if class_names else sorted({str(label) for label in raw})
        lookup = {name: index for index, name in enumerate(names)}
        try:
            indices = np.array([lookup[str(label)] for label in raw], dtype=np.int64)
        except KeyError as exc:
            raise EngineError(f"label {exc.args[0]!r} is not in class_names {names}") from None
        return indices, names, len(names)

    indices = np.array([int(label) for label in raw], dtype=np.int64)
    if indices.min() < 0:
        raise EngineError("enum label indices must be non-negative")
    num_classes = max(int(indices.max()) + 1, len(class_names) if class_names else 0, 2)
    if class_names and len(class_names) < int(indices.max()) + 1:
        raise EngineError(
            f"class_names has {len(class_names)} entries but label index "
            f"{int(indices.max())} was used"
        )
    return indices, (list(class_names) if class_names else None), num_classes


def coerce_numeric_label(label: Any) -> float:
    if isinstance(label, bool):
        return float(int(label))
    try:
        value = float(label)
    except (TypeError, ValueError):
        raise EngineError(f"cannot read {label!r} as a numeric label") from None
    if not np.isfinite(value):
        raise EngineError("numeric labels must be finite")
    return value


# ----------------------------------------------------------------------------
# Helpers
# ----------------------------------------------------------------------------
def reduce_logits(logits: np.ndarray, num_classes: int) -> np.ndarray:
    """Narrow a wider logit vector to the scenario's class count.

    A single base model can back scenarios with different class counts; the
    first ``num_classes`` columns are the scenario's. Asking for more columns
    than the model exposes is a configuration error, not something to pad.
    """
    logits = np.atleast_2d(np.asarray(logits, dtype=np.float64))
    available = logits.shape[1]
    if num_classes > available:
        raise EngineError(
            f"scenario needs {num_classes} classes but the base model exposes "
            f"{available}; raise JEV_MODEL_NUM_LABELS and retrain",
            status_code=409,
            error="model_mismatch",
        )
    return logits if num_classes == available else logits[:, :num_classes]


def stratified_split(
    labels: np.ndarray, validation_split: float, seed: int
) -> tuple[np.ndarray, np.ndarray]:
    """Split indices into (train, validation), keeping every class in both sides.

    A class with a single example stays in training: a holdout that contains a
    class the fit never saw measures nothing useful.
    """
    rng = np.random.default_rng(seed)
    total = labels.shape[0]
    if validation_split <= 0.0:
        indices = rng.permutation(total)
        return indices, np.array([], dtype=np.int64)

    train_parts: list[np.ndarray] = []
    validation_parts: list[np.ndarray] = []
    for value in np.unique(labels):
        members = rng.permutation(np.flatnonzero(labels == value))
        take = int(round(len(members) * validation_split))
        take = min(take, max(len(members) - 1, 0))
        validation_parts.append(members[:take])
        train_parts.append(members[take:])

    train = np.concatenate(train_parts) if train_parts else np.array([], dtype=np.int64)
    validation = (
        np.concatenate(validation_parts) if validation_parts else np.array([], dtype=np.int64)
    )
    return rng.permutation(train), rng.permutation(validation)


def random_split(total: int, validation_split: float, seed: int) -> tuple[np.ndarray, np.ndarray]:
    """Unstratified split, used by numeric scenarios."""
    rng = np.random.default_rng(seed)
    indices = rng.permutation(total)
    if validation_split <= 0.0:
        return indices, np.array([], dtype=np.int64)
    take = min(int(round(total * validation_split)), max(total - 2, 0))
    return indices[take:], indices[:take]


@dataclass
class _Scored:
    """Logits for every training sample, plus the labels they go with."""

    logits: np.ndarray
    class_labels: np.ndarray | None = None
    numeric_labels: np.ndarray | None = None


# ----------------------------------------------------------------------------
# Training
# ----------------------------------------------------------------------------
def train_scenario(
    request: PosthocTrainRequest,
    *,
    model: BaseDecisionModel,
    registry: CalibrationRegistry,
    settings: Settings,
) -> PosthocTrainResponse:
    """Fit and persist calibration for one scenario. Implements POST /posthoc_train."""
    started = time.perf_counter()
    samples = request.training_data
    if len(samples) < settings.train_min_samples:
        raise EngineError(
            f"need at least {settings.train_min_samples} samples to calibrate "
            f"(JEV_TRAIN_MIN_SAMPLES), got {len(samples)}",
            status_code=422,
            error="insufficient_data",
        )
    if len(samples) > settings.train_max_samples:
        raise EngineError(
            f"at most {settings.train_max_samples} samples per call "
            f"(JEV_TRAIN_MAX_SAMPLES), got {len(samples)}",
            status_code=413,
            error="too_much_data",
        )

    validation_split = (
        request.validation_split if request.validation_split is not None else settings.validation_split
    )
    raw_logits = model.logits([sample.input for sample in samples])

    if request.decision_type == "numeric":
        return _train_numeric(
            request,
            raw_logits=raw_logits,
            validation_split=validation_split,
            registry=registry,
            settings=settings,
            started=started,
        )
    return _train_classification(
        request,
        raw_logits=raw_logits,
        validation_split=validation_split,
        registry=registry,
        settings=settings,
        started=started,
    )


def _train_classification(
    request: PosthocTrainRequest,
    *,
    raw_logits: np.ndarray,
    validation_split: float,
    registry: CalibrationRegistry,
    settings: Settings,
    started: float,
) -> PosthocTrainResponse:
    if request.decision_type == "boolean":
        labels = np.array([coerce_boolean_label(s.label) for s in request.training_data], dtype=np.int64)
        class_names: list[str] | None = None
        num_classes = 2
    else:
        labels, class_names, num_classes = resolve_enum_labels(
            request.training_data, request.class_names
        )

    if len(np.unique(labels)) < 2:
        raise EngineError(
            "training data contains a single class; calibration needs both outcomes",
            status_code=422,
            error="insufficient_data",
        )

    logits = reduce_logits(raw_logits, num_classes)
    if labels.max() >= num_classes:
        raise EngineError(f"label index {int(labels.max())} exceeds {num_classes} classes")

    train_idx, validation_idx = stratified_split(labels, validation_split, settings.random_seed)
    train_logits, train_labels = logits[train_idx], labels[train_idx]

    temperature = cal.fit_temperature(
        train_logits,
        train_labels,
        init=settings.temperature_init,
        lower=settings.temperature_min,
        upper=settings.temperature_max,
        max_iter=settings.temperature_max_iter,
        lr=settings.temperature_lr,
    )
    scaler = cal.TemperatureScaler(temperature)

    scaled_train = scaler.probabilities(train_logits)
    calibrators: list[cal.IsotonicCalibrator | None] = []
    for class_index in range(num_classes):
        targets = (train_labels == class_index).astype(np.float64)
        positives = int(targets.sum())
        if len(targets) < settings.isotonic_min_samples or positives == 0 or positives == len(targets):
            # Too little signal for a monotone fit; the temperature-scaled
            # probability is the safer answer for this class.
            calibrators.append(None)
            continue
        calibrators.append(cal.fit_isotonic(scaled_train[:, class_index], targets))

    version = new_version()
    artifacts = [
        registry.save_temperature(request.scenario, request.decision_type, version, scaler),
        registry.save_isotonic(
            request.scenario, request.decision_type, version, calibrators, class_names
        ),
    ]

    evaluation_idx = validation_idx if validation_idx.size else train_idx
    split_name = "validation" if validation_idx.size else "train"
    eval_logits, eval_labels = logits[evaluation_idx], labels[evaluation_idx]
    before = cal.softmax(eval_logits)
    after = cal.apply_isotonic(scaler.probabilities(eval_logits), calibrators)

    metrics_before = _classification_metrics(before, eval_labels, split_name)
    metrics_after = _classification_metrics(after, eval_labels, split_name)

    registry.commit_version(
        request.scenario,
        request.decision_type,
        version,
        artifacts=artifacts,
        num_classes=num_classes,
        class_names=class_names,
        metadata={
            "num_samples": len(request.training_data),
            "num_train_samples": int(train_idx.size),
            "num_validation_samples": int(validation_idx.size),
            "temperature": float(temperature),
            "isotonic_classes": [i for i, c in enumerate(calibrators) if c is not None],
            "metrics_before": metrics_before.model_dump(),
            "metrics_after": metrics_after.model_dump(),
            "notes": request.notes,
        },
    )
    registry.clear_cache(request.scenario, request.decision_type)

    logger.info(
        "calibration fitted",
        extra={
            "scenario": request.scenario,
            "decision_type": request.decision_type,
            "calibration_version": version,
            "temperature": float(temperature),
            "num_samples": len(request.training_data),
        },
    )

    return PosthocTrainResponse(
        scenario=request.scenario,
        decision_type=request.decision_type,
        num_classes=num_classes,
        class_names=class_names,
        num_samples=len(request.training_data),
        num_train_samples=int(train_idx.size),
        num_validation_samples=int(validation_idx.size),
        temperature=float(temperature),
        calibration_version=version,
        artifacts=[key.rsplit("/", 1)[-1] for key in artifacts],
        metrics_before=metrics_before,
        metrics_after=metrics_after,
        duration_ms=(time.perf_counter() - started) * 1000.0,
    )


def _train_numeric(
    request: PosthocTrainRequest,
    *,
    raw_logits: np.ndarray,
    validation_split: float,
    registry: CalibrationRegistry,
    settings: Settings,
    started: float,
) -> PosthocTrainResponse:
    labels = np.array(
        [coerce_numeric_label(s.label) for s in request.training_data], dtype=np.float64
    )
    logits = np.atleast_2d(np.asarray(raw_logits, dtype=np.float64))
    num_classes = logits.shape[1]

    train_idx, validation_idx = random_split(
        labels.shape[0], validation_split, settings.random_seed
    )
    temperature = cal.fit_temperature_numeric(
        logits[train_idx],
        labels[train_idx],
        init=settings.temperature_init,
        lower=settings.temperature_min,
        upper=settings.temperature_max,
        max_iter=settings.temperature_max_iter,
        lr=settings.temperature_lr,
    )
    scaler = cal.TemperatureScaler(temperature)
    train_scores = cal.expected_index_score(scaler.probabilities(logits[train_idx]))

    calibrator = cal.fit_numeric_bins(
        train_scores,
        labels[train_idx],
        num_bins=request.numeric_bins or settings.numeric_bins,
        min_samples_per_bin=settings.numeric_min_samples_per_bin,
        tolerance=request.numeric_tolerance or settings.numeric_tolerance,
        tolerance_absolute=settings.numeric_tolerance_absolute,
    )

    version = new_version()
    artifacts = [
        registry.save_temperature(request.scenario, request.decision_type, version, scaler),
        registry.save_numeric(request.scenario, request.decision_type, version, calibrator),
    ]

    evaluation_idx = validation_idx if validation_idx.size else train_idx
    split_name = "validation" if validation_idx.size else "train"
    eval_labels = labels[evaluation_idx]
    eval_scores = cal.expected_index_score(scaler.probabilities(logits[evaluation_idx]))
    predicted, probabilities = calibrator.predict_many(eval_scores)

    # "Before" is the untuned baseline the calibration has to beat: the raw
    # score stretched onto the label range seen in training.
    baseline_scores = cal.expected_index_score(cal.softmax(logits[evaluation_idx]))
    baseline = calibrator.label_min + baseline_scores * (calibrator.label_max - calibrator.label_min)

    metrics_before = FitMetrics(
        split=split_name,
        num_samples=int(evaluation_idx.size),
        mean_absolute_error=cal.mean_absolute_error(baseline, eval_labels),
        coverage=cal.coverage(baseline, eval_labels, calibrator.tolerance),
    )
    metrics_after = FitMetrics(
        split=split_name,
        num_samples=int(evaluation_idx.size),
        mean_absolute_error=cal.mean_absolute_error(predicted, eval_labels),
        coverage=cal.coverage(predicted, eval_labels, calibrator.tolerance),
        expected_calibration_error=float(
            abs(
                np.mean(probabilities)
                - cal.coverage(predicted, eval_labels, calibrator.tolerance)
            )
        )
        if evaluation_idx.size
        else None,
    )

    registry.commit_version(
        request.scenario,
        request.decision_type,
        version,
        artifacts=artifacts,
        num_classes=num_classes,
        class_names=None,
        metadata={
            "num_samples": len(request.training_data),
            "num_train_samples": int(train_idx.size),
            "num_validation_samples": int(validation_idx.size),
            "temperature": float(temperature),
            "num_bins": len(calibrator.centers),
            "tolerance": calibrator.tolerance,
            "label_range": [calibrator.label_min, calibrator.label_max],
            "metrics_before": metrics_before.model_dump(),
            "metrics_after": metrics_after.model_dump(),
            "notes": request.notes,
        },
    )
    registry.clear_cache(request.scenario, request.decision_type)

    logger.info(
        "calibration fitted",
        extra={
            "scenario": request.scenario,
            "decision_type": request.decision_type,
            "calibration_version": version,
            "temperature": float(temperature),
            "num_samples": len(request.training_data),
        },
    )

    return PosthocTrainResponse(
        scenario=request.scenario,
        decision_type=request.decision_type,
        num_classes=num_classes,
        class_names=None,
        num_samples=len(request.training_data),
        num_train_samples=int(train_idx.size),
        num_validation_samples=int(validation_idx.size),
        temperature=float(temperature),
        calibration_version=version,
        artifacts=[key.rsplit("/", 1)[-1] for key in artifacts],
        metrics_before=metrics_before,
        metrics_after=metrics_after,
        duration_ms=(time.perf_counter() - started) * 1000.0,
    )


def _classification_metrics(probs: np.ndarray, labels: np.ndarray, split: str) -> FitMetrics:
    return FitMetrics(
        split=split,  # type: ignore[arg-type]
        num_samples=int(labels.shape[0]),
        negative_log_likelihood=cal.negative_log_likelihood(probs, labels),
        brier_score=cal.brier_score(probs, labels),
        expected_calibration_error=cal.expected_calibration_error(probs, labels),
        accuracy=cal.accuracy(probs, labels),
    )


# ----------------------------------------------------------------------------
# Inference
# ----------------------------------------------------------------------------
def decide(
    request: DecisionRequest,
    *,
    model: BaseDecisionModel,
    registry: CalibrationRegistry,
    settings: Settings,
) -> DecisionResponse:
    """Produce one typed, calibrated decision. Implements POST /decision."""
    started = time.perf_counter()
    bundle = registry.load_calibration(request.scenario, request.decision_type)

    if bundle is None:
        if settings.require_calibration or request.decision_type == "numeric":
            # A numeric decision has no meaning without calibration: the label
            # scale lives in the bins, not in the model.
            raise CalibrationMissingError(request.scenario, request.decision_type)
        logger.warning(
            "serving uncalibrated decision",
            extra={"scenario": request.scenario, "decision_type": request.decision_type},
        )

    logits = model.logits_one(request.model_input())

    if request.decision_type == "numeric":
        return _decide_numeric(request, logits, bundle, started)  # type: ignore[arg-type]
    return _decide_classification(request, logits, bundle, settings, started)


def _decide_classification(
    request: DecisionRequest,
    logits: np.ndarray,
    bundle: CalibrationBundle | None,
    settings: Settings,
    started: float,
) -> DecisionResponse:
    num_classes = 2 if request.decision_type == "boolean" else (bundle.num_classes if bundle else None)
    if num_classes is None:
        num_classes = np.atleast_2d(logits).shape[1]
    reduced = reduce_logits(logits, num_classes)

    if bundle is None:
        probs = cal.softmax(reduced)
    else:
        probs = bundle.temperature.probabilities(reduced)
        if bundle.isotonic:
            if len(bundle.isotonic) != num_classes:
                raise EngineError(
                    f"stored calibration covers {len(bundle.isotonic)} classes but the "
                    f"decision needs {num_classes}; retrain the scenario",
                    status_code=409,
                    error="model_mismatch",
                )
            probs = cal.apply_isotonic(probs, bundle.isotonic)

    row = probs[0]
    if request.decision_type == "boolean":
        probability_true = float(row[1])
        threshold = request.threshold if request.threshold is not None else settings.boolean_threshold
        value: bool | int = probability_true >= threshold
        if settings.boolean_probability == "true":
            probability = probability_true
        else:
            probability = probability_true if value else 1.0 - probability_true
        label = None
        distribution = None
    else:
        index = int(np.argmax(row))
        value = index
        probability = float(row[index])
        label = bundle.label_for(index) if bundle else str(index)
        distribution = (
            {
                (bundle.label_for(i) if bundle else str(i)): float(row[i])
                for i in range(num_classes)
            }
            if request.include_probabilities
            else None
        )

    return DecisionResponse(
        value=value,
        probability=float(np.clip(probability, 0.0, 1.0)),
        scenario=request.scenario,
        decision_type=request.decision_type,
        label=label,
        probabilities=distribution,
        calibrated=bundle is not None,
        calibration_version=bundle.version if bundle else None,
        latency_ms=(time.perf_counter() - started) * 1000.0,
    )


def _decide_numeric(
    request: DecisionRequest,
    logits: np.ndarray,
    bundle: CalibrationBundle,
    started: float,
) -> DecisionResponse:
    if bundle.numeric is None:
        raise EngineError(
            f"scenario {request.scenario!r} is registered as numeric but has no numeric "
            "artifact; retrain it",
            status_code=409,
            error="model_mismatch",
        )
    reduced = reduce_logits(logits, bundle.num_classes or np.atleast_2d(logits).shape[1])
    score = float(cal.expected_index_score(bundle.temperature.probabilities(reduced))[0])
    value, probability = bundle.numeric.predict(score)
    return DecisionResponse(
        value=float(value),
        probability=float(np.clip(probability, 0.0, 1.0)),
        scenario=request.scenario,
        decision_type=request.decision_type,
        calibrated=True,
        calibration_version=bundle.version,
        latency_ms=(time.perf_counter() - started) * 1000.0,
    )
