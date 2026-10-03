"""Tests for the xRouteBench → calibration converter.

The pivot is where the judgement lives: with 18 candidate models and a 0/1
score, most queries are a tie, so which model gets the label is decided by the
tie-break rather than by the score. That is worth pinning.
"""

from __future__ import annotations

import sys
from pathlib import Path

import pytest

pd = pytest.importorskip("pandas")

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))

from xroutebench_to_calibration import pivot_to_labels  # noqa: E402

MODELS = ["alpha", "beta", "gamma"]


def frame(rows: list[tuple[str, str, float, float, int]]) -> pd.DataFrame:
    return pd.DataFrame(
        rows, columns=["query", "model_name", "performance", "response_time", "token_num"]
    ).assign(task_name="demo")


def test_the_only_correct_model_wins_outright():
    out = pivot_to_labels(
        frame([("q", "alpha", 0.0, 0.1, 10), ("q", "beta", 1.0, 9.9, 99), ("q", "gamma", 0.0, 0.1, 10)]),
        tie_break="latency",
        model_order=MODELS,
    )
    assert out.iloc[0].label == "beta"
    assert out.iloc[0].tie_size == 1
    assert bool(out.iloc[0].solvable) is True


def test_a_tie_is_broken_by_the_cheapest_correct_model_not_the_strongest():
    """Three models are right; the router should learn to send this to the fastest."""
    out = pivot_to_labels(
        frame([("q", "alpha", 1.0, 5.0, 50), ("q", "beta", 1.0, 0.5, 80), ("q", "gamma", 1.0, 2.0, 20)]),
        tie_break="latency",
        model_order=MODELS,
    )
    assert out.iloc[0].label == "beta"
    assert out.iloc[0].tie_size == 3


def test_the_tie_break_column_changes_the_label():
    rows = [("q", "alpha", 1.0, 5.0, 50), ("q", "beta", 1.0, 0.5, 80), ("q", "gamma", 1.0, 2.0, 20)]
    by_tokens = pivot_to_labels(frame(rows), tie_break="tokens", model_order=MODELS)
    by_order = pivot_to_labels(frame(rows), tie_break="none", model_order=MODELS)
    assert by_tokens.iloc[0].label == "gamma"  # fewest tokens
    assert by_order.iloc[0].label == "alpha"  # first in the fixed order


def test_an_unsolvable_query_is_flagged_rather_than_silently_labelled():
    """No model got it right, so its label is noise; --drop-unsolvable uses this."""
    out = pivot_to_labels(
        frame([("q", "alpha", 0.0, 1.0, 10), ("q", "beta", 0.0, 0.5, 10), ("q", "gamma", 0.0, 2.0, 10)]),
        tie_break="latency",
        model_order=MODELS,
    )
    assert bool(out.iloc[0].solvable) is False
    assert out.iloc[0].best_score == 0.0


def test_partial_scores_pick_the_highest_before_the_tie_break():
    out = pivot_to_labels(
        frame([("q", "alpha", 0.5, 0.1, 10), ("q", "beta", 0.9, 9.0, 10), ("q", "gamma", 0.2, 0.1, 10)]),
        tie_break="latency",
        model_order=MODELS,
    )
    assert out.iloc[0].label == "beta"


def test_each_query_yields_exactly_one_row():
    rows = []
    for query in ("q1", "q2", "q3"):
        for model in MODELS:
            rows.append((query, model, 1.0, 1.0, 10))
    out = pivot_to_labels(frame(rows), tie_break="latency", model_order=MODELS)
    assert len(out) == 3
    assert set(out["query"]) == {"q1", "q2", "q3"}
