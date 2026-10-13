"""Weekly digest per subscriber (config/subscriptions.yml), by email or as a Teams channel post.

Each subscriber gets what is new since their last digest: the AI highlights for their products,
matching announcements, and significant documentation changes (noise excluded), with silent
changes marked. Every digest is archived to data/digests/<date>/<subscriber>.{md,html}.

Delivery (only with --send; secrets come from environment variables named in the config):
  channel: teams  -> POST an Adaptive Card to a Teams "Workflows" webhook (webhook_env)
  channel: email  -> transport: logic-app (POST to an HTTP-triggered Logic App, logic_app_env)
                     or smtp (SMTP_HOST, SMTP_PORT, SMTP_USERNAME, SMTP_PASSWORD, SMTP_FROM)

Usage:
  python -m tracker.digest            # render and archive only
  python -m tracker.digest --send     # also deliver, and remember what was sent
"""
from __future__ import annotations

import argparse
import datetime as dt
import html
import json
import os
import smtplib
import sys
from collections import defaultdict
from email.message import EmailMessage

from tracker.common import (
    DATA_DIR, DIGEST_STATE_PATH, LABELS, SUBSCRIPTIONS_PATH, http_request, is_true, load_config, load_json, load_yaml,
    save_json, utcnow,
)
from tracker.correlate import correlate
from tracker.store import load_announcements, load_changes, load_summaries

FIRST_DIGEST_DAYS = 7
CARD_LIMIT = 25_000  # Teams rejects cards much over 28 KB
AI_NOTE = "Highlights are AI-generated from the documentation diffs. Check the linked page before acting on them."


class DeliveryError(RuntimeError):
    pass


class NotConfigured(DeliveryError):
    """A secret or recipient list is missing: the digest is archived but not sent, and the run does not fail."""


def wants(sub: dict, products: list[str]) -> bool:
    chosen = sub.get("products") or ["*"]
    return "*" in chosen or any(p in chosen for p in products)


def build(sub: dict, cfg: dict, rows: list[dict], announcements: list[dict], summaries: dict, since: str) -> dict:
    """What a subscriber should see: rows and announcements collected after `since`, filtered to their choices."""
    thr = sub.get("min_importance", cfg["project"].get("significant_threshold", 30))
    labels = set(sub.get("labels") or LABELS)
    limit = sub.get("max_items", 25)
    picked = [r for r in rows if r["scan_id"] > since and not is_true(r.get("noise")) and not is_true(r.get("metadata_only"))
              and int(r["importance"] or 0) >= thr and (r.get("change_label") or "Doc update") in labels
              and wants(sub, [r["product"]])]
    picked.sort(key=lambda r: (LABELS.index(r.get("change_label") or "Doc update"), -int(r["importance"] or 0)))
    anns = [a for a in announcements if a["fetched_at"] > since and a["label"] in labels
            and wants(sub, [p for p in a["products"].split(";") if p])] if sub.get("include_announcements", True) else []
    anns.sort(key=lambda a: (LABELS.index(a["label"]), a["published"]))
    highlights = []
    for product in sorted({r["product"] for r in picked}):
        weeks = sorted(w for w in summaries.get(product, {}) if w in {r["week_start"] for r in picked if r["product"] == product})
        if weeks:
            for b in summaries[product][weeks[-1]]["bullets"][:3]:
                highlights.append({**b, "product": product, "week": weeks[-1]})
    by_product: dict[str, list[dict]] = defaultdict(list)
    for r in picked[:limit]:
        by_product[r["product"]].append(r)
    return {"name": sub.get("name", sub["id"]), "highlights": highlights, "announcements": anns[:limit],
            "changes": dict(sorted(by_product.items())), "more": max(0, len(picked) - limit),
            "site_url": cfg["project"].get("site_url", ""), "title": f"{cfg['project']['name']}: weekly digest",
            "date": utcnow().date().isoformat()}


def is_empty(ctx: dict) -> bool:
    return not (ctx["changes"] or ctx["announcements"])


SILENT = " (no matching announcement)"


def _row_line(r: dict) -> tuple[str, str, str, str]:
    """(label, text, link, silent note) for a doc change."""
    text = f"{r['title'] or r['file_path']}: {r['change_summary']}"
    return r.get("change_label") or "Doc update", text, r["url"] or r["commit_url"], SILENT if r.get("silent") else ""


