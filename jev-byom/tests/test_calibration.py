"""Unit tests for the calibration math.

These are the tests that matter most: a silent bug here does not crash, it
returns a confident wrong probability. Each one therefore checks a property
that must hold (monotonicity, normalisation, a bounded range) or compares
against an independently computed value rather than against a golden output.
"""

from __future__ import annotations

import math

import numpy as np
import pytest

from app import calibration as cal
from tests.conftest import make_overconfident_logits


# ----------------------------------------------------------------------------
# softmax / scores
# ----------------------------------------------------------------------------
def test_softmax_rows_sum_to_one_and_survive_large_logits():
    probs = cal.softmax(np.array([[1000.0, 999.0, 0.0], [-1000.0, -1001.0, 0.0]]))
    assert np.allclose(probs.sum(axis=1), 1.0)
    assert np.all(np.isfinite(probs))


def test_softmax_matches_hand_computation():
    probs = cal.softmax(np.array([[0.0, math.log(3.0)]]))
    assert probs[0, 0] == pytest.approx(0.25)
    assert probs[0, 1] == pytest.approx(0.75)


def test_expected_index_score_equals_p1_for_binary():
    probs = np.array([[0.3, 0.7], [0.9, 0.1]])
    assert cal.expected_index_score(probs) == pytest.approx([0.7, 0.1])


def test_expected_index_score_stays_in_unit_interval():
    rng = np.random.default_rng(0)
    probs = cal.softmax(rng.normal(size=(200, 5)))
    scores = cal.expected_index_score(probs)
    assert scores.min() >= 0.0 and scores.max() <= 1.0


def test_expected_index_score_rejects_single_class():
    with pytest.raises(ValueError):
        cal.expected_index_score(np.ones((3, 1)))


# ----------------------------------------------------------------------------
# Temperature scaling
# ----------------------------------------------------------------------------
def test_temperature_scaler_rejects_non_positive():
    with pytest.raises(ValueError):
        cal.TemperatureScaler(0.0)
    with pytest.raises(ValueError):
        cal.TemperatureScaler(-1.0)


def test_temperature_scaler_roundtrips_through_json_shape():
    scaler = cal.TemperatureScaler(2.5)
    assert cal.TemperatureScaler.from_dict(scaler.to_dict()).temperature == 2.5


def test_temperature_scaling_preserves_argmax():
    logits = np.array([[3.0, 1.0, -2.0], [0.1, 0.2, 0.15]])
    scaled = cal.TemperatureScaler(4.0).probabilities(logits)
    assert np.array_equal(scaled.argmax(axis=1), cal.softmax(logits).argmax(axis=1))


def test_fit_temperature_softens_overconfident_logits():
    logits, labels = make_overconfident_logits(600, 3, seed=1)
    temperature = cal.fit_temperature(logits, labels)

    assert temperature > 1.0, "over-confident logits need T > 1"
    before = cal.negative_log_likelihood(cal.softmax(logits), labels)
    after = cal.negative_log_likelihood(
        cal.TemperatureScaler(temperature).probabilities(logits), labels
    )
    assert after < before
    assert cal.expected_calibration_error(
        cal.TemperatureScaler(temperature).probabilities(logits), labels
    ) < cal.expected_calibration_error(cal.softmax(logits), labels)


def test_fit_temperature_sharpens_underconfident_logits():
    """Logits squashed toward zero are under-confident, so the fit wants T < 1."""
    logits, labels = make_overconfident_logits(600, 3, seed=2, scale=6.0, noise=0.5)
    temperature = cal.fit_temperature(logits * 0.05, labels)
    assert temperature < 1.0


def test_fit_temperature_is_near_optimal_against_a_grid():
    """The fitted T should beat every point on a dense grid, up to rounding."""
    logits, labels = make_overconfident_logits(400, 4, seed=3)
    fitted = cal.fit_temperature(logits, labels)
    fitted_nll = cal.negative_log_likelihood(
        cal.TemperatureScaler(fitted).probabilities(logits), labels
    )
    grid = np.linspace(0.1, 10.0, 200)
    best = min(
        cal.negative_log_likelihood(cal.TemperatureScaler(float(t)).probabilities(logits), labels)
        for t in grid
    )
    assert fitted_nll <= best + 1e-4


