#!/usr/bin/env python3
"""Write the exact Decision-1 request body the service would send for each dataset prompt.

Useful for inspecting what Decision-1 sees, or for sending the requests from another tool.

  python scripts/export_decision1_requests.py --data ../dataset-v31/routing_v31_test_ood_templates.jsonl \
      --output eval/decision1_requests_ood.jsonl
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from decision_router.catalog import Catalogs  # noqa: E402
from decision_router.comparison import PREFERENCE_TO_MODE, build_body, load_rows  # noqa: E402
from decision_router.config import Settings  # noqa: E402
from decision_router.decision1 import build_request, build_state  # noqa: E402
from decision_router.stage1 import Constraints, select  # noqa: E402

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--data", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--max-output-tokens", type=int, default=512)
    parser.add_argument("--compatibility", choices=["old", "new"], default="old")
    args = parser.parse_args()

    settings = Settings.from_env()
    catalog = Catalogs.load(settings.catalog_dir, args.compatibility, settings.deployment_overrides,
                            settings.default_mode).get(args.compatibility)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    count = 0
    with args.output.open("w", encoding="utf-8") as handle:
        for row in load_rows([args.data]):
            mode = PREFERENCE_TO_MODE[row["quality_preference"]]
            body = build_body(row["prompt"], args.max_output_tokens, "max_completion_tokens")
            candidates = select(catalog, mode, Constraints(), body, settings).candidates
            request = build_request(settings.decision1_deployment, build_state(body, settings.state_max_chars),
                                    catalog.modes[mode].instructions, catalog.criteria(candidates))
            handle.write(json.dumps({"id": row.get("id"), "routing_mode": mode, "request": request,
                                     "policy_label": row.get("selected_model")}, ensure_ascii=False) + "\n")
            count += 1
    print(f"wrote {count} requests to {args.output}")
