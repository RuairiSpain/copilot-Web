"""Shared helpers: config, paths, state, CSV I/O, week maths, product mapping and HTTP."""
from __future__ import annotations

import csv
import datetime as dt
import fnmatch
import json
import os
import re
import time
from pathlib import Path

import requests
import yaml

ROOT = Path(__file__).resolve().parent.parent
CONFIG_PATH = ROOT / "config" / "sources.yml"
DISCOVERED_PATH = ROOT / "config" / "discovered.yml"
SUBSCRIPTIONS_PATH = ROOT / "config" / "subscriptions.yml"
STATE_PATH = ROOT / "state" / "state.json"
FEED_STATE_PATH = ROOT / "state" / "feeds.json"
DIGEST_STATE_PATH = ROOT / "state" / "digests.json"
DATA_DIR = ROOT / "data"
SITE_DIR = ROOT / "site"

CHANGE_FIELDS = [
    "scan_id", "repo_id", "week_start", "commit_date", "commit_sha", "commit_url",
    "pr_number", "author", "commit_subject", "change_summary", "change_type",
    "file_path", "old_path", "product", "title", "description", "url",
    "ms_service", "ms_subservice", "ms_topic", "ms_date", "ms_author",
    "lines_added", "lines_removed", "metadata_only", "tags", "importance", "key_lines",
    "noise", "change_label",
]
COMMIT_FIELDS = [
    "scan_id", "repo_id", "commit_sha", "parents", "commit_date", "author",
    "subject", "files_changed", "tracked_files_changed",
]
SCAN_FIELDS = [
    "scan_id", "repo_id", "run_at", "branch", "from_sha", "to_sha", "since",
    "commits", "tracked_changes", "significant_changes", "status",
]
ANNOUNCEMENT_FIELDS = [
    "feed_id", "item_id", "published", "week_start", "fetched_at", "title", "url",
    "status", "label", "products", "categories", "summary",
]
# Change labels from the design document, most important first.
LABELS = ["Breaking change", "Retirement", "GA", "Preview", "New", "Doc update"]


# ---------------------------------------------------------------- config and state
def load_yaml(path: Path) -> dict:
    if not path.exists():
        return {}
    with open(path, encoding="utf-8") as fh:
        return yaml.safe_load(fh) or {}


def load_config() -> dict:
    return load_yaml(CONFIG_PATH)


def enabled_sources(cfg: dict) -> list[dict]:
    """Configured sources that are switched on, plus discovered sources that are switched on."""
    sources = list(cfg.get("sources", []))
    known = {s["id"] for s in sources}
    sources += [s for s in load_yaml(DISCOVERED_PATH).get("sources", []) if s["id"] not in known]
    return [s for s in sources if s.get("enabled", True)]


def load_json(path: Path) -> dict:
    if path.exists():
        return json.loads(path.read_text(encoding="utf-8") or "{}")
    return {}


def save_json(path: Path, data: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def load_state() -> dict:
    return load_json(STATE_PATH)


def save_state(state: dict) -> None:
    save_json(STATE_PATH, state)


# ---------------------------------------------------------------- dates
def utcnow() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0)


def parse_iso(value: str) -> dt.datetime:
    d = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    return d if d.tzinfo else d.replace(tzinfo=dt.timezone.utc)


def week_start(value: dt.datetime | dt.date | str) -> str:
    """ISO week bucket in UTC, labelled by its Monday (YYYY-MM-DD)."""
    if isinstance(value, str):
        value = parse_iso(value) if "T" in value else dt.date.fromisoformat(value)
    if isinstance(value, dt.datetime):
        # Commit dates carry the committer's offset; bucket them in UTC like the start-date filter.
        value = value.astimezone(dt.timezone.utc).date()
    return (value - dt.timedelta(days=value.weekday())).isoformat()


def weeks_between(start: str, end: str) -> list[str]:
    s = dt.date.fromisoformat(week_start(start))
    e = dt.date.fromisoformat(week_start(end))
    out = []
    while s <= e:
        out.append(s.isoformat())
        s += dt.timedelta(days=7)
    return out


# ---------------------------------------------------------------- paths and products
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


ACRONYMS = {"ai", "api", "aks", "cdn", "cli", "ddos", "dns", "hpc", "iot", "ml", "mcp", "nat", "sap", "sdk",
            "sql", "ssh", "vm", "vms", "vpn", "waf"}


def humanize(name: str) -> str:
    """'api-management' -> 'API Management'."""
    words = re.split(r"[-_\s]+", name.strip("-_ "))
    return " ".join(w.upper() if w.lower() in ACRONYMS else w.capitalize() for w in words if w)


