"""Weekly AI summary per product: a ranked bullet list (biggest customer impact first).

Reads data/<repo>/changes/<week>.csv and writes data/<repo>/summaries/<week>.json:
  {"<product>": {"row_count": n, "generated_at": ..., "provider": ..., "bullets": [
      {"headline": ..., "detail": ..., "impact": "High|Medium|Low", "urls": [...]}]}}
A product/week is (re)summarised only when its row count changed since the last summary,
so late changes picked up by the next Thursday scan refresh that week's summary. A summary that
fell back to the rule-based list because the model call failed is retried on the next run.
"""
from __future__ import annotations

import json
import os
import re
import time

import requests

from tracker.common import DATA_DIR, load_config, read_csv, utcnow

SYSTEM = (
    "You are a Microsoft cloud solution architect writing a weekly change digest for customer-facing "
    "colleagues. You receive documentation changes for ONE product and ONE week. Group related rows, "
    "ignore cosmetic edits, and return the most important changes as bullets ranked by customer impact "
    "(breaking changes, retirements and deprecations, GA, security, pricing/quota, new capabilities or models, "
    "previews, then notable guidance updates). Only use facts present in the input; do not speculate. "
    'Respond with JSON only: {"bullets":[{"headline":"<=12 words","detail":"1-2 sentences on what changed '
    'and why a customer should care","impact":"High|Medium|Low","urls":["<url from input>"]}]}'
)


def candidate_rows(rows: list[dict], limit: int) -> list[dict]:
    keep = [r for r in rows if r["metadata_only"] != "true" and "minor-edit" not in r["tags"].split(";")]
    keep.sort(key=lambda r: int(r["importance"] or 0), reverse=True)
    return keep[:limit]


def call_llm(cfg: dict, product: str, week: str, rows: list[dict]) -> list[dict] | None:
    provider = cfg.get("provider", "github-models")
    payload_rows = [{
        "title": r["title"], "url": r["url"] or r["commit_url"], "change": r["change_type"],
        "tags": r["tags"], "summary": r["change_summary"], "evidence": r["key_lines"],
        "lines": f"+{r['lines_added']}/-{r['lines_removed']}", "importance": r["importance"],
    } for r in rows]
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
        resp = post_with_retry(url, headers, body)
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
        clean.append({
            "headline": str(b.get("headline", ""))[:160],
            "detail": str(b.get("detail", ""))[:600],
            "impact": impact if impact in {"High", "Medium", "Low"} else "Medium",
            "urls": [u for u in urls if u in allowed][:3],
        })
    return clean


def post_with_retry(url: str, headers: dict, body: dict, attempts: int = 3) -> requests.Response:
    """POST, waiting and retrying on rate limits (429) and server errors (5xx)."""
    for i in range(attempts):
        resp = requests.post(url, headers=headers, json=body, timeout=120)
        retryable = resp.status_code == 429 or resp.status_code >= 500
        if not retryable or i == attempts - 1:
            return resp
        try:
            wait = min(60.0, float(resp.headers.get("Retry-After", "")))
        except ValueError:
            wait = 5.0 * 2 ** i
        print(f"  HTTP {resp.status_code}; retrying in {wait:.0f}s")
        time.sleep(wait)
    return resp


def rule_based(rows: list[dict], max_bullets: int) -> list[dict]:
    out = []
    for r in rows[:max_bullets]:
        imp = int(r["importance"] or 0)
        out.append({
            "headline": r["title"] or r["file_path"],
            "detail": r["change_summary"].rstrip(".") + "." + (f" Evidence: \"{r['key_lines']}\"" if r["key_lines"] else ""),
            "impact": "High" if imp >= 50 else "Medium" if imp >= 25 else "Low",
            "urls": [r["url"] or r["commit_url"]],
        })
    return out


def main() -> None:
    cfg = load_config()
    scfg = cfg.get("summary", {})
    for src in cfg["sources"]:
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
                if prev.get("row_count") == len(items) and not (prev.get("provider") == "rule-based" and wanted != "none"):
                    continue
                cands = candidate_rows(items, scfg.get("max_rows_per_product", 40))
                if not cands:
                    # Only minor or metadata-only edits this week: nothing to summarise.
                    bullets, provider = [], "none"
                else:
                    bullets, provider = call_llm(scfg, product, week, cands), wanted
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
