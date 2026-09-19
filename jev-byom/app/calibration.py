"""Post-hoc probability calibration.

Three calibrators, one per decision type, all fitted after the base model is
frozen:

* **Temperature scaling** — a single scalar ``T`` dividing the logits, fitted
  by LBFGS on the training split. Cheap, keeps the argmax, and fixes most of
  the over-confidence a cross-entropy-trained classifier shows.
* **Isotonic regression** (boolean and enum) — a monotone map from the
  temperature-scaled probability to the empirical frequency, fitted one-vs-rest
  per class. It repairs the shape of the curve that a single temperature
  cannot.
* **Numeric bins** — quantile bins over a scalar score derived from the logits;
  each bin stores the mean label (the value) and the empirical fraction of its
  samples that fell within a tolerance band (the probability).

Nothing here is pickled. Every artifact serialises to plain JSON — isotonic
regressors as their ``(x, y)`` breakpoints — so a calibration written by one
version of scikit-learn stays readable by the next, and loading an artifact
from blob storage can never execute code.
"""

from __future__ import annotations

import math
from collections.abc import Sequence
from dataclasses import dataclass
from typing import Any

import numpy as np

__all__ = [
    "IsotonicCalibrator",
    "NumericCalibrator",
    "TemperatureScaler",
    "accuracy",
    "apply_isotonic",
    "brier_score",
    "coverage",
    "expected_calibration_error",
    "expected_index_score",
    "fit_isotonic",
    "fit_numeric_bins",
    "fit_temperature",
    "fit_temperature_numeric",
    "mean_absolute_error",
    "negative_log_likelihood",
    "softmax",
]

_EPS = 1e-12


# ----------------------------------------------------------------------------
# Numpy helpers
# ----------------------------------------------------------------------------
def softmax(logits: np.ndarray, axis: int = -1) -> np.ndarray:
    """Numerically stable softmax over ``axis``."""
    logits = np.asarray(logits, dtype=np.float64)
    shifted = logits - np.max(logits, axis=axis, keepdims=True)
    exp = np.exp(shifted)
    return exp / np.sum(exp, axis=axis, keepdims=True)


def expected_index_score(probs: np.ndarray) -> np.ndarray:
    """Collapse a distribution over ``C`` ordered classes to a score in [0, 1].

    ``score = Σ_c p_c · c / (C - 1)``: the expected class index, normalised. For
    a binary head this is exactly ``p(class 1)``, so the numeric path degenerates
    to the boolean one when ``C == 2``, which keeps the two comparable.
    """
    probs = np.atleast_2d(np.asarray(probs, dtype=np.float64))
    num_classes = probs.shape[1]
    if num_classes < 2:
        raise ValueError("expected_index_score needs at least two classes")
    indices = np.arange(num_classes, dtype=np.float64)
    return probs @ indices / (num_classes - 1)


# ----------------------------------------------------------------------------
# Temperature scaling
# ----------------------------------------------------------------------------
@dataclass
class TemperatureScaler:
    """A single positive scalar dividing the logits."""

    temperature: float = 1.0

    def __post_init__(self) -> None:
        if not math.isfinite(self.temperature) or self.temperature <= 0:
            raise ValueError(f"temperature must be finite and positive, got {self.temperature!r}")

    def transform(self, logits: np.ndarray) -> np.ndarray:
        """Return ``logits / T``."""
        return np.asarray(logits, dtype=np.float64) / self.temperature

    def probabilities(self, logits: np.ndarray) -> np.ndarray:
        """Return ``softmax(logits / T)``."""
        return softmax(self.transform(logits))

    def to_dict(self) -> dict[str, Any]:
        return {"temperature": float(self.temperature)}

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> TemperatureScaler:
        return cls(temperature=float(payload["temperature"]))


