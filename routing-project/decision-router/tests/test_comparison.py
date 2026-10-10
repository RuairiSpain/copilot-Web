import asyncio
import json
import subprocess
import sys
from pathlib import Path

from decision_router.comparison import (
    PREFERENCE_TO_MODE,
    option_order_test,
    shuffled_orders,
    stratified_sample,
    summarize,
)

DATASET = Path(__file__).resolve().parents[2] / "dataset-v31"
POOL = ["gpt-5-nano", "gpt-5-mini", "deepseek-v4-flash", "gpt-5.5", "o4-mini", "gpt-5.6-terra"]
MODES = {"cost": POOL[:3], "balanced": POOL, "quality": POOL[3:]}


def test_dataset_labels_respect_the_routing_mode_filters():
    for name in ("routing_v31_train.jsonl", "routing_v31_val.jsonl", "routing_v31_test.jsonl",
                 "routing_v31_test_ood_templates.jsonl"):
        with (DATASET / name).open() as handle:
            for line in handle:
                row = json.loads(line)
                mode = PREFERENCE_TO_MODE[row["quality_preference"]]
                assert row["selected_model"] in MODES[mode], (name, row["id"])


def test_stratified_sample_is_balanced_and_reproducible():
    rows = [{"id": i, "task_type": t, "quality_preference": p}
            for i, (t, p) in enumerate((t, p) for t in "abcd" for p in "wxyz" for _ in range(20))]
    first = stratified_sample(rows, 32, seed=1)
    assert [r["id"] for r in first] == [r["id"] for r in stratified_sample(rows, 32, seed=1)]
    cells = {(r["task_type"], r["quality_preference"]) for r in first}
    assert len(first) == 32 and len(cells) == 16


def record(mode, ranking, candidates, router_model, policy=None, top=0.6):
    probabilities = {name: (1 - top) / (len(candidates) - 1) for name in candidates}
    probabilities[ranking[0]] = top
    return {"routing_mode": mode, "task_type": "code", "policy_label": policy,
            "decision1": {"ranking": ranking, "candidates": candidates, "latency_ms": 10.0, "skipped": False,
                          "probabilities": probabilities, "top_probability": top, "execution_order": ranking,
                          "low_confidence": False},
            "ours": {"served_model": ranking[0], "latency_ms": 100.0, "fallback_used": False, "cost": {"amount": None}},
            "router": {"provider_model": router_model, "served_model": router_model if router_model in POOL else None,
                       "latency_ms": 120.0, "cost": {"amount": None}}}


def test_summary_measures():
    records = [
        record("cost", ["gpt-5-nano", "gpt-5-mini"], POOL[:3], "gpt-5-nano", policy="gpt-5-nano"),
        record("cost", ["gpt-5-mini", "gpt-5-nano"], POOL[:3], "gpt-5-nano"),
        record("quality", ["gpt-5.5", "o4-mini"], POOL[3:], "gpt-5-mini"),
        record("quality", ["gpt-5.5", "o4-mini"], POOL[3:], "claude-x-2026"),
    ]
    summary = summarize(records)
    assert summary["compared"] == 3
    assert summary["agreement_top1"] == round(1 / 3, 4)
    assert summary["router_in_decision1_top2"] == round(2 / 3, 4)
    assert summary["router_outside_stage1"] == round(1 / 3, 4)
    assert summary["router_unmapped_models"] == {"claude-x-2026": 1}
    assert summary["by_mode"]["cost"]["agreement_top1"] == 0.5


def test_low_confidence_sweep_shows_what_each_threshold_would_do():
    records = [
        record("cost", ["gpt-5-nano", "gpt-5-mini", "deepseek-v4-flash"], POOL[:3], "gpt-5-mini", top=0.4),
        record("cost", ["gpt-5-nano", "gpt-5-mini", "deepseek-v4-flash"], POOL[:3], "gpt-5-nano", top=0.9),
    ]
    low = summarize(records)["low_confidence"]
    assert low["agreement_with_router_without_rule"] == 0.5
    sweep = {s["threshold"]: s for s in low["threshold_sweep"]}
    assert sweep[0.3]["fired_rate"] == 0.0 and sweep[0.3]["agreement_with_router"] == 0.5
    assert sweep[0.5]["fired_rate"] == 0.5 and sweep[0.5]["agreement_with_router"] == 1.0


def test_shuffled_orders_are_reproducible_and_never_the_original():
    names = ["a", "b", "c"]
    orders = shuffled_orders(names, 20, "seed:row-1")
    assert orders == shuffled_orders(names, 20, "seed:row-1")
    assert all(sorted(o) == names and o != names for o in orders)


def _option_order_run(make_pipeline, fake, bias):
    fake.position_bias = bias
    pipe = make_pipeline(low_confidence_threshold=0.0)

    async def go():
        body = {"messages": [{"role": "user", "content": "Plan a database migration."}]}
        results = []
        for i in range(40):
            body["messages"][0]["content"] = f"Plan a database migration, variant {i}."
            prepared = pipe.prepare(dict(body, routing_mode="balanced"))
            routed = await pipe.route(prepared, f"r{i}")
            results += await option_order_test(pipe, routed, prepared.body, 2, f"k{i}")
        return results
    return asyncio.run(go())


def test_option_order_test_finds_no_flips_for_an_order_insensitive_model(make_pipeline, fake):
    results = _option_order_run(make_pipeline, fake, bias=0.0)
    assert len(results) == 80 and not any(r["ranking_changed"] for r in results)


def test_option_order_test_detects_position_bias(make_pipeline, fake):
    results = _option_order_run(make_pipeline, fake, bias=3.0)
    assert sum(r["top1_changed"] for r in results) > 10
    sent = [r["body"]["questions"]["route"]["criteria"] for r in fake.requests if r["path"].endswith("/systemone")]
    assert len({tuple(c) for c in sent}) > 1, "criteria must actually be reordered"


def test_dry_run_end_to_end(tmp_path, root):
    out = tmp_path / "run"
    result = subprocess.run(
        [sys.executable, str(root / "scripts/compare_with_model_router.py"), "--dry-run", "--sample", "32",
         "--shuffle-options", "2", "--output-dir", str(out)],
        capture_output=True, text=True, cwd=root, timeout=120,
    )
    assert result.returncode == 0, result.stderr
    report = json.loads((out / "report.json").read_text())["summary"]
    assert report["n"] == 32 and report["decision1_answered"] == 32 and report["router_answered"] == 32
    assert (out / "decision_log.jsonl").read_text().count("\n") == 32
    rows = [json.loads(line) for line in (out / "results.jsonl").read_text().splitlines()]
    assert all(r["ours"]["served_model"] in r["decision1"]["candidates"] for r in rows)
    assert report["option_order"]["calls"] == 64 and report["option_order"]["ranking_change_rate"] == 0.0
    assert "## Option-order test" in (out / "report.md").read_text()