def render_markdown(ctx: dict) -> str:
    out = [f"# {ctx['title']}", "", f"For {ctx['name']}, {ctx['date']}.", ""]
    if ctx["highlights"]:
        out += ["## Highlights", ""]
        out += [f"- **{b['impact']}** · {b['product']} · [{b.get('label', 'Doc update')}] {b['headline']}: {b['detail']}"
                + "".join(f" [link]({u})" for u in b.get("urls", [])) for b in ctx["highlights"]]
        out += ["", f"_{AI_NOTE}_", ""]
    if ctx["announcements"]:
        out += ["## Announcements", ""]
        out += [f"- **[{a['label']}]** [{a['title']}]({a['url']})" + (f" ({a['products'].replace(';', ', ')})" if a["products"] else "")
                for a in ctx["announcements"]]
        out.append("")
    if ctx["changes"]:
        out += ["## Documentation changes", ""]
        for product, rows in ctx["changes"].items():
            out += [f"### {product}", ""]
            for r in rows:
                label, text, link, note = _row_line(r)
                out.append(f"- **[{label}]** [{text}]({link}){note} · [diff]({r['commit_url']})")
            out.append("")
        if ctx["more"]:
            out += [f"…and {ctx['more']} more on the dashboard.", ""]
    if ctx["site_url"]:
        out += [f"[Open the dashboard]({ctx['site_url']})", ""]
    return "\n".join(out)


def render_html(ctx: dict) -> str:
    e = html.escape
    parts = [f"<h1>{e(ctx['title'])}</h1><p>For {e(ctx['name'])}, {e(ctx['date'])}.</p>"]
    if ctx["highlights"]:
        parts.append("<h2>Highlights</h2><ul>" + "".join(
            f"<li><b>{e(b['impact'])}</b> · {e(b['product'])} · [{e(b.get('label', 'Doc update'))}] <b>{e(b['headline'])}</b>: "
            f"{e(b['detail'])}" + "".join(f' <a href="{e(u)}">link</a>' for u in b.get("urls", [])) + "</li>"
            for b in ctx["highlights"]) + f"</ul><p><i>{e(AI_NOTE)}</i></p>")
    if ctx["announcements"]:
        parts.append("<h2>Announcements</h2><ul>" + "".join(
            f'<li><b>[{e(a["label"])}]</b> <a href="{e(a["url"])}">{e(a["title"])}</a></li>' for a in ctx["announcements"]) + "</ul>")
    if ctx["changes"]:
        parts.append("<h2>Documentation changes</h2>")
        for product, rows in ctx["changes"].items():
            items = []
            for r in rows:
                label, text, link, note = _row_line(r)
                items.append(f'<li><b>[{e(label)}]</b> <a href="{e(link)}">{e(text)}</a>{e(note)} · '
                             f'<a href="{e(r["commit_url"])}">diff</a></li>')
            parts.append(f"<h3>{e(product)}</h3><ul>{''.join(items)}</ul>")
        if ctx["more"]:
            parts.append(f"<p>…and {ctx['more']} more on the dashboard.</p>")
    if ctx["site_url"]:
        parts.append(f'<p><a href="{e(ctx["site_url"])}">Open the dashboard</a></p>')
    return "<!doctype html><html><body style=\"font-family:Segoe UI,sans-serif\">" + "".join(parts) + "</body></html>\n"


