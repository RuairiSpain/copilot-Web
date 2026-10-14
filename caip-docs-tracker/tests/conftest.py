"""Shared fixtures: an isolated project tree and a fake HTTP layer."""
from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from tracker import (  # noqa: E402
    announcements, build_site, check_urls, common, digest, discover, scan, store, summarise,
)

MODULES = [common, scan, summarise, build_site, store, announcements, digest, discover, check_urls]
PATHS = {
    "CONFIG_PATH": "config/sources.yml", "DISCOVERED_PATH": "config/discovered.yml",
    "SUBSCRIPTIONS_PATH": "config/subscriptions.yml", "STATE_PATH": "state/state.json",
    "FEED_STATE_PATH": "state/feeds.json", "DIGEST_STATE_PATH": "state/digests.json",
    "DATA_DIR": "data", "SITE_DIR": "site", "CACHE_DIR": "cache",
}


class FakeResponse:
    def __init__(self, status: int = 200, body: object = "", headers: dict | None = None):
        self.status_code = status
        self.headers = headers or {}
        self._body = body
        self.text = body if isinstance(body, str) else json.dumps(body)

    def json(self):
        return self._body if not isinstance(self._body, str) else json.loads(self._body)

    def raise_for_status(self):
        if self.status_code >= 400:
            import requests
            raise requests.HTTPError(f"HTTP {self.status_code}")


class FakeHTTP:
    """Routes requests.request(method, url, ...) to canned responses; records every call."""

    def __init__(self):
        self.routes: list[tuple[str, str, object]] = []
        self.calls: list[dict] = []

    def add(self, method: str, url_prefix: str, *responses):
        """Responses are returned in order for matching calls (the last one repeats); a callable gets the call."""
        self.routes.append((method, url_prefix, list(responses)))

    def __call__(self, method, url, **kwargs):
        call = {"method": method, "url": url, **kwargs}
        self.calls.append(call)
        for m, prefix, responses in self.routes:
            if m == method and url.startswith(prefix):
                r = responses.pop(0) if len(responses) > 1 else responses[0]
                if isinstance(r, Exception):
                    raise r
                return r(call) if callable(r) else r
        return FakeResponse(404, "not found")


@pytest.fixture
def http(monkeypatch):
    fake = FakeHTTP()
    monkeypatch.setattr(common.requests, "request", fake)
    monkeypatch.setattr(common.time, "sleep", lambda s: None)
    return fake


@pytest.fixture
def project(tmp_path, monkeypatch):
    """A throwaway project tree with the real config; every module's paths point into it."""
    (tmp_path / "config").mkdir()
    (tmp_path / "config" / "sources.yml").write_text((ROOT / "config" / "sources.yml").read_text())
    (tmp_path / "config" / "subscriptions.yml").write_text((ROOT / "config" / "subscriptions.yml").read_text())
    for mod in MODULES:
        for name, rel in PATHS.items():
            if hasattr(mod, name):
                monkeypatch.setattr(mod, name, tmp_path / rel)
    for var in ("GITHUB_TOKEN", "AZURE_OPENAI_ENDPOINT", "AZURE_OPENAI_API_KEY", "AZURE_OPENAI_DEPLOYMENT",
                "TEAMS_WEBHOOK_URL", "LOGIC_APP_URL", "DIGEST_PHASE0_TO", "SMTP_HOST", "SMTP_FROM",
                "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_PORT", "TRACKER_LARGE_EDITS", "TRACKER_LARGE_EDIT_LINES"):
        monkeypatch.delenv(var, raising=False)
    return tmp_path


@pytest.fixture
def fake_repo(tmp_path):
    repo = tmp_path / "fake-repo"
    subprocess.run(["bash", str(ROOT / "tests" / "make_test_repo.sh"), str(repo)], check=True, capture_output=True)
    return repo


def set_config(project: Path, mutate) -> dict:
    """Edit the project's sources.yml in place with `mutate(cfg)`."""
    import yaml
    path = project / "config" / "sources.yml"
    cfg = yaml.safe_load(path.read_text())
    mutate(cfg)
    path.write_text(yaml.safe_dump(cfg, sort_keys=False))
    return cfg


def run_main(module, monkeypatch, *argv: str) -> int:
    """Run module.main() with argv; return its exit code (0 when it returns normally)."""
    monkeypatch.setattr(sys, "argv", [module.__name__, *argv])
    try:
        module.main()
    except SystemExit as exc:
        return exc.code if isinstance(exc.code, int) else 1
    return 0
