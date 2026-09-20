#!/usr/bin/env python3
"""Fit a linear probe head on a frozen encoder, and save it as a checkpoint.

    python scripts/train_probe_head.py --train-body router-data/posthoc_train.json \
        --out ./probe-head --calibration-out router-data/posthoc_train_calib.json

This service calibrates a frozen model; it does not train one. That is fine
when the base model already emits task-relevant logits and useless when it does
not — a stock encoder's classification head is randomly initialised, and no
amount of post-hoc calibration can extract information the head never had.

The cheapest way to fix that is not a fine-tune. Freeze the encoder, take its
[CLS] features, and fit multinomial logistic regression — one matrix — on top.
Minutes on a CPU. The result is written back into the checkpoint's own
classifier layer, so `JEV_MODEL_NAME` can point straight at it and the engine
neither knows nor cares how the head was obtained.

**The split matters more than the probe.** Post-hoc calibration must be fitted
on outputs the base model did not train on. Fit both on the same rows and the
calibrator learns the head's memorised accuracy and reports that inflated
number for the rest of its life. Measured on xRouteBench, sharing the rows made
the engine over-confident by 19 points; splitting them left it honest to within
4. So this script splits by default and writes out the calibration half for you
to POST to /posthoc_train — that file is the point of the script as much as the
checkpoint is.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from pathlib import Path


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--train-body", required=True, help="A /posthoc_train body to learn from.")
    parser.add_argument("--out", default="./probe-head", help="Where to save the checkpoint.")
    parser.add_argument(
        "--calibration-out",
        default=None,
        help="Where to write the held-out rows for /posthoc_train. Strongly recommended.",
    )
    parser.add_argument("--base-model", default="distilbert-base-uncased")
    parser.add_argument(
        "--probe-fraction", type=float, default=0.70,
        help="Share of rows used to fit the probe; the rest are for calibration.",
    )
    parser.add_argument("--max-length", type=int, default=256)
    parser.add_argument("--batch-size", type=int, default=32)
    parser.add_argument("--C", type=float, default=1.0, help="Inverse regularisation strength.")
    parser.add_argument("--max-iter", type=int, default=2000)
    parser.add_argument("--seed", type=int, default=7)
    parser.add_argument("--feature-cache", default=None, help="Reuse encoder features across runs.")
    args = parser.parse_args()

    import numpy as np
    import torch
    from sklearn.linear_model import LogisticRegression
    from transformers import AutoModelForSequenceClassification, AutoTokenizer

    body = json.loads(Path(args.train_body).read_text(encoding="utf-8"))
    if body["decision_type"] != "enum":
        print("a probe head only makes sense for an enum scenario", file=sys.stderr)
        return 1
    samples = body["training_data"]
    class_names = body.get("class_names") or sorted({s["label"] for s in samples})
    labels = np.array([class_names.index(s["label"]) for s in samples])
    texts = [next(iter(s["input"].values())) for s in samples]
    print(f"{len(texts)} rows, {len(class_names)} classes")

    tokenizer = AutoTokenizer.from_pretrained(args.base_model)
    model = AutoModelForSequenceClassification.from_pretrained(
        args.base_model, num_labels=len(class_names), ignore_mismatched_sizes=True
    )
    model.eval()

    # DistilBERT computes classifier(dropout(ReLU(pre_classifier(hidden[:, 0])))).
    # Pinning pre_classifier to the identity puts the probe directly on the
    # encoder's own features, so the saved head *is* the fitted probe.
    has_pre = hasattr(model, "pre_classifier")
    if has_pre:
        with torch.no_grad():
            model.pre_classifier.weight.copy_(torch.eye(model.pre_classifier.weight.shape[0]))
            model.pre_classifier.bias.zero_()

    cache = Path(args.feature_cache) if args.feature_cache else None
    if cache and cache.is_file():
        features = np.load(cache)
        print(f"features {features.shape} from cache")
    else:
        started = time.perf_counter()
        encoder = getattr(model, "distilbert", None) or getattr(model, "bert", None)
        if encoder is None:
            print(f"cannot find the encoder inside {type(model).__name__}", file=sys.stderr)
            return 1
        chunks = []
        for start in range(0, len(texts), args.batch_size):
            encoded = tokenizer(
                texts[start : start + args.batch_size],
                padding=True, truncation=True, max_length=args.max_length, return_tensors="pt",
            )
            with torch.inference_mode():
                hidden = encoder(**encoded).last_hidden_state[:, 0]
                chunks.append((torch.relu(hidden) if has_pre else hidden).numpy())
        features = np.concatenate(chunks).astype(np.float64)
        print(f"features {features.shape} in {time.perf_counter() - started:.0f}s")
        if cache:
            np.save(cache, features)

    rng = np.random.default_rng(args.seed)
    order = rng.permutation(len(features))
    cut = int(len(features) * args.probe_fraction)
    fit_idx, calib_idx = order[:cut], order[cut:]

    probe = LogisticRegression(max_iter=args.max_iter, C=args.C)
    probe.fit(features[fit_idx], labels[fit_idx])
    in_sample = probe.score(features[fit_idx], labels[fit_idx])
    held_out = probe.score(features[calib_idx], labels[calib_idx]) if len(calib_idx) else float("nan")
    majority = (
        float((labels[calib_idx] == np.bincount(labels[fit_idx]).argmax()).mean())
        if len(calib_idx)
        else float("nan")
    )
    print(
        f"probe: in-sample {in_sample:.3f} | held out {held_out:.3f} | majority {majority:.3f}\n"
        f"  (the held-out number is the one that predicts routing quality; the gap to "
        f"in-sample is how much the calibration split is protecting you from)"
    )

    with torch.no_grad():
        model.classifier.weight.copy_(
            torch.tensor(probe.coef_, dtype=model.classifier.weight.dtype)
        )
        model.classifier.bias.copy_(
            torch.tensor(probe.intercept_, dtype=model.classifier.bias.dtype)
        )
    model.config.id2label = dict(enumerate(class_names))
    model.config.label2id = {name: index for index, name in enumerate(class_names)}
    model.save_pretrained(args.out)
    tokenizer.save_pretrained(args.out)

    # The saved head must reproduce the probe, or the calibration that follows
    # is being fitted against something other than what was measured above.
    sample = tokenizer(
        texts[:8], padding=True, truncation=True, max_length=args.max_length, return_tensors="pt"
    )
    with torch.inference_mode():
        produced = model(**sample).logits.numpy()
    drift = float(np.abs(produced - probe.decision_function(features[:8])).max())
    print(f"saved head reproduces the probe to {drift:.2e}")
    if drift > 1e-3:
        print("saved head does NOT match the probe; refusing to claim success", file=sys.stderr)
        return 1

    if args.calibration_out and len(calib_idx):
        calibration_body = dict(body)
        calibration_body["training_data"] = [samples[i] for i in calib_idx]
        calibration_body["notes"] = "held out from the probe fit; safe to calibrate on"
        Path(args.calibration_out).write_text(json.dumps(calibration_body), encoding="utf-8")
        print(f"wrote {len(calib_idx)} calibration rows -> {args.calibration_out}")

    print(f"\nJEV_MODEL_NAME={args.out} JEV_MODEL_NUM_LABELS={len(class_names)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