def resolve_product(path: str, src: dict, ms_service: str = "", cfg: dict | None = None) -> str:
    """Map a changed file to a product.

    Order: the source's explicit path prefixes, then the page's ms.service (config
    `ms_service_products`), then the folder name under `folder_products.prefix`, then the default.
    """
    product, rule = first_prefix(path, src.get("products", []), "product")
    if rule:
        return product
    by_service = (cfg or {}).get("ms_service_products") or {}
    if ms_service and ms_service in by_service:
        return by_service[ms_service]
    fp = src.get("folder_products")
    if fp and path.startswith(fp.get("prefix", "")):
        rest = path[len(fp.get("prefix", "")):]
        if "/" in rest:  # a file directly under the prefix has no folder to name it by
            folder = rest.split("/")[0]
            return (fp.get("names") or {}).get(folder) or fp.get("name_prefix", "") + humanize(folder)
    return src.get("default_product", "Other")


def product_aliases(cfg: dict) -> dict[str, str]:
    """Lower-cased alias -> canonical product name, including each canonical name itself."""
    out: dict[str, str] = {}
    for product, aliases in (cfg.get("product_aliases") or {}).items():
        for name in [product, *(aliases or [])]:
            out[name.lower()] = product
    return out


def products_matching(aliases: dict[str, str], categories: list[str], text: str = "") -> list[str]:
    """Canonical products named by feed categories, or by an alias phrase in the text (see product_aliases)."""
    found = [aliases[c.strip().lower()] for c in categories if c.strip().lower() in aliases]
    low = text.lower()
    for alias, product in aliases.items():
        if re.search(rf"(?<![\w-]){re.escape(alias)}(?![\w-])", low):
            found.append(product)
    return list(dict.fromkeys(found))


def slug(name: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-") or "x"


# ---------------------------------------------------------------- CSV
def append_csv(path: Path, fields: list[str], rows: list[dict]) -> None:
    if not rows:
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists():
        with open(path, newline="", encoding="utf-8") as fh:
            header = next(csv.reader(fh), [])
        if header != fields:
            # The file was written with an older column list: rewrite it with the current one.
            old = read_csv(path)
            write_csv(path, fields, old)
    new = not path.exists()
    with open(path, "a", newline="", encoding="utf-8") as fh:
        w = csv.DictWriter(fh, fieldnames=fields, extrasaction="ignore")
        if new:
            w.writeheader()
        w.writerows(rows)


def write_csv(path: Path, fields: list[str], rows: list[dict]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with open(path, "w", newline="", encoding="utf-8") as fh:
        w = csv.DictWriter(fh, fieldnames=fields, extrasaction="ignore", restval="")
        w.writeheader()
        w.writerows(rows)


def read_csv(path: Path) -> list[dict]:
    if not path.exists():
        return []
    with open(path, newline="", encoding="utf-8") as fh:
        return list(csv.DictReader(fh))


def is_true(value: object) -> bool:
    return str(value).lower() in {"1", "true", "yes"}


def env_flag(name: str) -> bool:
    return is_true(os.environ.get(name, ""))


# ---------------------------------------------------------------- HTTP
USER_AGENT = "caip-docs-tracker (+https://github.com/RuairiSpain/copilot-Web)"


def http_request(method: str, url: str, *, attempts: int = 3, timeout: float = 60, **kwargs) -> requests.Response:
    """Send a request, waiting and retrying on rate limits (429) and server errors (5xx)."""
    headers = {"User-Agent": USER_AGENT, **(kwargs.pop("headers", None) or {})}
    for i in range(attempts - 1):
        resp = requests.request(method, url, headers=headers, timeout=timeout, **kwargs)
        if resp.status_code != 429 and resp.status_code < 500:
            return resp
        try:
            wait = min(60.0, float(resp.headers.get("Retry-After", "")))
        except ValueError:
            wait = 5.0 * 2 ** i
        print(f"  HTTP {resp.status_code} from {url}; retrying in {wait:.0f}s")
        time.sleep(wait)
    return requests.request(method, url, headers=headers, timeout=timeout, **kwargs)


def github_headers() -> dict:
    """Headers for the GitHub API; authenticated (5,000 requests/hour) when GITHUB_TOKEN is set."""
    h = {"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"}
    token = os.environ.get("GITHUB_TOKEN")
    if token:
        h["Authorization"] = f"Bearer {token}"
    return h
