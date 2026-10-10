"""Run dataset prompts through this pipeline and through Foundry Model Router, and compare.

Both arms get the same chat-completions body and call the same Foundry deployments, so
the difference measured is the routing decision. The dataset's `selected_model` column is
a hand-written policy label: it is reported as a reference, not as the right answer.
"""
from __future__ import annotations

import asyncio
import json
import math
import random
import time
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any

from .catalog import Catalog, ModelEntry
from .decision1 import Decision, DecisionError, build_state, execution_order
from .foundry import ChatClient
from .pipeline import ProviderError, Routed, RouterPipeline, RoutingError
from .pricing import PriceTable

PREFERENCE_TO_MODE = {"cheap": "cost", "medium": "balanced", "high": "balanced", "extra_high": "quality"}
THRESHOLD_SWEEP = (0.3, 0.4, 0.5, 0.6, 0.7, 0.8)


def shuffled_orders(names: list[str], repeats: int, key: str) -> list[list[str]]:
    """`repeats` reorderings of `names`, reproducible from `key`, each different from the original order."""
    rng = random.Random(key)
    orders = []
    for _ in range(repeats):
        order = list(names)
        rng.shuffle(order)
        if order == names and len(names) > 1:
            order = order[1:] + order[:1]  # never resend the original order: it would measure nothing
        orders.append(order)
    return orders


async def option_order_test(pipeline: RouterPipeline, routed: Routed, body: dict[str, Any], repeats: int,
                            key: str) -> list[dict[str, Any]]:
    """Ask Decision-1 the same question again with the options in a different order.

    The state, instructions and option descriptions are identical to the original call; only the
    order of the keys in `criteria` changes. A position-insensitive model returns the same ranking.
    """
    base: Decision = routed.decision
    if base.skipped:
        return []
    by_name: dict[str, ModelEntry] = {m.name: m for m in routed.candidates}
    criteria = pipeline.catalog.criteria(routed.candidates)
    state = build_state(body, pipeline.settings.state_max_chars)
    instructions = pipeline.catalog.modes[routed.mode].instructions
    results = []
    for order in shuffled_orders(base.candidates, repeats, key):
        try:
            shuffled = await pipeline.decision.decide(state, instructions, {n: criteria[n] for n in order},
                                                      [by_name[n] for n in order])
        except DecisionError as exc:
            results.append({"order": order, "error": exc.code})
            continue
        shuffled_first, _ = execution_order(shuffled.ranking, shuffled.probabilities,
                                            pipeline.settings.low_confidence_threshold)
        results.append({
            "order": order,
            "ranking": shuffled.ranking,
            "probabilities": shuffled.probabilities,
            "top1_changed": shuffled.ranking[0] != base.ranking[0],
            "served_first_changed": shuffled_first[0] != routed.order[0],
            "ranking_changed": shuffled.ranking != base.ranking,
            "max_probability_shift": round(max(abs(shuffled.probabilities[n] - base.probabilities[n])
                                               for n in base.candidates), 6),
        })
    return results


def load_rows(paths: list[Path]) -> list[dict[str, Any]]:
    rows = []
    for path in paths:
        with path.open(encoding="utf-8") as handle:
            rows.extend(json.loads(line) for line in handle if line.strip())
    return rows


def stratified_sample(rows: list[dict[str, Any]], n: int | None, seed: int) -> list[dict[str, Any]]:
    """Equal share from every (task_type, quality_preference) cell, reproducible from the seed."""
    if not n or n >= len(rows):
        return list(rows)
    rng = random.Random(seed)
    cells: dict[tuple[str, str], list[dict[str, Any]]] = defaultdict(list)
    for row in rows:
        cells[(row.get("task_type", ""), row.get("quality_preference", ""))].append(row)
    for group in cells.values():
        rng.shuffle(group)
    per_cell = math.ceil(n / len(cells))
    picked = [row for key in sorted(cells) for row in cells[key][:per_cell]]
    rng.shuffle(picked)
    return picked[:n]


