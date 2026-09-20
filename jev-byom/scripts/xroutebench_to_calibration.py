#!/usr/bin/env python3
"""Turn xRouteBench routing data into /posthoc_train and /decision payloads.

    python scripts/xroutebench_to_calibration.py --out-dir ./router-data

xRouteBench (https://huggingface.co/datasets/ulab-ai/xRouteBench) stores one row
per (query, candidate model) with a `performance` score, token counts and a
response time. A router needs the opposite shape: one row per query, labelled
with the model that should have handled it. This script does that pivot.

The interesting decision is what "best" means, because with 18 candidates and a
0/1 score most queries are a tie — several models got it right. Picking the
strongest model every time would train a router to ignore the question. So the
tie is broken on cost: among the models that scored best, take the cheapest by
`--tie-break`, and only then fall back to a fixed order so the labels are
deterministic. That turns "who is smartest" into "who is sufficient", which is
the question a router is actually for.

Emits three files:

    posthoc_train.json   the POST /posthoc_train body
    decisions.jsonl      held-out prompts, each with its gold label
    summary.json         class balance, tie statistics, the oracle ceiling

Nothing here is specific to xRouteBench beyond the column names at the top; the
same pivot works for any per-(query, model) table.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

# Column names in xRouteBench's llmrouter_* configs.
QUERY = "query"
MODEL = "model_name"
SCORE = "performance"
LATENCY = "response_time"
TOKENS = "token_num"
TASK = "task_name"

TIE_BREAKS = {
    "latency": (LATENCY, "fastest correct model"),
    "tokens": (TOKENS, "fewest tokens among correct models"),
    "none": (None, "fixed model order only"),
}


def load_split(repo: str, config: str, split: str):
    """Read one parquet split straight from the Hub."""
    import pandas as pd
    from huggingface_hub import hf_hub_download

    path = hf_hub_download(repo, f"{config}/{split}.parquet", repo_type="dataset")
    return pd.read_parquet(path)


def pivot_to_labels(df, *, tie_break: str, model_order: list[str]) -> Any:
    """One row per query: the label, the tie size, and the per-model scores."""
    import pandas as pd

    rank = {name: index for index, name in enumerate(model_order)}
    tie_column, _ = TIE_BREAKS[tie_break]

    rows = []
    for query, group in df.groupby(QUERY, sort=False):
        best = group[group[SCORE] == group[SCORE].max()]
        order = [SCORE]
        ascending = [False]
        if tie_column is not None and tie_column in best.columns:
            order.append(tie_column)
            ascending.append(True)
        best = best.assign(_rank=best[MODEL].map(rank)).sort_values(
            order + ["_rank"], ascending=ascending + [True]
        )
        winner = best.iloc[0]
        rows.append(
            {
                "query": query,
                "label": winner[MODEL],
                "task": winner[TASK],
                "best_score": float(winner[SCORE]),
                "tie_size": int(len(best)),
                "solvable": bool(group[SCORE].max() > 0),
            }
        )
    return pd.DataFrame(rows)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", default="ulab-ai/xRouteBench")
    parser.add_argument("--config", default="llmrouter_generic")
    parser.add_argument("--scenario", default="llm-router")
    parser.add_argument("--out-dir", default="./router-data")
    parser.add_argument(
        "--tie-break", choices=sorted(TIE_BREAKS), default="latency",
        help="How to choose among models that scored equally well.",
    )
    parser.add_argument(
        "--max-models", type=int, default=0,
        help="Keep only the N most-often-best models, mapping the rest away. "
             "0 keeps all of them. Fewer classes means each one gets more "
             "examples, which is what the isotonic fits need.",
    )
    parser.add_argument(
        "--drop-unsolvable", action="store_true",
        help="Drop queries no candidate model got right: there is no correct "
             "route for them, so their label is noise.",
    )
    parser.add_argument("--max-train", type=int, default=0, help="Cap training rows.")
    parser.add_argument("--num-decisions", type=int, default=100)
    parser.add_argument("--max-chars", type=int, default=4000, help="Truncate long prompts.")
    parser.add_argument("--seed", type=int, default=42)
    args = parser.parse_args()

    import numpy as np

    train_raw = load_split(args.repo, args.config, "train")
    test_raw = load_split(args.repo, args.config, "test")
    model_order = sorted(train_raw[MODEL].unique())

    train = pivot_to_labels(train_raw, tie_break=args.tie_break, model_order=model_order)
    test = pivot_to_labels(test_raw, tie_break=args.tie_break, model_order=model_order)

    if args.drop_unsolvable:
        train, test = train[train.solvable], test[test.solvable]

    if args.max_models:
        keep = list(train.label.value_counts().head(args.max_models).index)
        train = train[train.label.isin(keep)]
        test = test[test.label.isin(keep)]

    class_names = sorted(set(train.label) | set(test.label))
    if len(class_names) < 2:
        print("need at least two classes after filtering", file=sys.stderr)
        return 1

    rng = np.random.default_rng(args.seed)
    if args.max_train and len(train) > args.max_train:
        train = train.iloc[rng.permutation(len(train))[: args.max_train]]

    take = min(args.num_decisions, len(test))
    held_out = test.iloc[rng.permutation(len(test))[:take]]

    out_dir = Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    body = {
        "decision_type": "enum",
        "scenario": args.scenario,
        "class_names": class_names,
        "notes": (
            f"{args.repo}/{args.config}, tie-break={args.tie_break}, "
            f"{len(train)} queries over {len(class_names)} candidate models"
        ),
        "training_data": [
            {"input": {"text": row.query[: args.max_chars]}, "label": row.label}
            for row in train.itertuples()
        ],
    }
    (out_dir / "posthoc_train.json").write_text(json.dumps(body), encoding="utf-8")

    with (out_dir / "decisions.jsonl").open("w", encoding="utf-8") as handle:
        for row in held_out.itertuples():
            handle.write(
                json.dumps(
                    {
                        "text": row.query[: args.max_chars],
                        "gold": row.label,
                        "task": row.task,
                        "tie_size": row.tie_size,
                        "solvable": row.solvable,
                    }
                )
                + "\n"
            )

    counts = train.label.value_counts()
    summary = {
        "repo": args.repo,
        "config": args.config,
        "tie_break": args.tie_break,
        "num_classes": len(class_names),
        "class_names": class_names,
        "num_train_queries": int(len(train)),
        "num_decisions": int(len(held_out)),
        # The accuracy a router gets by always naming the most common label.
        "majority_class": counts.index[0],
        "majority_share_train": float(counts.iloc[0] / len(train)),
        "majority_share_holdout": float(
            (held_out.label == counts.index[0]).mean() if len(held_out) else 0.0
        ),
        # The ceiling: how often *any* candidate model answers correctly.
        "oracle_solvable_share": float(train.solvable.mean()),
        "mean_tie_size": float(train.tie_size.mean()),
        "label_counts_train": {str(k): int(v) for k, v in counts.items()},
    }
    (out_dir / "summary.json").write_text(json.dumps(summary, indent=2), encoding="utf-8")

    print(json.dumps(summary, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