def test_fit_temperature_respects_bounds():
    logits, labels = make_overconfident_logits(200, 3, seed=4)
    temperature = cal.fit_temperature(logits, labels, lower=1.5, upper=1.8, init=1.6)
    assert 1.5 <= temperature <= 1.8


def test_fit_temperature_rejects_bad_shapes():
    logits, labels = make_overconfident_logits(20, 3, seed=5)
    with pytest.raises(ValueError):
        cal.fit_temperature(logits, labels[:-1])
    with pytest.raises(ValueError):
        cal.fit_temperature(logits, np.full_like(labels, 99))
    with pytest.raises(ValueError):
        cal.fit_temperature(np.zeros((0, 3)), np.zeros((0,), dtype=np.int64))


def test_fit_temperature_numeric_reduces_squared_error():
    rng = np.random.default_rng(6)
    logits = rng.normal(size=(300, 4)) * 4.0
    scores = cal.expected_index_score(cal.softmax(logits / 3.0))
    targets = 10.0 + 20.0 * scores + rng.normal(0.0, 0.2, size=scores.shape)

    temperature = cal.fit_temperature_numeric(logits, targets)
    normalised = (targets - targets.min()) / (targets.max() - targets.min())

    def mse(t: float) -> float:
        return float(
            np.mean((cal.expected_index_score(cal.softmax(logits / t)) - normalised) ** 2)
        )

    assert mse(temperature) <= mse(1.0) + 1e-9
    assert mse(temperature) <= min(mse(float(t)) for t in np.linspace(0.2, 8.0, 100)) + 1e-4


def test_fit_temperature_numeric_handles_constant_targets():
    logits = np.random.default_rng(7).normal(size=(50, 3))
    temperature = cal.fit_temperature_numeric(logits, np.full(50, 4.2))
    assert math.isfinite(temperature) and temperature > 0


# ----------------------------------------------------------------------------
# Isotonic regression
# ----------------------------------------------------------------------------
def test_fit_isotonic_matches_sklearn_predict_after_serialisation():
    """The stored breakpoints must reproduce sklearn's own predict exactly."""
    from sklearn.isotonic import IsotonicRegression

    rng = np.random.default_rng(8)
    probs = rng.random(300)
    targets = (rng.random(300) < probs**2).astype(float)

    reference = IsotonicRegression(y_min=0.0, y_max=1.0, increasing=True, out_of_bounds="clip")
    reference.fit(probs, targets)

    calibrator = cal.IsotonicCalibrator.from_dict(cal.fit_isotonic(probs, targets).to_dict())
    query = np.concatenate([probs, np.array([-0.5, 0.0, 1.0, 1.5])])
    assert np.allclose(calibrator.predict(query), reference.predict(query), atol=1e-9)


def test_isotonic_output_is_monotone_and_bounded():
    rng = np.random.default_rng(9)
    probs = rng.random(200)
    targets = (rng.random(200) < probs).astype(float)
    calibrator = cal.fit_isotonic(probs, targets)

    query = np.linspace(0.0, 1.0, 101)
    mapped = calibrator.predict(query)
    assert np.all(np.diff(mapped) >= -1e-12), "isotonic map must be non-decreasing"
    assert mapped.min() >= 0.0 and mapped.max() <= 1.0


def test_isotonic_corrects_a_systematically_skewed_probability():
    """Probabilities that claim 0.9 while being right 45% of the time get pulled down."""
    rng = np.random.default_rng(10)
    probs = np.clip(rng.normal(0.9, 0.02, size=400), 0.0, 1.0)
    targets = (rng.random(400) < 0.45).astype(float)
    calibrator = cal.fit_isotonic(probs, targets)
    assert calibrator.predict(np.array([0.9]))[0] == pytest.approx(0.45, abs=0.08)


def test_isotonic_handles_a_constant_fit():
    calibrator = cal.fit_isotonic(np.full(10, 0.3), np.ones(10))
    assert calibrator.predict(np.array([0.0, 0.3, 1.0])) == pytest.approx([1.0, 1.0, 1.0])