def _lbfgs_minimise(
    loss_fn,
    *,
    init: float,
    lower: float,
    upper: float,
    max_iter: int,
    lr: float,
) -> float:
    """Minimise ``loss_fn(T)`` over ``T ∈ [lower, upper]``.

    Uses torch's LBFGS when torch is importable (the production path, since the
    base model already pulls torch in) and a bounded golden-section search
    otherwise. Both optimise the same one-dimensional objective; the fallback
    keeps the calibration math testable in a torch-free environment.
    """
    log_lower, log_upper = math.log(lower), math.log(upper)

    try:
        import torch
    except ImportError:  # pragma: no cover - exercised only without torch
        return _golden_section(loss_fn, log_lower, log_upper, max_iter)

    param = torch.tensor(
        [math.log(min(max(init, lower), upper))], dtype=torch.float64, requires_grad=True
    )
    optimiser = torch.optim.LBFGS([param], lr=lr, max_iter=max_iter)

    def closure() -> torch.Tensor:
        optimiser.zero_grad()
        temperature = torch.exp(param.clamp(log_lower, log_upper))
        loss = loss_fn(temperature)
        loss.backward()
        return loss

    optimiser.step(closure)
    with torch.no_grad():
        return float(torch.exp(param.clamp(log_lower, log_upper)).item())


def _golden_section(loss_fn, low: float, high: float, max_iter: int) -> float:
    """Golden-section search on log-temperature, for the no-torch fallback."""
    invphi = (math.sqrt(5.0) - 1.0) / 2.0
    a, b = low, high
    c, d = b - invphi * (b - a), a + invphi * (b - a)
    fc, fd = float(loss_fn(math.exp(c))), float(loss_fn(math.exp(d)))
    for _ in range(max(max_iter, 1)):
        if fc < fd:
            b, d, fd = d, c, fc
            c = b - invphi * (b - a)
            fc = float(loss_fn(math.exp(c)))
        else:
            a, c, fc = c, d, fd
            d = a + invphi * (b - a)
            fd = float(loss_fn(math.exp(d)))
        if abs(b - a) < 1e-6:
            break
    return math.exp((a + b) / 2.0)


def fit_temperature(
    logits: np.ndarray,
    labels: np.ndarray,
    *,
    init: float = 1.0,
    lower: float = 0.05,
    upper: float = 20.0,
    max_iter: int = 100,
    lr: float = 0.05,
) -> float:
    """Fit ``T`` by minimising cross-entropy of ``softmax(logits / T)``.

    Returns the fitted temperature clamped to ``[lower, upper]``. The objective
    is convex in ``1/T``, so the optimum is unique and the starting point only
    affects how fast it is reached.
    """
    logits = np.atleast_2d(np.asarray(logits, dtype=np.float64))
    labels = np.asarray(labels, dtype=np.int64).reshape(-1)
    if logits.shape[0] != labels.shape[0]:
        raise ValueError("logits and labels must have the same length")
    if logits.shape[0] == 0:
        raise ValueError("cannot fit a temperature on zero samples")
    if labels.min() < 0 or labels.max() >= logits.shape[1]:
        raise ValueError("labels must index the logit columns")

    try:
        import torch

        t_logits = torch.as_tensor(logits, dtype=torch.float64)
        t_labels = torch.as_tensor(labels, dtype=torch.long)

        def loss_fn(temperature):
            return torch.nn.functional.cross_entropy(t_logits / temperature, t_labels)

    except ImportError:  # pragma: no cover - exercised only without torch

        def loss_fn(temperature):
            probs = softmax(logits / float(temperature))
            picked = probs[np.arange(labels.shape[0]), labels]
            return -np.mean(np.log(np.clip(picked, _EPS, 1.0)))

    return _lbfgs_minimise(
        loss_fn, init=init, lower=lower, upper=upper, max_iter=max_iter, lr=lr
    )


