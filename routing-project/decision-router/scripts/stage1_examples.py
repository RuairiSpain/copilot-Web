#!/usr/bin/env python3
"""Generate docs/STAGE1_EXAMPLES.md: example requests and what stage 1 hands to Decision-1.

Every output in the document is computed by running the request through the router's own
request handling (profiles, validation) and stage 1 against the checked-in catalogs, so the
document cannot drift from the code:

  python scripts/stage1_examples.py            # rewrite the document
  python scripts/stage1_examples.py --check    # exit 1 if the document is out of date (used by a test)

To add an example, append to EXAMPLES in stage1_examples_data.py and rerun.
"""
from __future__ import annotations

import argparse
import json
import sys
from dataclasses import replace
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
sys.path.insert(0, str(Path(__file__).resolve().parent))
from stage1_examples_data import EXAMPLES, FINDINGS, INTRO, ZERO_INTRO  # noqa: E402

from decision_router.catalog import Catalogs  # noqa: E402
from decision_router.config import Settings  # noqa: E402
from decision_router.instructions import InstructionTemplates  # noqa: E402
from decision_router.pipeline import RouterPipeline, RoutingError  # noqa: E402
from decision_router.stage1 import estimated_tokens, select  # noqa: E402

DOC = ROOT.parent / "docs" / "STAGE1_EXAMPLES.md"
MAX_LISTED = 12  # above this many removals in a step, models are counted per reason instead of named


def run(example: dict) -> dict:
    settings = replace(Settings(foundry_endpoint="https://docs.invalid", allow_unauthenticated=True),
                       **example.get("settings", {}))
    catalogs = Catalogs.load(settings.catalog_dir, settings.compatibility)
    pipe = RouterPipeline(settings, catalogs, None, None, None, None)  # prepare() needs settings and catalogs only
    try:
        prepared = pipe.prepare(json.loads(json.dumps(example["request"])))
    except RoutingError as exc:
        return {"rejected": exc.body()["error"]}
    result = select(prepared.catalog, prepared.mode, prepared.constraints, prepared.body, settings,
                    passthrough=prepared.passthrough, cost_band=prepared.cost_band)
    prompt_tokens, output_tokens = estimated_tokens(prepared.body, settings.chars_per_token)
    rendered = None
    if len(result.candidates) >= 2:
        rendered = InstructionTemplates.load(settings.instructions_path).render(
            result.candidates, mode=prepared.mode, constraints=prepared.constraints, cost_band=prepared.cost_band,
            requirements=result.requirements, body=prepared.body, chars_per_token=settings.chars_per_token,
            profile=prepared.profile, passthrough=prepared.passthrough)
    return {
        "rendered": rendered,
        "catalog": prepared.catalog, "mode": prepared.mode, "profile": prepared.profile, "pin": prepared.pin, "cost_band": prepared.cost_band,
        "passthrough": prepared.passthrough, "constraints": prepared.constraints.as_dict(),
        "requirements": result.requirements, "steps": result.steps, "empty_at": result.empty_at,
        "candidates": [m.name for m in result.candidates], "tokens": (prompt_tokens, output_tokens),
        "pool": len(prepared.catalog.models),
    }


def shorten(value, limit: int = 160):
    """Long message text in the requests is shown shortened; the real request is used for the run."""
    if isinstance(value, str) and len(value) > limit:
        return f"<{len(value):,} characters>"
    if isinstance(value, list):
        return [shorten(v, limit) for v in value]
    if isinstance(value, dict):
        return {k: shorten(v, limit) for k, v in value.items()}
    return value


def steps_table(out: dict) -> list[str]:
    lines = ["| Step | Removed | Left |", "|---|---|---|"]
    for step in out["steps"]:
        removed = step["removed"]
        by_reason: dict[str, list[str]] = {}
        for r in removed:
            by_reason.setdefault(r["reason"], []).append(r["model"])
        if not removed:
            cell = "–"
        elif len(removed) > MAX_LISTED:
            cell = "<br>".join(f"{len(models)} × {reason}" for reason, models in
                               sorted(by_reason.items(), key=lambda kv: -len(kv[1])))
        else:
            cell = "<br>".join(f"{reason}: " + ", ".join(f"`{m}`" for m in models) for reason, models in by_reason.items())
        lines.append(f"| `{step['step']}` | {cell} | {step['remaining']} |")
    return lines


