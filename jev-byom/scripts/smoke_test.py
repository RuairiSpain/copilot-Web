#!/usr/bin/env python3
"""Train and query one scenario of each decision type against a live endpoint.

    python scripts/smoke_test.py --base-url https://jev-byom-app.azurecontainerapps.io
    python scripts/smoke_test.py --base-url http://localhost:8000 --api-key "$JEV_API_KEY"

The labels it sends are synthetic, so the *probabilities* it gets back will be
poor by design — it checks that the plumbing works (train, persist, load,
decide, typed output), not that the model is any good.

It is deliberately stdlib-only (``urllib``), so it runs anywhere ``az`` does
without installing anything, and it exits non-zero on the first failure so CI
can gate a deployment on it.
"""

from __future__ import annotations

import argparse
import json
import random
import sys
import urllib.error
import urllib.request
from typing import Any

TIMEOUT_SECONDS = 120


def call(base_url: str, path: str, payload: dict[str, Any] | None, api_key: str | None) -> Any:
    """POST (or GET when payload is None) and return the parsed body."""
    url = f"{base_url.rstrip('/')}{path}"
    data = json.dumps(payload).encode("utf-8") if payload is not None else None
    request = urllib.request.Request(url, data=data, method="POST" if data else "GET")
    request.add_header("content-type", "application/json")
    if api_key:
        request.add_header("x-api-key", api_key)
    try:
        with urllib.request.urlopen(request, timeout=TIMEOUT_SECONDS) as response:
            return json.loads(response.read())
    except urllib.error.HTTPError as error:
        body = error.read().decode("utf-8", "replace")
        raise SystemExit(f"{path} failed with HTTP {error.code}: {body}") from None
    except urllib.error.URLError as error:
        raise SystemExit(f"{path} unreachable: {error.reason}") from None


def training_rows(count: int, label_fn) -> list[dict[str, Any]]:
    return [
        {"input": {"text": f"smoke test case number {index}"}, "label": label_fn(index)}
        for index in range(count)
    ]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--api-key", default=None)
    parser.add_argument("--samples", type=int, default=60)
    parser.add_argument(
        "--prefix",
        default="smoke",
        help="Scenario name prefix, so a smoke run never overwrites a real scenario.",
    )
    args = parser.parse_args()

    rng = random.Random(0)

    health = call(args.base_url, "/health", None, args.api_key)
    print(f"health: {json.dumps(health)}")

    checks = [
        (
            "boolean",
            training_rows(args.samples, lambda index: index % 3 != 0),
            {},
        ),
        (
            "enum",
            training_rows(args.samples, lambda index: ["low", "medium", "high"][index % 3]),
            {"class_names": ["low", "medium", "high"]},
        ),
        (
            "numeric",
            training_rows(args.samples, lambda index: 100.0 + index + rng.gauss(0, 2)),
            {"numeric_tolerance": 0.1},
        ),
    ]

    failures = 0
    for decision_type, rows, extra in checks:
        scenario = f"{args.prefix}-{decision_type}"
        trained = call(
            args.base_url,
            "/posthoc_train",
            {
                "decision_type": decision_type,
                "scenario": scenario,
                "training_data": rows,
                **extra,
            },
            args.api_key,
        )
        decision = call(
            args.base_url,
            "/decision",
            {
                "decision_type": decision_type,
                "scenario": scenario,
                "data": {"text": "smoke test case number 7"},
            },
            args.api_key,
        )

        problems = []
        if not (0.0 <= decision["probability"] <= 1.0):
            problems.append(f"probability out of range: {decision['probability']}")
        if decision["calibration_version"] != trained["calibration_version"]:
            problems.append("decision did not use the version just trained")
        if decision_type == "boolean" and not isinstance(decision["value"], bool):
            problems.append(f"expected a bool, got {type(decision['value']).__name__}")
        if decision_type == "enum" and not isinstance(decision["value"], int):
            problems.append(f"expected an int, got {type(decision['value']).__name__}")
        if decision_type == "numeric" and not isinstance(decision["value"], (int, float)):
            problems.append(f"expected a number, got {type(decision['value']).__name__}")

        status = "FAIL" if problems else "ok"
        failures += bool(problems)
        print(
            f"[{status}] {decision_type:<8} scenario={scenario} "
            f"T={trained['temperature']:.3f} value={decision['value']!r} "
            f"p={decision['probability']:.3f} version={decision['calibration_version']}"
        )
        for problem in problems:
            print(f"         {problem}")

    scenarios = call(args.base_url, "/scenarios", None, args.api_key)["scenarios"]
    print(f"scenarios registered: {len(scenarios)}")
    print(
        "note: the labels above are synthetic and unrelated to the model's own "
        "scores, so a temperature at its clamp and a low probability are the "
        "correct answers here. This checks plumbing, not model quality."
    )
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