def fit_temperature_numeric(
    logits: np.ndarray,
    targets: np.ndarray,
    *,
    init: float = 1.0,
    lower: float = 0.05,
    upper: float = 20.0,
    max_iter: int = 100,
    lr: float = 0.05,
) -> float:
    """Fit ``T`` for a numeric scenario.

    A numeric scenario has no class labels, so cross-entropy does not apply.
    The objective is instead the squared error between the expected-index score
    of ``softmax(logits / T)`` and the min-max normalised targets — the same
    single parameter, fitted against the quantity the numeric bins consume.
    """
    logits = np.atleast_2d(np.asarray(logits, dtype=np.float64))
    targets = np.asarray(targets, dtype=np.float64).reshape(-1)
    if logits.shape[0] != targets.shape[0]:
        raise ValueError("logits and targets must have the same length")
    if logits.shape[0] == 0:
        raise ValueError("cannot fit a temperature on zero samples")

    lo, hi = float(np.min(targets)), float(np.max(targets))
    span = hi - lo
    normalised = np.zeros_like(targets) if span <= _EPS else (targets - lo) / span

    try:
        import torch

        t_logits = torch.as_tensor(logits, dtype=torch.float64)
        t_targets = torch.as_tensor(normalised, dtype=torch.float64)
        indices = torch.arange(logits.shape[1], dtype=torch.float64)
        denominator = float(logits.shape[1] - 1)

        def loss_fn(temperature):
            probs = torch.softmax(t_logits / temperature, dim=-1)
            score = probs @ indices / denominator
            return torch.mean((score - t_targets) ** 2)

    except ImportError:  # pragma: no cover - exercised only without torch

        def loss_fn(temperature):
            score = expected_index_score(softmax(logits / float(temperature)))
            return float(np.mean((score - normalised) ** 2))

    return _lbfgs_minimise(
        loss_fn, init=init, lower=lower, upper=upper, max_iter=max_iter, lr=lr
    )


# ----------------------------------------------------------------------------
# Isotonic regression
# ----------------------------------------------------------------------------
@dataclass(frozen=True)
class IsotonicCalibrator:
    """A fitted monotone map, stored as its interpolation breakpoints.

    ``scikit-learn``'s ``IsotonicRegression`` predicts by linearly interpolating
    ``(X_thresholds_, y_thresholds_)`` after clipping the input to the fitted
    range. Keeping only those two arrays reproduces ``predict`` exactly for the
    increasing, ``out_of_bounds="clip"`` case used here, and makes the artifact
    plain JSON.
    """

    x: tuple[float, ...]
    y: tuple[float, ...]

    def __post_init__(self) -> None:
        if len(self.x) != len(self.y):
            raise ValueError("isotonic breakpoints must come in pairs")
        if not self.x:
            raise ValueError("isotonic calibrator needs at least one breakpoint")
        if any(b < a for a, b in zip(self.x, self.x[1:], strict=False)):
            raise ValueError("isotonic x breakpoints must be non-decreasing")

    def predict(self, probs: np.ndarray) -> np.ndarray:
        """Map raw probabilities through the fitted curve, clipped to [0, 1]."""
        values = np.asarray(probs, dtype=np.float64)
        xs = np.asarray(self.x, dtype=np.float64)
        ys = np.asarray(self.y, dtype=np.float64)
        clipped = np.clip(values, xs[0], xs[-1])
        return np.clip(np.interp(clipped, xs, ys), 0.0, 1.0)

    def to_dict(self) -> dict[str, Any]:
        return {"x": [float(v) for v in self.x], "y": [float(v) for v in self.y]}

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> IsotonicCalibrator:
        return cls(x=tuple(float(v) for v in payload["x"]), y=tuple(float(v) for v in payload["y"]))


def fit_isotonic(probs: np.ndarray, targets: np.ndarray) -> IsotonicCalibrator:
    """Fit a one-vs-rest isotonic regressor from probabilities to {0, 1} targets."""
    from sklearn.isotonic import IsotonicRegression

    probs = np.asarray(probs, dtype=np.float64).reshape(-1)
    targets = np.asarray(targets, dtype=np.float64).reshape(-1)
    if probs.shape[0] != targets.shape[0]:
        raise ValueError("probs and targets must have the same length")
    if probs.shape[0] < 2:
        raise ValueError("isotonic regression needs at least two samples")

    regressor = IsotonicRegression(y_min=0.0, y_max=1.0, increasing=True, out_of_bounds="clip")
    regressor.fit(probs, targets)
    xs = np.asarray(regressor.X_thresholds_, dtype=np.float64)
    ys = np.asarray(regressor.y_thresholds_, dtype=np.float64)
    if xs.size == 1:
        # A constant fit: widen it to a flat segment so np.interp behaves the
        # same way sklearn's clip-to-range predict does.
        xs = np.array([xs[0], xs[0] + _EPS])
        ys = np.array([ys[0], ys[0]])
    return IsotonicCalibrator(x=tuple(xs.tolist()), y=tuple(ys.tolist()))


