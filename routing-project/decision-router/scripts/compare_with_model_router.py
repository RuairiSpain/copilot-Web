#!/usr/bin/env python3
"""Compare the Decision-1 pipeline with Foundry Model Router on the routing dataset.

Model Router's routing mode is a deployment setting, so give one Model Router deployment
per mode, all with the same model subset as config/model_catalog.json:

  export FOUNDRY_ENDPOINT=https://<resource>.services.ai.azure.com
  export MODEL_ROUTER_DEPLOYMENTS='{"cost": "model-router-cost", "balanced": "model-router", "quality": "model-router-quality"}'
  python scripts/compare_with_model_router.py --sample 400 --output-dir eval/run-001

  # Without credentials, against the in-memory fake (checks the harness, not the routers):
  python scripts/compare_with_model_router.py --dry-run --sample 64 --output-dir eval/dry-run
"""
from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
from dataclasses import replace
from pathlib import Path

import httpx

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from decision_router.auth import FoundryAuth  # noqa: E402
from decision_router.catalog import Catalog  # noqa: E402
from decision_router.comparison import load_rows, render_markdown, run, stratified_sample, summarize  # noqa: E402
from decision_router.config import Settings  # noqa: E402
from decision_router.decision1 import Decision1Client  # noqa: E402
from decision_router.foundry import ChatClient  # noqa: E402
from decision_router.pipeline import RouterPipeline  # noqa: E402
from decision_router.pricing import PriceTable  # noqa: E402
from decision_router.telemetry import Telemetry  # noqa: E402
from decision_router.testing import FakeFoundry  # noqa: E402

DATASET = Path(__file__).resolve().parents[2] / "dataset-v31"


async def main(args: argparse.Namespace) -> int:
    settings = Settings.from_env()
    output_dir: Path = args.output_dir
    output_dir.mkdir(parents=True, exist_ok=True)
    settings = replace(settings, decision_log_path=output_dir / "decision_log.jsonl")
    catalog = Catalog.load(settings.catalog_path, settings.deployment_overrides, settings.default_mode)
    prices = PriceTable(settings.pricing_path)

    router_deployments = json.loads(os.getenv("MODEL_ROUTER_DEPLOYMENTS") or "{}")
    if args.dry_run:
        router_deployments = {mode: f"model-router-{mode}" for mode in catalog.modes}
        pool = [m.deployment for m in catalog.models]
        fake = FakeFoundry(pool, {f"model-router-{mode}": [m.name for m in catalog.models] for mode in catalog.modes})
        http = httpx.AsyncClient(transport=fake.transport())
        settings = replace(settings, foundry_endpoint="https://dry-run.services.ai.azure.com")
        auth = FoundryAuth(api_key="dry-run")
    else:
        if not settings.configured:
            print("Set FOUNDRY_ENDPOINT (see the module docstring), or use --dry-run.", file=sys.stderr)
            return 2
        missing = set(catalog.modes) - set(router_deployments)
        if missing:
            print(f"MODEL_ROUTER_DEPLOYMENTS has no deployment for: {sorted(missing)}", file=sys.stderr)
            return 2
        http = httpx.AsyncClient(limits=httpx.Limits(max_connections=args.concurrency * 4))
        auth = FoundryAuth(settings.foundry_api_key)

    decision_auth = FoundryAuth(settings.decision1_api_key) if settings.decision1_api_key and not args.dry_run else auth
    chat = ChatClient(settings.resolved_chat_url or "", auth, http=http)
    pipeline = RouterPipeline(
        settings, catalog,
        Decision1Client(settings.resolved_decision1_url or "", settings.decision1_deployment, decision_auth,
                        timeout_seconds=settings.decision_timeout_seconds,
                        max_attempts=settings.decision_max_attempts, http=http),
        chat, Telemetry(settings.decision_log_path, settings.log_prompts), prices,
    )

    paths = args.data or [DATASET / "routing_v31_test_ood_templates.jsonl"]
    rows = stratified_sample(load_rows(paths), args.sample, args.seed)
    print(f"running {len(rows)} prompts through both arms ...", file=sys.stderr)
    try:
        records = await run(
            rows, concurrency=args.concurrency, output=output_dir / "results.jsonl",
            pipeline=pipeline, router=chat, router_deployments=router_deployments, catalog=catalog, prices=prices,
            max_output_tokens=args.max_output_tokens, max_tokens_param=args.max_tokens_param,
            decide_only=args.decide_only, store_outputs=not args.no_outputs, timeout=settings.request_timeout_seconds,
            shuffle_repeats=args.shuffle_options, seed=args.seed,
        )
    finally:
        await pipeline.close()
        await http.aclose()

    summary = summarize(records)
    meta = {"data": [str(p) for p in paths], "sample": args.sample, "seed": args.seed, "dry_run": args.dry_run,
            "decide_only": args.decide_only, "shuffle_options": args.shuffle_options,
            "low_confidence_threshold": settings.low_confidence_threshold, "catalog_sha256": catalog.fingerprint,
            "router_deployments": router_deployments, "decision1_deployment": settings.decision1_deployment}
    (output_dir / "report.json").write_text(json.dumps({"meta": meta, "summary": summary}, indent=2) + "\n")
    (output_dir / "report.md").write_text(render_markdown(summary, meta))
    print(render_markdown(summary, meta))
    return 0


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--data", type=Path, action="append", help="dataset JSONL (repeatable); default: OOD split")
    parser.add_argument("--sample", type=int, default=200, help="rows, stratified by task x preference; 0 = all")
    parser.add_argument("--seed", type=int, default=20261010)
    parser.add_argument("--concurrency", type=int, default=4)
    parser.add_argument("--max-output-tokens", type=int, default=512)
    parser.add_argument("--max-tokens-param", choices=["max_completion_tokens", "max_tokens", "none"],
                        default="max_completion_tokens",
                        help="reasoning models reject max_tokens; some non-OpenAI models reject max_completion_tokens")
    parser.add_argument("--decide-only", action="store_true",
                        help="stop our arm after Decision-1 (Model Router still generates: it has no route-only call)")
    parser.add_argument("--shuffle-options", type=int, nargs="?", const=1, default=0, metavar="N",
                        help="option-order test: re-ask Decision-1 N more times per prompt (default 1) with the "
                             "options in a different order, and report how often the ranking changes. "
                             "See docs/OPTION_ORDER_TESTING.md")
    parser.add_argument("--no-outputs", action="store_true", help="do not store response text")
    parser.add_argument("--dry-run", action="store_true", help="use the in-memory fake instead of Foundry")
    parser.add_argument("--output-dir", type=Path, default=Path("eval/comparison"))
    raise SystemExit(asyncio.run(main(parser.parse_args())))
