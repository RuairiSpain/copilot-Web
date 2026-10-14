"""Weekly AI summary per product: a ranked bullet list (biggest customer impact first).

Reads data/<repo>/changes/<week>.csv and writes data/<repo>/summaries/<week>.json:
  {"<product>": {"row_count": n, "generated_at": ..., "provider": ..., "bullets": [
      {"headline": ..., "detail": ..., "impact": "High|Medium|Low", "label": ..., "urls": [...]}]}}
A product/week is (re)summarised only when its row count changed since the last summary,
so late changes picked up by the next Thursday scan refresh that week's summary. A summary that
fell back to the rule-based list because the model was unavailable is retried on later runs, but only
for the last `summary.retry_weeks` weeks (default 4), and a run stops calling the model after
MAX_MODEL_FAILURES failed calls in a row.

A large edit (tag "large-edit", see scan.py) is sent with the text of the whole page, which scan.py
leaves in cache/, so the model summarises what the page now says rather than the diff.
"""
from __future__ import annotations

import datetime as dt
import json
import os
import re

from tracker.common import (
    DATA_DIR, LABELS, enabled_sources, http_request, is_true, large_edits_enabled, load_config, page_cache_path,
    read_csv, utcnow, week_start,
)

MAX_MODEL_FAILURES = 3  # failed model calls in a row before a run stops calling the model
PAGE_PROMPT_CHARS = 6_000  # page text sent per large edit
MAX_PAGES_PER_CALL = 5


def page_text(r: dict) -> str:
    """The cached page text of a large edit, or '' (not a large edit, switched off, or not cached)."""
    if "large-edit" not in r["tags"].split(";") or not large_edits_enabled():
        return ""
    path = page_cache_path(r.get("repo_id", ""), r.get("commit_sha", ""), r.get("file_path", ""))
    return path.read_text(encoding="utf-8")[:PAGE_PROMPT_CHARS] if path.exists() else ""

SYSTEM = (
    "You are a Microsoft cloud solution architect writing a weekly change digest for customer-facing "
    "colleagues. You receive documentation changes for ONE product and ONE week. Group related rows, "
    "ignore cosmetic edits, and return the most important changes as bullets ranked by customer impact "
    "(breaking changes, retirements and deprecations, GA, security, pricing/quota, new capabilities or models, "
    "previews, then notable guidance updates). Only use facts present in the input; do not speculate. "
    'A change with a "page" field was a large edit: summarise what the whole page now says, not the diff. '
    'Label each bullet with exactly one type: Breaking change, Retirement, GA, Preview, New or Doc update. '
    'Respond with JSON only: {"bullets":[{"headline":"<=12 words","detail":"1-2 sentences on what changed '
    'and why a customer should care","impact":"High|Medium|Low","label":"<type>","urls":["<url from input>"]}]}'
)


def candidate_rows(rows: list[dict], limit: int) -> list[dict]:
    keep = [r for r in rows if not is_true(r.get("noise")) and r["metadata_only"] != "true"
            and "minor-edit" not in r["tags"].split(";")]
    keep.sort(key=lambda r: int(r["importance"] or 0), reverse=True)
    return keep[:limit]