def test_isotonic_rejects_mismatched_inputs():
    with pytest.raises(ValueError):
        cal.fit_isotonic(np.array([0.1, 0.2]), np.array([1.0]))
    with pytest.raises(ValueError):
        cal.fit_isotonic(np.array([0.1]), np.array([1.0]))


def test_isotonic_calibrator_validates_breakpoints():
    with pytest.raises(ValueError):
        cal.IsotonicCalibrator(x=(0.1, 0.2), y=(0.3,))
    with pytest.raises(ValueError):
        cal.IsotonicCalibrator(x=(), y=())
    with pytest.raises(ValueError):
        cal.IsotonicCalibrator(x=(0.5, 0.1), y=(0.0, 1.0))


def test_apply_isotonic_renormalises_and_passes_through_none():
    probs = np.array([[0.2, 0.3, 0.5]])
    calibrators = [cal.IsotonicCalibrator(x=(0.0, 1.0), y=(0.0, 0.5)), None, None]
    out = cal.apply_isotonic(probs, calibrators)

    assert out.shape == probs.shape
    assert out.sum() == pytest.approx(1.0)
    # class 0 was halved, so it must have lost share to the untouched classes.
    assert out[0, 0] < probs[0, 0]


def test_apply_isotonic_falls_back_to_uniform_when_a_row_collapses():
    flat = cal.IsotonicCalibrator(x=(0.0, 1.0), y=(0.0, 0.0))
    out = cal.apply_isotonic(np.array([[0.4, 0.6]]), [flat, flat])
    assert out[0] == pytest.approx([0.5, 0.5])


def test_apply_isotonic_requires_one_calibrator_per_class():
    with pytest.raises(ValueError):
        cal.apply_isotonic(np.array([[0.5, 0.5]]), [None])


# ----------------------------------------------------------------------------
# Numeric bins
# ----------------------------------------------------------------------------
def _numeric_fixture(num_samples: int = 200, seed: int = 11):
    rng = np.random.default_rng(seed)
    scores = rng.random(num_samples)
    labels = 5.0 + 10.0 * scores + rng.normal(0.0, 0.3, size=num_samples)
    return scores, labels


def test_numeric_bins_track_the_underlying_relationship():
    scores, labels = _numeric_fixture()
    calibrator = cal.fit_numeric_bins(scores, labels, num_bins=10, tolerance=0.1)

    low, _ = calibrator.predict(0.05)
    high, _ = calibrator.predict(0.95)
    assert low < high
    assert low == pytest.approx(5.0, abs=1.5)
    assert high == pytest.approx(15.0, abs=1.5)


def test_numeric_bin_values_are_non_decreasing_for_a_monotone_signal():
    scores, labels = _numeric_fixture()
    calibrator = cal.fit_numeric_bins(scores, labels, num_bins=8, tolerance=0.1)
    assert np.all(np.diff(np.asarray(calibrator.values)) >= -0.5)


def test_numeric_predictions_stay_inside_the_observed_label_range():
    scores, labels = _numeric_fixture()
    calibrator = cal.fit_numeric_bins(scores, labels, num_bins=6, tolerance=0.1)
    for score in (-5.0, 0.0, 0.5, 1.0, 5.0):
        value, probability = calibrator.predict(score)
        assert calibrator.label_min <= value <= calibrator.label_max
        assert 0.0 <= probability <= 1.0


def test_numeric_counts_account_for_every_training_sample():
    scores, labels = _numeric_fixture(150, seed=12)
    calibrator = cal.fit_numeric_bins(scores, labels, num_bins=7, tolerance=0.1)
    assert sum(calibrator.counts) == 150
    assert len(calibrator.edges) == len(calibrator.centers) + 1


def test_numeric_bins_respect_min_samples_per_bin():
    scores, labels = _numeric_fixture(40, seed=13)
    calibrator = cal.fit_numeric_bins(
        scores, labels, num_bins=20, min_samples_per_bin=10, tolerance=0.1
    )
    assert len(calibrator.centers) <= 4
    assert min(calibrator.counts) >= 10


