#!/usr/bin/env python3
"""Command 4 — show the Foundry IQ pipeline behind a query.

Three modes, and one thing worth being straight about first:

**The `activity` array arrives with the response, not before it.** Agentic
retrieval is a single HTTP call; there is no event stream of subqueries as the
planner issues them. So "live" here means one of two honest things:

    --watch      run this in a second terminal. It tails `.trace/latest.json`
                 and renders each new pipeline the moment 3_search.py writes
                 one. This is the demo-friendly mode: two panes, question on
                 the left, pipeline appearing on the right.

    --live "q"   issues the query itself, showing an elapsed ticker while the
                 service works, then expands the real records when they land.
                 The ticker is a progress indicator, not streamed internals.

    --replay F   render a saved trace. Deterministic, good for slides.

    --log-analytics
                 the genuinely server-side view: the KQL for the search
                 service's diagnostic logs, run through `az monitor
                 log-analytics query` if you pass --workspace. Requires
                 diagnostic settings on the search service (see README).
"""

from __future__ import annotations

import argparse
import json
import shutil
import subprocess
import sys
import threading
import time
from pathlib import Path
from typing import Any

from _common import ConfigError, SearchClient, fail, load_settings
from trace_view import DIM, RESET, render_activity, summarize, totals

TRACE_DIR = Path(__file__).resolve().parents[1] / ".trace"
DEFAULT_TRACE = TRACE_DIR / "latest.json"

KQL = """\
AzureDiagnostics
| where ResourceProvider == "MICROSOFT.SEARCH"
| where TimeGenerated > ago({window})
| project TimeGenerated, OperationName, Query_s, IndexName_s, Documents_d, DurationMs, resultSignature_d
| order by TimeGenerated desc
| take {take}
"""


def render_trace_file(path: Path, *, title: str) -> None:
    payload = json.loads(path.read_text(encoding="utf-8"))
    response = payload.get("response", payload)
    request = payload.get("request", {})
    activity = response.get("activity") or []

    question = ""
    for message in request.get("messages", []):
        if message.get("role") == "user":
            for part in message.get("content", []):
                if part.get("type") == "text":
                    question = part["text"]

    print(f"\n\033[1m{title}\033[0m")
    if question:
        print(f"{DIM}query{RESET}  {question}")
    effort = (request.get("retrievalReasoningEffort") or {}).get("kind")
    if effort:
        print(f"{DIM}effort{RESET} {effort}")
    render_activity(activity, title="pipeline")
    print(f"\n{summarize(activity, payload.get('wallMs'))}")

    counts = totals(activity)
    if counts["subqueries"] > 1:
        print(
            f"{DIM}note: the planner fanned out to {counts['subqueries']} subqueries — "
            f"that fan-out is the 'agentic' part of agentic retrieval.{RESET}"
        )


def watch(path: Path, interval: float) -> int:
    print(f"watching {path} — run 3_search.py in another terminal (Ctrl-C to stop)")
    last_stamp = path.stat().st_mtime if path.exists() else 0.0
    try:
        while True:
            if path.exists():
                stamp = path.stat().st_mtime
                if stamp != last_stamp:
                    last_stamp = stamp
                    # The writer truncates then writes; a partial read is a
                    # normal race here, so retry rather than crash the pane.
                    for _ in range(5):
                        try:
                            render_trace_file(path, title=time.strftime("trace %H:%M:%S"))
                            break
                        except json.JSONDecodeError:
                            time.sleep(0.1)
            time.sleep(interval)
    except KeyboardInterrupt:
        print("\nstopped")
    return 0