def build_body(prompt: str, max_output_tokens: int | None, max_tokens_param: str) -> dict[str, Any]:
    body: dict[str, Any] = {"messages": [{"role": "user", "content": prompt}]}
    if max_output_tokens and max_tokens_param != "none":
        body[max_tokens_param] = max_output_tokens
    return body


def _message_text(parsed: Any) -> str | None:
    try:
        return parsed["choices"][0]["message"]["content"]
    except (KeyError, IndexError, TypeError):
        return None


async def run_row(row: dict[str, Any], *, pipeline: RouterPipeline, router: ChatClient,
                  router_deployments: dict[str, str], catalog: Catalog, prices: PriceTable,
                  max_output_tokens: int | None, max_tokens_param: str, decide_only: bool,
                  store_outputs: bool, timeout: float, shuffle_repeats: int = 0, seed: int = 0) -> dict[str, Any]:
    mode = PREFERENCE_TO_MODE[row["quality_preference"]]
    body = build_body(row["prompt"], max_output_tokens, max_tokens_param)
    record: dict[str, Any] = {
        "id": row.get("id"), "task_type": row.get("task_type"), "complexity": row.get("complexity"),
        "template_id": row.get("template_id"), "quality_preference": row["quality_preference"],
        "routing_mode": mode, "policy_label": row.get("selected_model"),
    }

    async def ours() -> None:
        out: dict[str, Any] = {}
        try:
            _, forwarded = pipeline.prepare(dict(body, routing_mode=mode))
            routed = await pipeline.route(forwarded, mode, f"cmp-{row.get('id')}")
            d = routed.decision
            record["decision1"] = {"candidates": d.candidates, "ranking": d.ranking, "probabilities": d.probabilities,
                                   "execution_order": routed.order, "low_confidence": routed.low_confidence,
                                   "top_probability": d.probabilities[d.ranking[0]],
                                   "confidence": d.confidence, "latency_ms": d.latency_ms, "skipped": d.skipped,
                                   "usage": d.usage}
            if shuffle_repeats:
                record["option_order"] = await option_order_test(pipeline, routed, forwarded, shuffle_repeats,
                                                                 f"{seed}:{row.get('id')}")
            if decide_only:
                pipeline.record_route_only(routed, forwarded)
            else:
                t0 = time.perf_counter()
                completion = await pipeline.complete(routed, forwarded)
                parsed = json.loads(completion.content)
                out.update(served_model=completion.served.name, provider_model=parsed.get("model"),
                           fallback_used=completion.served.name != routed.order[0],
                           latency_ms=round((time.perf_counter() - t0) * 1000 + d.latency_ms, 3),
                           usage=parsed.get("usage"), cost=prices.cost(completion.served.name, parsed.get("usage")))
                if store_outputs:
                    out["output"] = _message_text(parsed)
        except (RoutingError, ProviderError) as exc:
            out["error"] = getattr(exc, "code", None) or f"http_{getattr(exc, 'status_code', '?')}"
        record["ours"] = out

    async def managed() -> None:
        deployment = router_deployments.get(mode)
        out: dict[str, Any] = {"deployment": deployment}
        if not deployment:
            out["error"] = "no_router_deployment_for_mode"
            record["router"] = out
            return
        t0 = time.perf_counter()
        try:
            response = await router.send(deployment, body, timeout=timeout)
            out["latency_ms"] = round((time.perf_counter() - t0) * 1000, 3)
            if response.status_code >= 400:
                out["error"] = f"http_{response.status_code}"
            else:
                parsed = response.json()
                served = catalog.resolve(parsed.get("model"))
                out.update(provider_model=parsed.get("model"), served_model=served, usage=parsed.get("usage"),
                           cost=prices.cost(served, parsed.get("usage")))
                if store_outputs:
                    out["output"] = _message_text(parsed)
        except Exception as exc:  # one bad row must not stop a long run
            out["error"] = type(exc).__name__
        record["router"] = out

    await asyncio.gather(ours(), managed())
    return record