def test_numeric_tolerance_is_relative_by_default_and_absolute_on_request():
    scores, labels = _numeric_fixture()
    span = labels.max() - labels.min()
    relative = cal.fit_numeric_bins(scores, labels, tolerance=0.1)
    absolute = cal.fit_numeric_bins(scores, labels, tolerance=0.1, tolerance_absolute=True)
    assert relative.tolerance == pytest.approx(0.1 * span)
    assert absolute.tolerance == pytest.approx(0.1)
    # A wider band can only ever raise coverage.
    assert np.mean(relative.coverages) >= np.mean(absolute.coverages)


def test_numeric_coverage_reflects_the_noise_level():
    """A tolerance far wider than the noise means near-certain coverage."""
    scores, labels = _numeric_fixture()
    calibrator = cal.fit_numeric_bins(scores, labels, num_bins=10, tolerance=5.0, tolerance_absolute=True)
    assert min(calibrator.coverages) > 0.95


def test_numeric_calibrator_roundtrips_through_json_shape():
    scores, labels = _numeric_fixture()
    calibrator = cal.fit_numeric_bins(scores, labels, num_bins=5, tolerance=0.2)
    restored = cal.NumericCalibrator.from_dict(calibrator.to_dict())
    for score in np.linspace(0.0, 1.0, 20):
        assert restored.predict(float(score)) == pytest.approx(calibrator.predict(float(score)))


def test_numeric_bins_handle_constant_labels():
    scores = np.linspace(0.0, 1.0, 30)
    calibrator = cal.fit_numeric_bins(scores, np.full(30, 7.0), num_bins=5, tolerance=0.1)
    value, probability = calibrator.predict(0.4)
    assert value == pytest.approx(7.0)
    assert probability == pytest.approx(1.0)


def test_numeric_bins_reject_bad_inputs():
    with pytest.raises(ValueError):
        cal.fit_numeric_bins(np.array([0.1, 0.2]), np.array([1.0]))
    with pytest.raises(ValueError):
        cal.fit_numeric_bins(np.array([]), np.array([]))
    with pytest.raises(ValueError):
        cal.fit_numeric_bins(np.array([0.1, 0.2]), np.array([1.0, 2.0]), tolerance=-1.0)


def test_numeric_bin_index_is_clamped_to_the_fitted_range():
    scores, labels = _numeric_fixture(60, seed=14)
    calibrator = cal.fit_numeric_bins(scores, labels, num_bins=5, tolerance=0.1)
    assert calibrator.bin_index(-100.0) == 0
    assert calibrator.bin_index(100.0) == len(calibrator.centers) - 1


# ----------------------------------------------------------------------------
# Metrics
# ----------------------------------------------------------------------------
def test_metrics_on_a_perfect_prediction():
    probs = np.array([[0.0, 1.0], [1.0, 0.0]])
    labels = np.array([1, 0])
    assert cal.accuracy(probs, labels) == 1.0
    assert cal.brier_score(probs, labels) == pytest.approx(0.0)
    assert cal.negative_log_likelihood(probs, labels) == pytest.approx(0.0)
    assert cal.expected_calibration_error(probs, labels) == pytest.approx(0.0)


def test_brier_score_matches_hand_computation():
    probs = np.array([[0.7, 0.3]])
    labels = np.array([0])
    assert cal.brier_score(probs, labels) == pytest.approx(0.09 + 0.09)


def test_expected_calibration_error_flags_overconfidence():
    """Claim 0.9 confidence, be right half the time: ECE ≈ 0.4."""
    probs = np.array([[0.1, 0.9]] * 100)
    labels = np.array([1] * 50 + [0] * 50)
    assert cal.expected_calibration_error(probs, labels) == pytest.approx(0.4, abs=1e-9)


def test_negative_log_likelihood_is_finite_for_zero_probability():
    value = cal.negative_log_likelihood(np.array([[1.0, 0.0]]), np.array([1]))
    assert math.isfinite(value) and value > 20


def test_mean_absolute_error_and_coverage():
    values = np.array([1.0, 2.0, 3.0])
    labels = np.array([1.5, 2.0, 5.0])
    assert cal.mean_absolute_error(values, labels) == pytest.approx((0.5 + 0.0 + 2.0) / 3)
    assert cal.coverage(values, labels, tolerance=0.5) == pytest.approx(2 / 3)