def live(query: str, effort: str) -> int:
    try:
        settings = load_settings()
    except ConfigError as err:
        fail(str(err))
        return 1

    client = SearchClient(settings.search_endpoint)
    request = {
        "messages": [{"role": "user", "content": [{"type": "text", "text": query}]}],
        "outputMode": "answerSynthesis",
        "retrievalReasoningEffort": {"kind": effort},
        "includeActivity": True,
    }
    result: dict[str, Any] = {}

    def call() -> None:
        try:
            result["response"] = client.post(f"{settings.kb_path}/retrieve", request, timeout=180)
        except Exception as err:  # surfaced after the ticker stops
            result["error"] = err

    print(f"\033[1mquery\033[0m {query}   {DIM}effort={effort}{RESET}")
    print(f"{DIM}the service returns its activity log with the response; the ticker below is elapsed time, "
          f"not streamed internals{RESET}")

    worker = threading.Thread(target=call, daemon=True)
    started = time.time()
    worker.start()
    frames = "|/-\\"
    frame = 0
    while worker.is_alive():
        sys.stdout.write(f"\r  {frames[frame % len(frames)]} retrieving… {time.time() - started:5.1f}s")
        sys.stdout.flush()
        frame += 1
        time.sleep(0.12)
    worker.join()
    wall_ms = int((time.time() - started) * 1000)
    sys.stdout.write("\r" + " " * 40 + "\r")

    if "error" in result:
        fail(str(result["error"]))
        return 1

    response = result["response"]
    TRACE_DIR.mkdir(parents=True, exist_ok=True)
    DEFAULT_TRACE.write_text(
        json.dumps({"request": request, "response": response, "wallMs": wall_ms}, indent=2), encoding="utf-8"
    )
    render_activity(response.get("activity") or [], title="pipeline")
    print(f"\n{summarize(response.get('activity') or [], wall_ms)}")
    return 0


def log_analytics(workspace: str | None, window: str, take: int) -> int:
    query = KQL.format(window=window, take=take)
    print("\033[1mKQL\033[0m (search service diagnostic logs)\n")
    print(query)
    if not workspace:
        print(f"{DIM}pass --workspace <log-analytics-workspace-id> to run it{RESET}")
        return 0
    if not shutil.which("az"):
        fail("az CLI not found on PATH")
        return 1
    completed = subprocess.run(
        ["az", "monitor", "log-analytics", "query", "--workspace", workspace, "--analytics-query", query, "-o", "json"],
        capture_output=True,
        text=True,
        check=False,
    )
    if completed.returncode != 0:
        fail(f"az monitor log-analytics query failed: {completed.stderr.strip()[:800]}")
        return 1
    rows = json.loads(completed.stdout or "[]")
    if not rows:
        print(f"{DIM}no rows — diagnostic settings may not be enabled on the search service (see README){RESET}")
        return 0
    for row in rows:
        print(
            f"  {row.get('TimeGenerated', '')[:19]}  {str(row.get('OperationName', ''))[:28]:<28} "
            f"{str(row.get('IndexName_s', ''))[:24]:<24} {row.get('DurationMs', '')}ms"
        )
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--watch", action="store_true", help="tail the trace file and render each new pipeline")
    mode.add_argument("--live", metavar="QUERY", help="issue the query here and render its pipeline")
    mode.add_argument("--replay", nargs="?", const=DEFAULT_TRACE, type=Path, help="render a saved trace file")
    mode.add_argument("--log-analytics", action="store_true", help="show/run the KQL for search diagnostic logs")
    parser.add_argument("--file", type=Path, default=DEFAULT_TRACE, help="trace file for --watch")
    parser.add_argument("--interval", type=float, default=0.4, help="poll interval for --watch")
    parser.add_argument("--effort", default="auto", choices=["minimal", "low", "medium", "auto"])
    parser.add_argument("--workspace", help="Log Analytics workspace id for --log-analytics")
    parser.add_argument("--window", default="30m", help="KQL lookback window")
    parser.add_argument("--take", type=int, default=40)
    args = parser.parse_args()

    if args.watch:
        return watch(args.file, args.interval)
    if args.live:
        return live(args.live, args.effort)
    if args.log_analytics:
        return log_analytics(args.workspace, args.window, args.take)

    path = args.replay or DEFAULT_TRACE
    if not path.exists():
        fail(f"{path} not found — run 3_search.py first, or use --live")
        return 1
    render_trace_file(path, title=f"replay {path.name}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