def call_llm(cfg: dict, product: str, week: str, rows: list[dict]) -> list[dict] | None:
    provider = cfg.get("provider", "github-models")
    payload_rows, pages = [], 0
    for r in rows:
        row = {"title": r["title"], "url": r["url"] or r["commit_url"], "change": r["change_type"], "tags": r["tags"],
               "label": r.get("change_label", ""), "summary": r["change_summary"], "evidence": r["key_lines"],
               "lines": f"+{r['lines_added']}/-{r['lines_removed']}", "importance": r["importance"]}
        text = page_text(r) if pages < MAX_PAGES_PER_CALL else ""
        if text:
            row["page"] = text
            pages += 1
        payload_rows.append(row)
    messages = [
        {"role": "system", "content": SYSTEM},
        {"role": "user", "content": f"Product: {product}\nWeek starting: {week}\nMax bullets: {cfg.get('max_bullets', 8)}\n"
                                     f"Changes (JSON):\n{json.dumps(payload_rows, ensure_ascii=False)}"},
    ]
    body = {"messages": messages, "temperature": 0.2}
    if provider == "github-models":
        token = os.environ.get("GITHUB_TOKEN")
        if not token:
            return None
        url = "https://models.github.ai/inference/chat/completions"
        headers = {"Authorization": f"Bearer {token}", "Content-Type": "application/json"}
        body["model"] = cfg.get("model", "openai/gpt-4.1-mini")
    elif provider == "azure-openai":
        ep, key, dep = (os.environ.get(k) for k in ("AZURE_OPENAI_ENDPOINT", "AZURE_OPENAI_API_KEY", "AZURE_OPENAI_DEPLOYMENT"))
        if not (ep and key and dep):
            return None
        url = f"{ep.rstrip('/')}/openai/deployments/{dep}/chat/completions?api-version=2024-10-21"
        headers = {"api-key": key, "Content-Type": "application/json"}
    else:
        return None
    try:
        resp = http_request("POST", url, headers=headers, json=body, timeout=120, retry_5xx=True)  # no side effects
        resp.raise_for_status()
        text = resp.json()["choices"][0]["message"]["content"]
        text = re.sub(r"^```(json)?|```$", "", text.strip(), flags=re.MULTILINE).strip()
        bullets = json.loads(text).get("bullets", [])
    except Exception as exc:  # any bad response falls back to the rule-based summary
        print(f"  summary call failed for {product} {week}: {exc}")
        return None
    if not isinstance(bullets, list):
        print(f"  summary call for {product} {week} returned no bullet list")
        return None
    allowed = {p["url"] for p in payload_rows}
    clean = []
    for b in bullets[: cfg.get("max_bullets", 8)]:
        if not isinstance(b, dict):
            continue
        urls = [u for u in b.get("urls") or [] if isinstance(u, str)] if isinstance(b.get("urls"), list) else []
        impact = b.get("impact") if isinstance(b.get("impact"), str) else ""
        label = b.get("label") if b.get("label") in LABELS else "Doc update"
        clean.append({
            "headline": str(b.get("headline", ""))[:160],
            "detail": str(b.get("detail", ""))[:600],
            "impact": impact if impact in {"High", "Medium", "Low"} else "Medium",
            "label": label,
            "urls": [u for u in urls if u in allowed][:3],
        })
    return clean


def rule_based(rows: list[dict], max_bullets: int) -> list[dict]:
    out = []
    for r in rows[:max_bullets]:
        imp = int(r["importance"] or 0)
        detail = r["change_summary"].rstrip(".") + "." + (f" Evidence: \"{r['key_lines']}\"" if r["key_lines"] else "")
        if "large-edit" in r["tags"].split(";") and r.get("description"):
            detail += f" The page now covers: {r['description']}"
        out.append({
            "headline": r["title"] or r["file_path"],
            "detail": detail,
            "impact": "High" if imp >= 50 else "Medium" if imp >= 25 else "Low",
            "label": r.get("change_label") or "Doc update",
            "urls": [r["url"] or r["commit_url"]],
        })
    return out


def main() -> None:
    cfg = load_config()
    scfg = cfg.get("summary", {})
    retry_from = week_start(utcnow() - dt.timedelta(weeks=scfg.get("retry_weeks", 4)))
    failures = 0
    for src in enabled_sources(cfg):
        base = DATA_DIR / src["id"]
        for csv_path in sorted((base / "changes").glob("*.csv")):
            week = csv_path.stem
            rows = read_csv(csv_path)
            out_path = base / "summaries" / f"{week}.json"
            existing = json.loads(out_path.read_text(encoding="utf-8")) if out_path.exists() else {}
            by_product: dict[str, list[dict]] = {}
            for r in rows:
                by_product.setdefault(r["product"], []).append(r)
            changed = False
            wanted = scfg.get("provider", "none")
            for product, items in sorted(by_product.items()):
                prev = existing.get(product, {})
                retry = prev.get("provider") == "rule-based" and wanted != "none" and week >= retry_from
                if prev.get("row_count") == len(items) and not retry:
                    continue
                cands = candidate_rows(items, scfg.get("max_rows_per_product", 40))
                if not cands:
                    # Only minor or metadata-only edits this week: nothing to summarise.
                    bullets, provider = [], "none"
                else:
                    bullets, provider = None, wanted
                    if failures < MAX_MODEL_FAILURES:
                        bullets = call_llm(scfg, product, week, cands)
                        failures = 0 if bullets is not None else failures + 1
                        if failures == MAX_MODEL_FAILURES:
                            print(f"  {failures} model calls failed in a row; using rule-based summaries for the rest of this run")
                    if bullets is None:
                        bullets, provider = rule_based(cands, scfg.get("max_bullets", 8)), "rule-based"
                if prev.get("row_count") == len(items) and provider == prev.get("provider"):
                    continue  # retry failed again; keep the existing summary and file untouched
                order = {"High": 0, "Medium": 1, "Low": 2}
                bullets.sort(key=lambda b: order.get(b["impact"], 1))  # stable: keeps model ranking within a level
                existing[product] = {"row_count": len(items), "generated_at": utcnow().isoformat(),
                                     "provider": provider, "bullets": bullets}
                changed = True
                print(f"[{src['id']}] {week} {product}: {len(bullets)} bullets ({provider})")
            if changed:
                out_path.parent.mkdir(parents=True, exist_ok=True)
                out_path.write_text(json.dumps(existing, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