def render_example(number: str, example: dict, out: dict) -> list[str]:
    lines = [f"### {number}. {example['title']}", "", f"**Why it is useful:** {example['why']}", ""]
    request = shorten(example["request"])
    fields = [f'  "{key}": {json.dumps(value, ensure_ascii=False)}' for key, value in request.items()]
    lines += ["```json", "{", ",\n".join(fields), "}", "```", ""]
    if example.get("settings"):
        env = ", ".join(f"`{k}={v if not isinstance(v, tuple) else ','.join(v)}`" for k, v in example["settings"].items())
        lines += [f"Router settings for this example: {env}.", ""]
    if "rejected" in out:
        err = out["rejected"]
        lines += [f"**Result:** rejected with **400 `{err['code']}`** before stage 1: {err['message']}", "",
                  example["explanation"], ""]
        return lines
    facts = [f"catalog `{out['catalog'].compatibility}` ({out['pool']} models)", f"mode `{out['mode']}`"]
    if out["profile"]:
        facts.append(f"profile `{out['profile']}`")
    if out["passthrough"]:
        facts.append("`claude_translation: false`")
    if out["cost_band"]:
        facts.append(f"cost band {out['cost_band'].label()}")
    if out["pin"]:
        facts.append("pinning on")
    facts.append(f"estimated ~{out['tokens'][0]:,} prompt tokens")
    lines += ["**Read from the request:** " + ", ".join(facts) + ". Requirements: "
              + (", ".join(f"`{k}` ({v})" for k, v in out["requirements"].items()) or "none") + ".", ""]
    lines += steps_table(out) + [""]
    if out["empty_at"]:
        lines += [f"**Stage 1 output:** no models. The request gets **422 `no_eligible_models`**; the "
                  f"`{out['empty_at']}` step removed the last candidates. Decision-1 is not called.", ""]
    elif len(out["candidates"]) == 1:
        lines += [f"**Stage 1 output:** one model, `{out['candidates'][0]}`. It is called directly; "
                  "Decision-1 is not called.", ""]
    else:
        names = ", ".join(f"`{n}`" for n in out["candidates"])
        lines += [f"**Stage 1 output (the {len(out['candidates'])} options Decision-1 chooses from, cheapest "
                  f"first):** {names}", ""]
        rendered = out["rendered"]
        lines += [f"**Instructions sent to Decision-1** (fragments: {', '.join(f'`{f}`' for f in rendered.fragments)}):",
                  "", f"> {rendered.instructions}", ""]
    lines += [f"**Explanation:** {example['explanation']}", ""]
    return lines


def render() -> str:
    results = [(e, run(e)) for e in EXAMPLES]
    routed = [(e, o) for e, o in results if not o.get("empty_at") and "rejected" not in o]
    empty = [(e, o) for e, o in results if o.get("empty_at")]
    rejected = [(e, o) for e, o in results if "rejected" in o]
    lines = ["<!-- Generated by decision-router/scripts/stage1_examples.py from stage1_examples_data.py. "
             "Do not edit by hand. -->", "", "# Stage 1 examples", "", INTRO.strip(), ""]
    lines += ["## Summary", "", "| # | Example | Stage 1 output |", "|---|---|---|"]
    groups = (("A", routed), ("Z", empty), ("R", rejected))
    for prefix, group in groups:
        for i, (e, o) in enumerate(group, 1):
            if "rejected" in o:
                result = f"400 `{o['rejected']['code']}`"
            elif o["empty_at"]:
                result = f"0 models (empty at `{o['empty_at']}`)"
            else:
                result = f"{len(o['candidates'])} of {o['pool']}"
            lines.append(f"| {prefix}{i} | {e['title']} | {result} |")
    lines += ["", "## Findings", "", FINDINGS.strip(), "", "## Requests that reach Decision-1", ""]
    for i, (e, o) in enumerate(routed, 1):
        lines += render_example(f"A{i}", e, o)
    lines += ["## Requests that leave no models", "", ZERO_INTRO.strip(), ""]
    for i, (e, o) in enumerate(empty, 1):
        lines += render_example(f"Z{i}", e, o)
    if rejected:
        lines += ["## Requests rejected before stage 1", "",
                  "These never reach stage 1: the request itself is invalid, so the caller gets a 400 "
                  "that says what to fix, not an empty model list.", ""]
        for i, (e, o) in enumerate(rejected, 1):
            lines += render_example(f"R{i}", e, o)
    return "\n".join(lines).rstrip() + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--check", action="store_true", help="fail if the document is out of date")
    args = parser.parse_args()
    text = render()
    if args.check:
        if not DOC.exists() or DOC.read_text() != text:
            print(f"{DOC} is out of date: run scripts/stage1_examples.py", file=sys.stderr)
            return 1
        return 0
    DOC.write_text(text)
    print(f"wrote {DOC}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