def adaptive_card(ctx: dict) -> dict:
    """An Adaptive Card (1.4) for Teams, trimmed to stay under the card size limit."""
    def text(t: str, **kw) -> dict:
        return {"type": "TextBlock", "text": t, "wrap": True, **kw}

    lines = [("Highlights", f"**{b['impact']}** · {b['product']} · {b['headline']}") for b in ctx["highlights"]]
    lines += [("Announcements", f"**[{a['label']}]** [{a['title']}]({a['url']})") for a in ctx["announcements"]]
    for product, rows in ctx["changes"].items():
        for r in rows:
            label, t, link, note = _row_line(r)
            lines.append((f"Documentation changes: {product}", f"**[{label}]** [{t}]({link}){note}"))
    actions = [{"type": "Action.OpenUrl", "title": "Open the dashboard", "url": ctx["site_url"]}] if ctx["site_url"] else []
    while True:
        body = [text(ctx["title"], size="Large", weight="Bolder"), text(f"For {ctx['name']}, {ctx['date']}.", isSubtle=True)]
        section = None
        for heading, line in lines:
            if heading != section:
                body.append(text(heading, weight="Bolder", spacing="Medium"))
                section = heading
            body.append(text(line, spacing="Small"))
        if ctx["highlights"]:
            body.append(text(AI_NOTE, isSubtle=True, size="Small"))
        card = {"type": "AdaptiveCard", "$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
                "version": "1.4", "body": body, "actions": actions}
        if len(json.dumps(card)) <= CARD_LIMIT or not lines:
            return card
        lines = lines[: max(0, len(lines) - max(1, len(lines) // 5))]


def env(name: str) -> str:
    value = os.environ.get(name, "")
    if not value:
        raise NotConfigured(f"environment variable {name} is not set")
    return value


def recipients(sub: dict) -> list[str]:
    to = sub.get("to") or [a.strip() for a in os.environ.get(sub.get("to_env", ""), "").split(",") if a.strip()]
    if not to:
        raise NotConfigured("no recipients: set `to` or the variable named by `to_env`")
    return to


def send(sub: dict, ctx: dict, markdown: str, html_body: str) -> None:
    subject = f"{ctx['title']} ({ctx['date']})"
    channel = sub.get("channel", "teams")
    if channel == "teams":
        payload = {"type": "message", "attachments": [{"contentType": "application/vnd.microsoft.card.adaptive",
                                                       "contentUrl": None, "content": adaptive_card(ctx)}]}
        resp = http_request("POST", env(sub.get("webhook_env", "TEAMS_WEBHOOK_URL")), json=payload, timeout=30)
    elif channel == "email" and sub.get("transport", "logic-app") == "logic-app":
        resp = http_request("POST", env(sub.get("logic_app_env", "LOGIC_APP_URL")), timeout=30,
                            json={"to": recipients(sub), "subject": subject, "html": html_body, "markdown": markdown})
    elif channel == "email" and sub["transport"] == "smtp":
        msg = EmailMessage()
        msg["Subject"], msg["From"], msg["To"] = subject, env("SMTP_FROM"), ", ".join(recipients(sub))
        msg.set_content(markdown)
        msg.add_alternative(html_body, subtype="html")
        with smtplib.SMTP(env("SMTP_HOST"), int(os.environ.get("SMTP_PORT", "587")), timeout=30) as smtp:
            smtp.starttls()
            if os.environ.get("SMTP_USERNAME"):
                smtp.login(os.environ["SMTP_USERNAME"], env("SMTP_PASSWORD"))
            smtp.send_message(msg)
        return
    else:
        raise DeliveryError(f"unknown channel/transport: {channel}/{sub.get('transport')}")
    if resp.status_code >= 300:
        raise DeliveryError(f"HTTP {resp.status_code}: {resp.text[:200]}")


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--send", action="store_true", help="deliver the digests and remember what was sent")
    ap.add_argument("--only", help="a single subscriber id")
    args = ap.parse_args()
    cfg, subs = load_config(), [s for s in load_yaml(SUBSCRIPTIONS_PATH).get("subscribers", []) if s.get("enabled", True)]
    if args.only and args.only not in {s["id"] for s in subs}:
        ap.error(f"--only {args.only}: unknown subscriber id")
    thr = cfg["project"].get("significant_threshold", 30)
    rows, announcements, summaries = load_changes(cfg), load_announcements(), load_summaries(cfg)
    correlate(rows, announcements, thr, cfg["project"].get("correlation_window_days", 14))
    state, now, failed = load_json(DIGEST_STATE_PATH), utcnow().isoformat(), []
    for sub in subs:
        if args.only and sub["id"] != args.only:
            continue
        # A new subscriber starts with the last week rather than the whole history.
        since = state.get(sub["id"]) or (utcnow() - dt.timedelta(days=FIRST_DIGEST_DAYS)).isoformat()
        ctx = build(sub, cfg, rows, announcements, summaries, since)
        if is_empty(ctx):
            print(f"[{sub['id']}] nothing new")
            continue
        markdown, html_body = render_markdown(ctx), render_html(ctx)
        out = DATA_DIR / "digests" / ctx["date"]
        out.mkdir(parents=True, exist_ok=True)
        (out / f"{sub['id']}.md").write_text(markdown, encoding="utf-8")
        (out / f"{sub['id']}.html").write_text(html_body, encoding="utf-8")
        n = sum(len(v) for v in ctx["changes"].values())
        print(f"[{sub['id']}] {n} changes, {len(ctx['announcements'])} announcements -> {out / sub['id']}.md")
        if not args.send:
            continue
        try:
            send(sub, ctx, markdown, html_body)
        except NotConfigured as exc:
            print(f"[{sub['id']}] not sent, delivery not configured: {exc}")
            continue
        except (DeliveryError, OSError, smtplib.SMTPException) as exc:
            print(f"[{sub['id']}] NOT SENT: {exc}")
            failed.append(sub["id"])
            continue
        state[sub["id"]] = now
        print(f"[{sub['id']}] sent by {sub.get('channel', 'teams')}")
    save_json(DIGEST_STATE_PATH, state)
    if failed:
        sys.exit(1)


if __name__ == "__main__":
    main()