def apply_isotonic(
    probs: np.ndarray, calibrators: Sequence[IsotonicCalibrator | None]
) -> np.ndarray:
    """Apply per-class calibrators to a batch of distributions and renormalise.

    Classes with no calibrator keep their temperature-scaled probability. The
    row is renormalised afterwards because one-vs-rest isotonic maps do not
    preserve the simplex; a row that collapses to zero falls back to uniform.
    """
    probs = np.atleast_2d(np.asarray(probs, dtype=np.float64))
    if len(calibrators) != probs.shape[1]:
        raise ValueError("one calibrator (or None) is required per class")

    out = probs.copy()
    for index, calibrator in enumerate(calibrators):
        if calibrator is not None:
            out[:, index] = calibrator.predict(probs[:, index])

    totals = out.sum(axis=1, keepdims=True)
    degenerate = totals[:, 0] <= _EPS
    if np.any(degenerate):
        out[degenerate] = 1.0 / out.shape[1]
        totals = out.sum(axis=1, keepdims=True)
    return out / totals


# ----------------------------------------------------------------------------
# Numeric bin calibration
# ----------------------------------------------------------------------------
@dataclass(frozen=True)
class NumericCalibrator:
    """Quantile bins over the scalar score, each carrying a value and a coverage.

    ``edges`` has ``len(centers) + 1`` entries. ``values[i]`` is the mean label
    of bin ``i`` and ``coverages[i]`` the fraction of that bin's samples whose
    label fell within ``tolerance`` of it — the calibrated probability that a
    decision drawn from that bin is right.
    """

    edges: tuple[float, ...]
    centers: tuple[float, ...]
    values: tuple[float, ...]
    coverages: tuple[float, ...]
    counts: tuple[int, ...]
    tolerance: float
    label_min: float
    label_max: float
    interpolate: bool = True

    def __post_init__(self) -> None:
        n = len(self.centers)
        if n == 0:
            raise ValueError("numeric calibrator needs at least one bin")
        if len(self.edges) != n + 1:
            raise ValueError("edges must have one more entry than centers")
        if not (len(self.values) == len(self.coverages) == len(self.counts) == n):
            raise ValueError("values, coverages and counts must match the bin count")
        if self.tolerance <= 0:
            raise ValueError("tolerance must be positive")

    def bin_index(self, score: float) -> int:
        """Return the bin a score falls into, clamped to the fitted range."""
        edges = np.asarray(self.edges, dtype=np.float64)
        # `right=True`-style placement: a score equal to an interior edge goes to
        # the lower bin, so bins are (lo, hi] apart from the first, which is closed.
        index = int(np.searchsorted(edges[1:-1], float(score), side="left"))
        return min(max(index, 0), len(self.centers) - 1)

    def predict(self, score: float) -> tuple[float, float]:
        """Map a score to ``(value, probability)``."""
        index = self.bin_index(score)
        probability = float(np.clip(self.coverages[index], 0.0, 1.0))
        if self.interpolate and len(self.centers) > 1:
            value = float(
                np.interp(
                    float(score),
                    np.asarray(self.centers, dtype=np.float64),
                    np.asarray(self.values, dtype=np.float64),
                )
            )
        else:
            value = float(self.values[index])
        return float(np.clip(value, self.label_min, self.label_max)), probability

    def predict_many(self, scores: np.ndarray) -> tuple[np.ndarray, np.ndarray]:
        pairs = [self.predict(float(s)) for s in np.asarray(scores, dtype=np.float64).reshape(-1)]
        values = np.array([p[0] for p in pairs], dtype=np.float64)
        probabilities = np.array([p[1] for p in pairs], dtype=np.float64)
        return values, probabilities

    def to_dict(self) -> dict[str, Any]:
        return {
            "edges": [float(v) for v in self.edges],
            "centers": [float(v) for v in self.centers],
            "values": [float(v) for v in self.values],
            "coverages": [float(v) for v in self.coverages],
            "counts": [int(v) for v in self.counts],
            "tolerance": float(self.tolerance),
            "label_min": float(self.label_min),
            "label_max": float(self.label_max),
            "interpolate": bool(self.interpolate),
        }

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> NumericCalibrator:
        return cls(
            edges=tuple(float(v) for v in payload["edges"]),
            centers=tuple(float(v) for v in payload["centers"]),
            values=tuple(float(v) for v in payload["values"]),
            coverages=tuple(float(v) for v in payload["coverages"]),
            counts=tuple(int(v) for v in payload["counts"]),
            tolerance=float(payload["tolerance"]),
            label_min=float(payload["label_min"]),
            label_max=float(payload["label_max"]),
            interpolate=bool(payload.get("interpolate", True)),
        )


