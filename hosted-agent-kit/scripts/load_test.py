"""Send concurrent requests to a running pooling service and report latency and outcomes.

Local example (service started with POOL_AUTH_MODE=development):
    uv run python scripts/load_test.py --url http://localhost:8080 --agent research-agent \
        --requests 200 --concurrency 50 --users 20

Against Azure, pass a bearer token with --token (or the POOL_TOKEN environment variable).
The script sends real prompts to real agents, so it consumes Foundry capacity. Use it on a
non-production project.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import statistics
import time
from collections import Counter
from dataclasses import dataclass, field

import httpx


@dataclass
class Report:
    requests: int
    concurrency: int
    elapsed_seconds: float
    statuses: Counter[str] = field(default_factory=Counter)
    error_codes: Counter[str] = field(default_factory=Counter)
    latencies: list[float] = field(default_factory=list)

    @property
    def throughput(self) -> float:
        return self.requests / self.elapsed_seconds if self.elapsed_seconds > 0 else 0.0

    def percentile(self, fraction: float) -> float:
        if not self.latencies:
            return 0.0
        ordered = sorted(self.latencies)
        return ordered[min(len(ordered) - 1, int(fraction * len(ordered)))]

    def as_dict(self) -> dict[str, object]:
        return {
            "requests": self.requests,
            "concurrency": self.concurrency,
            "elapsed_seconds": round(self.elapsed_seconds, 3),
            "throughput_per_second": round(self.throughput, 2),
            "statuses": dict(self.statuses),
            "error_codes": dict(self.error_codes),
            "latency_seconds": {
                "mean": round(statistics.fmean(self.latencies), 4) if self.latencies else 0.0,
                "p50": round(self.percentile(0.50), 4),
                "p95": round(self.percentile(0.95), 4),
                "p99": round(self.percentile(0.99), 4),
                "max": round(max(self.latencies), 4) if self.latencies else 0.0,
            },
        }


async def run_load(
    client: httpx.AsyncClient,
    *,
    agent: str,
    requests: int,
    concurrency: int,
    users: int,
    message: str = "load test",
    token: str | None = None,
    stream: bool = False,
) -> Report:
    """Run ``requests`` chat calls, at most ``concurrency`` at once, cycling over ``users``."""
    report = Report(requests=requests, concurrency=concurrency, elapsed_seconds=0.0)
    gate = asyncio.Semaphore(concurrency)

    async def one(index: int) -> None:
        headers = (
            {"Authorization": f"Bearer {token}"}
            if token
            else {"X-Dev-User-Id": f"load-user-{index % users}"}
        )
        body = {"message": message, "stream": stream}
        async with gate:
            started = time.perf_counter()
            try:
                response = await client.post(f"/v1/agents/{agent}/chat", json=body, headers=headers)
                await response.aread()
            except httpx.HTTPError as exc:
                report.statuses[type(exc).__name__] += 1
                return
            report.latencies.append(time.perf_counter() - started)
            report.statuses[str(response.status_code)] += 1
            if response.status_code >= 400:
                try:
                    report.error_codes[str(response.json().get("error_code", "unknown"))] += 1
                except ValueError:
                    report.error_codes["non-json"] += 1

    started_all = time.perf_counter()
    await asyncio.gather(*(one(i) for i in range(requests)))
    report.elapsed_seconds = time.perf_counter() - started_all
    return report


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default="http://localhost:8080")
    parser.add_argument("--agent", required=True)
    parser.add_argument("--requests", type=int, default=100)
    parser.add_argument("--concurrency", type=int, default=20)
    parser.add_argument(
        "--users", type=int, default=10, help="distinct development users to cycle through"
    )
    parser.add_argument("--token", default=os.environ.get("POOL_TOKEN"))
    parser.add_argument("--stream", action="store_true")
    parser.add_argument("--timeout", type=float, default=300.0)
    args = parser.parse_args(argv)

    async def go() -> Report:
        async with httpx.AsyncClient(base_url=args.url, timeout=args.timeout) as client:
            return await run_load(
                client,
                agent=args.agent,
                requests=args.requests,
                concurrency=args.concurrency,
                users=max(1, args.users),
                token=args.token,
                stream=args.stream,
            )

    report = asyncio.run(go())
    print(json.dumps(report.as_dict(), indent=2))
    failures = sum(count for status, count in report.statuses.items() if not status.startswith("2"))
    return 0 if failures == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())
