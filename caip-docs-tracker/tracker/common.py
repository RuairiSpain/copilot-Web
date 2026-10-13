"""Shared helpers: config, paths, CSV I/O, week maths."""
from __future__ import annotations

import csv
import datetime as dt
import fnmatch
import json
import os
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parent.parent
CONFIG_PATH = ROOT / "config" / "sources.yml"
STATE_PATH = ROOT / "state" / "state.json"
DATA_DIR = ROOT / "data"
SITE_DIR = ROOT / "site"

CHANGE_FIELDS = [
    "scan_id", "repo_id", "week_start", "commit_date", "commit_sha", "commit_url",
    "pr_number", "author", "commit_subject", "change_summary", "change_type",
    "file_path", "old_path", "product", "title", "description", "url",
    "ms_service", "ms_subservice", "ms_topic", "ms_date", "ms_author",
    "lines_added", "lines_removed", "metadata_only", "tags", "importance", "key_lines",
]
COMMIT_FIELDS = [
    "scan_id", "repo_id", "commit_sha", "parents", "commit_date", "author",
    "subject", "files_changed", "tracked_files_changed",
]
SCAN_FIELDS = [
    "scan_id", "repo_id", "run_at", "branch", "from_sha", "to_sha", "since",
    "commits", "tracked_changes", "significant_changes", "status",
]


def load_config() -> dict:
    with open(CONFIG_PATH, encoding="utf-8") as fh:
        return yaml.safe_load(fh)


def load_state() -> dict:
    if STATE_PATH.exists():
        return json.loads(STATE_PATH.read_text(encoding="utf-8") or "{}")
    return {}


def save_state(state: dict) -> None:
    STATE_PATH.parent.mkdir(parents=True, exist_ok=True)
    STATE_PATH.write_text(json.dumps(state, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def utcnow() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0)


def parse_iso(value: str) -> dt.datetime:
    return dt.datetime.fromisoformat(value.replace("Z", "+00:00"))


def week_start(value: dt.datetime | dt.date | str) -> str:
    """ISO week bucket, labelled by its Monday (YYYY-MM-DD)."""
    if isinstance(value, str):
        value = parse_iso(value) if "T" in value else dt.date.fromisoformat(value)
    d = value.date() if isinstance(value, dt.datetime) else value
    return (d - dt.timedelta(days=d.weekday())).isoformat()


def weeks_between(start: str, end: str) -> list[str]:
    s = dt.date.fromisoformat(week_start(start))
    e = dt.date.fromisoformat(week_start(end))
    out = []
    while s <= e:
        out.append(s.isoformat())
        s += dt.timedelta(days=7)
    return out


def path_matches(path: str, patterns: list[str]) -> bool:
    for pat in patterns or []:
        if fnmatch.fnmatch(path, pat):
            return True
        # allow "dir/**/x" to also match "dir/x"
        if "/**/" in pat and fnmatch.fnmatch(path, pat.replace("/**/", "/")):
            return True
    return False


def first_prefix(path: str, rules: list[dict], key: str, default: str = "") -> tuple[str, dict | None]:
    for rule in rules or []:
        if path.startswith(rule["prefix"]):
            return rule[key], rule
    return default, None


def append_csv(path: Path, fields: list[str], rows: list[dict]) -> None:
    if not rows:
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    new = not path.exists()
    with open(path, "a", newline="", encoding="utf-8") as fh:
        w = csv.DictWriter(fh, fieldnames=fields, extrasaction="ignore")
        if new:
            w.writeheader()
        for r in rows:
            w.writerow(r)


def read_csv(path: Path) -> list[dict]:
    if not path.exists():
        return []
    with open(path, newline="", encoding="utf-8") as fh:
        return list(csv.DictReader(fh))


def env_flag(name: str) -> bool:
    return os.environ.get(name, "").lower() in {"1", "true", "yes"}