def fit_numeric_bins(
    scores: np.ndarray,
    labels: np.ndarray,
    *,
    num_bins: int = 10,
    min_samples_per_bin: int = 3,
    tolerance: float = 0.1,
    tolerance_absolute: bool = False,
    interpolate: bool = True,
) -> NumericCalibrator:
    """Fit quantile bins mapping ``score`` to ``(value, probability)``.

    ``tolerance`` is a fraction of the label range unless ``tolerance_absolute``
    is set, in which case it is used as-is. A degenerate label range (every
    label identical) falls back to an absolute tolerance so the coverage stays
    meaningful rather than collapsing to zero.
    """
    scores = np.asarray(scores, dtype=np.float64).reshape(-1)
    labels = np.asarray(labels, dtype=np.float64).reshape(-1)
    if scores.shape[0] != labels.shape[0]:
        raise ValueError("scores and labels must have the same length")
    if scores.shape[0] == 0:
        raise ValueError("cannot fit numeric bins on zero samples")
    if num_bins < 1:
        raise ValueError("num_bins must be at least 1")

    label_min, label_max = float(np.min(labels)), float(np.max(labels))
    label_span = label_max - label_min
    absolute_tolerance = (
        float(tolerance)
        if tolerance_absolute or label_span <= _EPS
        else float(tolerance) * label_span
    )
    if absolute_tolerance <= 0:
        raise ValueError("tolerance must resolve to a positive width")

    # Cap the bin count so no bin is thinner than min_samples_per_bin by
    # construction; quantile edges then get deduplicated for tied scores.
    effective_bins = max(1, min(int(num_bins), scores.shape[0] // max(min_samples_per_bin, 1)))
    quantiles = np.linspace(0.0, 1.0, effective_bins + 1)
    edges = np.unique(np.quantile(scores, quantiles))
    if edges.size < 2:
        edges = np.array([edges[0], edges[0] + _EPS])
    edges[0], edges[-1] = float(np.min(scores)), float(np.max(scores))
    if edges[-1] <= edges[0]:
        edges = np.array([edges[0], edges[0] + _EPS])

    assignments = np.searchsorted(edges[1:-1], scores, side="left")

    kept_edges: list[float] = [float(edges[0])]
    centers: list[float] = []
    values: list[float] = []
    coverages: list[float] = []
    counts: list[int] = []
    pending: list[int] = []

    for index in range(edges.size - 1):
        pending.append(index)
        mask = np.isin(assignments, pending)
        count = int(np.count_nonzero(mask))
        is_last = index == edges.size - 2
        if count < min_samples_per_bin and not is_last:
            # Too thin: fold it into the next bin by keeping it pending.
            continue
        if count == 0:
            pending = []
            continue
        bin_labels = labels[mask]
        bin_scores = scores[mask]
        mean_label = float(np.mean(bin_labels))
        kept_edges.append(float(edges[index + 1]))
        centers.append(float(np.mean(bin_scores)))
        values.append(mean_label)
        coverages.append(float(np.mean(np.abs(bin_labels - mean_label) <= absolute_tolerance)))
        counts.append(count)
        pending = []

    if pending and counts:
        # Leftovers after the final kept bin: merge them into it.
        mask = np.isin(assignments, pending)
        extra = labels[mask]
        if extra.size:
            merged = np.concatenate([extra, np.full(counts[-1], values[-1])])
            values[-1] = float(np.mean(merged))
            counts[-1] += int(extra.size)
            coverages[-1] = float(
                np.mean(np.abs(merged - values[-1]) <= absolute_tolerance)
            )
        kept_edges[-1] = float(edges[-1])

    if not centers:  # every bin was empty — a single bin over everything
        mean_label = float(np.mean(labels))
        kept_edges = [float(edges[0]), float(edges[-1])]
        centers = [float(np.mean(scores))]
        values = [mean_label]
        coverages = [float(np.mean(np.abs(labels - mean_label) <= absolute_tolerance))]
        counts = [int(labels.size)]
    else:
        kept_edges[-1] = float(edges[-1])

    return NumericCalibrator(
        edges=tuple(kept_edges),
        centers=tuple(centers),
        values=tuple(values),
        coverages=tuple(coverages),
        counts=tuple(counts),
        tolerance=absolute_tolerance,
        label_min=label_min,
        label_max=label_max,
        interpolate=interpolate,
    )


# ----------------------------------------------------------------------------
# Metrics
# ----------------------------------------------------------------------------
def negative_log_likelihood(probs: np.ndarray, labels: np.ndarray) -> float:
    """Mean ``-log p(true class)``."""
    probs = np.atleast_2d(np.asarray(probs, dtype=np.float64))
    labels = np.asarray(labels, dtype=np.int64).reshape(-1)
    picked = probs[np.arange(labels.shape[0]), labels]
    return float(-np.mean(np.log(np.clip(picked, _EPS, 1.0))))


def brier_score(probs: np.ndarray, labels: np.ndarray) -> float:
    """Multiclass Brier score: mean squared error against the one-hot target."""
    probs = np.atleast_2d(np.asarray(probs, dtype=np.float64))
    labels = np.asarray(labels, dtype=np.int64).reshape(-1)
    onehot = np.zeros_like(probs)
    onehot[np.arange(labels.shape[0]), labels] = 1.0
    return float(np.mean(np.sum((probs - onehot) ** 2, axis=1)))


def expected_calibration_error(
    probs: np.ndarray, labels: np.ndarray, *, num_bins: int = 10
) -> float:
    """Top-label ECE over equal-width confidence bins."""
    probs = np.atleast_2d(np.asarray(probs, dtype=np.float64))
    labels = np.asarray(labels, dtype=np.int64).reshape(-1)
    confidences = probs.max(axis=1)
    predictions = probs.argmax(axis=1)
    correct = (predictions == labels).astype(np.float64)

    edges = np.linspace(0.0, 1.0, num_bins + 1)
    total = 0.0
    for lower, upper in zip(edges[:-1], edges[1:], strict=False):
        mask = (confidences > lower) & (confidences <= upper)
        if lower == 0.0:
            mask |= confidences == 0.0
        if not np.any(mask):
            continue
        weight = np.mean(mask)
        total += weight * abs(np.mean(correct[mask]) - np.mean(confidences[mask]))
    return float(total)


def accuracy(probs: np.ndarray, labels: np.ndarray) -> float:
    probs = np.atleast_2d(np.asarray(probs, dtype=np.float64))
    labels = np.asarray(labels, dtype=np.int64).reshape(-1)
    return float(np.mean(probs.argmax(axis=1) == labels))


def mean_absolute_error(values: np.ndarray, labels: np.ndarray) -> float:
    values = np.asarray(values, dtype=np.float64).reshape(-1)
    labels = np.asarray(labels, dtype=np.float64).reshape(-1)
    return float(np.mean(np.abs(values - labels)))


def coverage(values: np.ndarray, labels: np.ndarray, tolerance: float) -> float:
    """Fraction of predictions landing within ``tolerance`` of the label."""
    values = np.asarray(values, dtype=np.float64).reshape(-1)
    labels = np.asarray(labels, dtype=np.float64).reshape(-1)
    return float(np.mean(np.abs(values - labels) <= tolerance))