async def run(rows: list[dict[str, Any]], *, concurrency: int, output: Path, **kwargs: Any) -> list[dict[str, Any]]:
    semaphore = asyncio.Semaphore(concurrency)
    output.parent.mkdir(parents=True, exist_ok=True)
    results: list[dict[str, Any]] = []
    with output.open("w", encoding="utf-8") as handle:
        async def one(row: dict[str, Any]) -> None:
            async with semaphore:
                record = await run_row(row, **kwargs)
            results.append(record)
            handle.write(json.dumps(record, ensure_ascii=False) + "\n")
            handle.flush()
        await asyncio.gather(*(one(r) for r in rows))
    return results


def _percentile(values: list[float], q: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    pos = (len(ordered) - 1) * q
    lo, hi = math.floor(pos), math.ceil(pos)
    return round(ordered[lo] + (ordered[hi] - ordered[lo]) * (pos - lo), 3)


def _rate(hits: int, n: int) -> float | None:
    return round(hits / n, 4) if n else None


def summarize(records: list[dict[str, Any]]) -> dict[str, Any]:
    decided = [r for r in records if (r.get("decision1") or {}).get("ranking")]
    routed = [r for r in records if (r.get("router") or {}).get("provider_model")]
    routed_ids = {id(r) for r in routed}
    both = [r for r in decided if id(r) in routed_ids and r["router"].get("served_model")]
    unmapped = Counter(r["router"]["provider_model"] for r in routed if not r["router"].get("served_model"))

    def top1(r: dict[str, Any]) -> str:
        """The model our pipeline calls first (after the low-confidence rule)."""
        return (r["decision1"].get("execution_order") or r["decision1"]["ranking"])[0]

    by_mode: dict[str, Any] = {}
    for mode in sorted({r["routing_mode"] for r in records}):
        rows = [r for r in both if r["routing_mode"] == mode]
        by_mode[mode] = {
            "n": len(rows),
            "agreement_top1": _rate(sum(top1(r) == r["router"]["served_model"] for r in rows), len(rows)),
            "decision1_top1": dict(Counter(top1(r) for r in decided if r["routing_mode"] == mode).most_common()),
            "router_served": dict(Counter(r["router"]["served_model"] for r in routed
                                          if r["routing_mode"] == mode and r["router"].get("served_model")).most_common()),
        }

    by_task = {}
    for task in sorted({r.get("task_type") for r in both if r.get("task_type")}):
        rows = [r for r in both if r.get("task_type") == task]
        by_task[task] = {"n": len(rows),
                         "agreement_top1": _rate(sum(top1(r) == r["router"]["served_model"] for r in rows), len(rows))}

    def cost_total(arm: str) -> dict[str, Any]:
        amounts = [(r.get(arm) or {}).get("cost", {}) or {} for r in records]
        priced = [a["amount"] for a in amounts if a.get("amount") is not None]
        served = [r for r in records if (r.get(arm) or {}).get("served_model")]
        return {"priced_rows": len(priced), "unpriced_rows": len(served) - len(priced),
                "total": round(sum(priced), 6) if priced else None}

    ours_rows = [r for r in records if (r.get("ours") or {}).get("served_model")]
    return {
        "n": len(records),
        "decision1_answered": len(decided),
        "router_answered": len(routed),
        "compared": len(both),
        "errors": {
            "ours": dict(Counter(r["ours"]["error"] for r in records if (r.get("ours") or {}).get("error"))),
            "router": dict(Counter(r["router"]["error"] for r in records if (r.get("router") or {}).get("error"))),
        },
        "agreement_top1": _rate(sum(top1(r) == r["router"]["served_model"] for r in both), len(both)),
        "router_in_decision1_top2": _rate(sum(r["router"]["served_model"] in r["decision1"]["ranking"][:2] for r in both),
                                          len(both)),
        "router_outside_stage1": _rate(sum(r["router"]["served_model"] not in r["decision1"]["candidates"] for r in both),
                                       len(both)),
        "router_unmapped_models": dict(unmapped.most_common(10)),
        "policy_label_agreement": {
            "decision1_top1": _rate(sum(top1(r) == r.get("policy_label") for r in decided), len(decided)),
            "router": _rate(sum(r["router"].get("served_model") == r.get("policy_label") for r in routed), len(routed)),
        },
        "ours_fallback_rate": _rate(sum(bool(r["ours"].get("fallback_used")) for r in ours_rows), len(ours_rows)),
        "latency_ms": {
            "decision1_p50": _percentile([r["decision1"]["latency_ms"] for r in decided if not r["decision1"]["skipped"]], .5),
            "decision1_p95": _percentile([r["decision1"]["latency_ms"] for r in decided if not r["decision1"]["skipped"]], .95),
            "ours_total_p50": _percentile([r["ours"]["latency_ms"] for r in ours_rows], .5),
            "ours_total_p95": _percentile([r["ours"]["latency_ms"] for r in ours_rows], .95),
            "router_total_p50": _percentile([r["router"]["latency_ms"] for r in routed], .5),
            "router_total_p95": _percentile([r["router"]["latency_ms"] for r in routed], .95),
        },
        "cost": {"ours": cost_total("ours"), "router": cost_total("router")},
        "low_confidence": _low_confidence_summary(decided, both),
        "option_order": _option_order_summary(records),
        "by_mode": by_mode,
        "by_task": by_task,
    }


def _low_confidence_summary(decided: list[dict[str, Any]], both: list[dict[str, Any]]) -> dict[str, Any]:
    """How often the low-confidence rule fired, and what other thresholds would have done."""
    scored = [r for r in decided if not r["decision1"]["skipped"]]
    compared = [r for r in both if not r["decision1"]["skipped"]]
    sweep = []
    for threshold in THRESHOLD_SWEEP:
        def first(r: dict[str, Any], t: float = threshold) -> str:
            return execution_order(r["decision1"]["ranking"], r["decision1"]["probabilities"], t)[0][0]
        sweep.append({
            "threshold": threshold,
            "fired_rate": _rate(sum(r["decision1"]["top_probability"] < threshold for r in scored), len(scored)),
            "agreement_with_router": _rate(sum(first(r) == r["router"]["served_model"] for r in compared),
                                           len(compared)),
        })
    no_rule = _rate(sum(r["decision1"]["ranking"][0] == r["router"]["served_model"] for r in compared), len(compared))
    return {
        "fired_rate": _rate(sum(bool(r["decision1"].get("low_confidence")) for r in scored), len(scored)),
        "top_probability_p50": _percentile([r["decision1"]["top_probability"] for r in scored], .5),
        "agreement_with_router_without_rule": no_rule,
        "threshold_sweep": sweep,
    }


def _option_order_summary(records: list[dict[str, Any]]) -> dict[str, Any] | None:
    tested = [r for r in records if r.get("option_order")]
    if not tested:
        return None
    calls = [c for r in tested for c in r["option_order"] if "error" not in c]

    def rates(items: list[dict[str, Any]]) -> dict[str, Any]:
        return {
            "calls": len(items),
            "top1_flip_rate": _rate(sum(c["top1_changed"] for c in items), len(items)),
            "served_first_flip_rate": _rate(sum(c["served_first_changed"] for c in items), len(items)),
            "ranking_change_rate": _rate(sum(c["ranking_changed"] for c in items), len(items)),
            "mean_max_probability_shift": round(sum(c["max_probability_shift"] for c in items) / len(items), 6)
            if items else None,
        }

    by_mode = {mode: rates([c for r in tested if r["routing_mode"] == mode for c in r["option_order"]
                            if "error" not in c])
               for mode in sorted({r["routing_mode"] for r in tested})}
    errors = Counter(c["error"] for r in tested for c in r["option_order"] if "error" in c)
    return {"prompts": len(tested), **rates(calls), "errors": dict(errors), "by_mode": by_mode}


def _counts(counts: dict[str, int]) -> str:
    return ", ".join(f"{name} {n}" for name, n in counts.items()) or "-"


def render_markdown(summary: dict[str, Any], meta: dict[str, Any]) -> str:
    lines = [
        "# Decision-1 pipeline vs Foundry Model Router",
        "",
        f"Dataset: {', '.join(meta.get('data', []))} | rows: {summary['n']} | seed: {meta.get('seed')} | "
        f"dry run: {meta.get('dry_run')}",
        "",
        "| Measure | Value |",
        "|---|---|",
        f"| Rows compared (both arms answered, router model in pool) | {summary['compared']} |",
        f"| Same model chosen (our first call = Model Router) | {summary['agreement_top1']} |",
        f"| Model Router's model in Decision-1 top-2 | {summary['router_in_decision1_top2']} |",
        f"| Model Router chose outside the mode's stage-1 set | {summary['router_outside_stage1']} |",
        f"| Our fallback rate | {summary['ours_fallback_rate']} |",
        f"| Policy-label agreement, Decision-1 / Model Router (reference only) | "
        f"{summary['policy_label_agreement']['decision1_top1']} / {summary['policy_label_agreement']['router']} |",
        f"| Decision-1 latency p50 / p95 ms | {summary['latency_ms']['decision1_p50']} / {summary['latency_ms']['decision1_p95']} |",
        f"| End-to-end p50 / p95 ms, ours | {summary['latency_ms']['ours_total_p50']} / {summary['latency_ms']['ours_total_p95']} |",
        f"| End-to-end p50 / p95 ms, Model Router | {summary['latency_ms']['router_total_p50']} / {summary['latency_ms']['router_total_p95']} |",
        f"| Cost total, ours / Model Router | {summary['cost']['ours']['total']} / {summary['cost']['router']['total']} |",
        "",
        "## By routing mode",
        "",
        "| Mode | n | Agreement | Our first call | Model Router |",
        "|---|---|---|---|---|",
    ]
    for mode, item in summary["by_mode"].items():
        lines.append(f"| {mode} | {item['n']} | {item['agreement_top1']} | {_counts(item['decision1_top1'])} | "
                     f"{_counts(item['router_served'])} |")
    low = summary["low_confidence"]
    lines += [
        "",
        "## Low-confidence rule",
        "",
        f"Fired on {low['fired_rate']} of decisions (top probability below the configured threshold). "
        f"Median top probability: {low['top_probability_p50']}. Agreement with Model Router without the rule: "
        f"{low['agreement_with_router_without_rule']}.",
        "",
        "| Threshold | Share of decisions where it fires | Agreement with Model Router |",
        "|---|---|---|",
    ]
    lines += [f"| {s['threshold']} | {s['fired_rate']} | {s['agreement_with_router']} |" for s in low["threshold_sweep"]]
    order = summary.get("option_order")
    if order:
        lines += [
            "",
            "## Option-order test",
            "",
            f"{order['prompts']} prompts, {order['calls']} reshuffled Decision-1 calls.",
            "",
            "| Mode | Calls | Top-1 flips | First-call flips | Ranking changed | Mean max probability shift |",
            "|---|---|---|---|---|---|",
            f"| all | {order['calls']} | {order['top1_flip_rate']} | {order['served_first_flip_rate']} | "
            f"{order['ranking_change_rate']} | {order['mean_max_probability_shift']} |",
        ]
        lines += [f"| {mode} | {m['calls']} | {m['top1_flip_rate']} | {m['served_first_flip_rate']} | "
                  f"{m['ranking_change_rate']} | {m['mean_max_probability_shift']} |" for mode, m in order["by_mode"].items()]
        if order["errors"]:
            lines.append(f"\nReshuffled calls that failed: {order['errors']}.")
    if summary["router_unmapped_models"]:
        lines += ["", "**Model Router returned models that are not in the catalog:** "
                  f"{summary['router_unmapped_models']}. Add them as aliases, or align the pool with the router's "
                  "model subset; until then those rows are not compared."]
    if summary["errors"]["ours"] or summary["errors"]["router"]:
        lines += ["", f"Errors: ours {summary['errors']['ours']}, Model Router {summary['errors']['router']}."]
    lines += ["", "Agreement says how often the two routers choose the same model. It says nothing about which "
              "answer was better: judge the stored outputs for that."]
    return "\n".join(lines) + "\n"
