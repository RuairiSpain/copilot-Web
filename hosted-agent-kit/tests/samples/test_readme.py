"""The README's example application works as written."""

from __future__ import annotations

import re
import sys
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from hosted_agent_kit import Hack
from hosted_agent_kit.testing import DemoFoundry

README = Path(__file__).resolve().parents[2] / "README.md"


def blocks(language: str) -> list[str]:
    return re.findall(rf"```{language}\n(.*?)```", README.read_text(encoding="utf-8"), re.S)


def test_the_readme_application_runs(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    scheduler = next(b for b in blocks("yaml") if b.startswith("agentPool:"))
    app_source = next(b for b in blocks("python") if "from fastapi import FastAPI" in b)
    (tmp_path / "scheduler.yaml").write_text(scheduler)
    (tmp_path / "readme_app.py").write_text(app_source)
    monkeypatch.chdir(tmp_path)
    monkeypatch.syspath_prepend(str(tmp_path))
    original = Hack.from_yaml.__func__  # type: ignore[attr-defined]
    monkeypatch.setattr(
        Hack,
        "from_yaml",
        classmethod(lambda cls, path, **kw: original(cls, path, adapter=DemoFoundry(), **kw)),
    )
    sys.modules.pop("readme_app", None)
    import readme_app  # type: ignore[import-not-found]

    with TestClient(readme_app.app) as client:
        response = client.post("/ask", json={"message": "hello"})
    assert response.status_code == 200
    assert response.json()["output_text"] == "Echo from support-bot: hello"
    sys.modules.pop("readme_app", None)


def test_the_readme_scheduler_yaml_is_valid() -> None:
    from hosted_agent_kit.config.loader import build_config, parse_yaml

    scheduler = next(b for b in blocks("yaml") if b.startswith("agentPool:"))
    config = build_config(parse_yaml(scheduler, "README"))
    assert config.names == ["support-bot", "memory-bot"]
