"""The operational scripts: version verification and the load generator."""

from __future__ import annotations

import importlib.util
import json
import sys
from pathlib import Path
from types import ModuleType
from typing import Any

import httpx
import pytest

from tests.integration.conftest import start_api

SCRIPTS = Path(__file__).resolve().parents[2] / "scripts"


def load(name: str) -> ModuleType:
    spec = importlib.util.spec_from_file_location(name, SCRIPTS / f"{name}.py")
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


verify = load("verify_versions")
load_test = load("load_test")
live_canary: Any = load("live_canary")


def test_every_version_check_passes_against_the_installed_sdk() -> None:
    checks = verify.run_checks()
    assert checks and all(c.ok for c in checks), [c for c in checks if not c.ok]


def test_main_reports_success_in_text_and_json(capsys: pytest.CaptureFixture[str]) -> None:
    assert verify.main([]) == 0
    assert "ok" in capsys.readouterr().out
    assert verify.main(["--json"]) == 0
    assert all(item["ok"] for item in json.loads(capsys.readouterr().out))


def test_a_mismatched_lock_file_fails_the_check(tmp_path: Path) -> None:
    (tmp_path / "pyproject.toml").write_text(
        '[project]\ndependencies = ["azure-ai-projects==2.7.0"]\n', encoding="utf-8"
    )
    (tmp_path / "uv.lock").write_text(
        '[[package]]\nname = "azure-ai-projects"\nversion = "2.6.0"\n', encoding="utf-8"
    )
    pin, agree = verify.check_versions(tmp_path)
    assert pin.ok and not agree.ok and "uv.lock=2.6.0" in agree.detail


def test_an_unpinned_dependency_fails_the_pin_check(tmp_path: Path) -> None:
    (tmp_path / "pyproject.toml").write_text(
        '[project]\ndependencies = ["azure-ai-projects>=2"]\n', encoding="utf-8"
    )
    (tmp_path / "uv.lock").write_text("", encoding="utf-8")
    pin, agree = verify.check_versions(tmp_path)
    assert not pin.ok and not agree.ok


def test_missing_lock_entry_is_reported(tmp_path: Path) -> None:
    (tmp_path / "uv.lock").write_text(
        '[[package]]\nname = "other"\nversion = "1"\n', encoding="utf-8"
    )
    assert verify.locked_version(tmp_path / "uv.lock") is None


def test_new_sdk_statuses_are_flagged_but_missing_ones_fail(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from azure.ai.projects import models

    class Status:
        def __init__(self, value: str) -> None:
            self.value = value

    monkeypatch.setattr(models, "AgentSessionStatus", [Status("active"), Status("hibernating")])
    status = next(c for c in verify.check_sdk_surface() if c.name == "session statuses")
    assert not status.ok and "missing statuses" in status.detail and "hibernating" in status.detail


def test_new_statuses_alone_do_not_fail(monkeypatch: pytest.MonkeyPatch) -> None:
    from azure.ai.projects import models

    class Status:
        def __init__(self, value: str) -> None:
            self.value = value

    values = [*verify.REQUIRED_STATUSES, "hibernating"]
    monkeypatch.setattr(models, "AgentSessionStatus", [Status(v) for v in values])
    status = next(c for c in verify.check_sdk_surface() if c.name == "session statuses")
    assert status.ok and "hibernating" in status.detail


def test_old_python_fails(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(verify.sys, "version_info", (3, 11, 9, "final", 0))
    assert not verify.check_python().ok


def test_main_returns_one_when_any_check_fails(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    monkeypatch.setattr(verify, "run_checks", lambda: [verify.Check("x", False, "broken")])
    assert verify.main([]) == 1
    assert "FAIL" in capsys.readouterr().out


# ----------------------------------------------------------------- load generator


async def test_load_generator_reports_latency_and_outcomes_against_the_app() -> None:
    api, manager = await start_api(
        agents={"research-agent": {"mode": "stateless", "max_sessions": 4}}, defaults={}
    )
    try:
        report = await load_test.run_load(
            api.client, agent="research-agent", requests=30, concurrency=10, users=5
        )
        data = report.as_dict()
        assert data["statuses"] == {"200": 30} and report.throughput > 0
        assert set(data["latency_seconds"]) == {"mean", "p50", "p95", "p99", "max"}  # type: ignore[arg-type]
        streamed = await load_test.run_load(
            api.client, agent="research-agent", requests=3, concurrency=3, users=1, stream=True
        )
        assert streamed.statuses["200"] == 3
    finally:
        await api.client.aclose()
        await manager.__aexit__(None, None, None)


async def test_load_generator_records_error_codes_and_transport_failures() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        if "boom" in request.url.path:
            raise httpx.ConnectError("refused")
        if "html" in request.url.path:
            return httpx.Response(502, text="<html>bad gateway</html>")
        return httpx.Response(429, json={"error_code": "QUEUE_FULL"})

    async with httpx.AsyncClient(
        transport=httpx.MockTransport(handler), base_url="http://t"
    ) as client:
        full = await load_test.run_load(
            client, agent="a", requests=4, concurrency=2, users=2, token="t"
        )
        assert full.error_codes == {"QUEUE_FULL": 4}
        broken = await load_test.run_load(client, agent="boom", requests=2, concurrency=1, users=1)
        assert broken.statuses == {"ConnectError": 2} and not broken.latencies
        html = await load_test.run_load(client, agent="html", requests=1, concurrency=1, users=1)
        assert html.error_codes == {"non-json": 1}


def test_empty_report_has_zero_statistics() -> None:
    report = load_test.Report(requests=0, concurrency=1, elapsed_seconds=0.0)
    data = report.as_dict()
    assert report.throughput == 0.0 and report.percentile(0.5) == 0.0
    assert data["latency_seconds"]["mean"] == 0.0  # type: ignore[index]


def test_main_runs_end_to_end_and_signals_failure(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    transport = httpx.MockTransport(lambda r: httpx.Response(200, json={"result": {}}))
    real = httpx.AsyncClient
    monkeypatch.setattr(
        load_test.httpx, "AsyncClient", lambda **kw: real(transport=transport, **kw)
    )
    assert load_test.main(["--agent", "a", "--requests", "5", "--concurrency", "2"]) == 0
    assert json.loads(capsys.readouterr().out)["statuses"] == {"200": 5}
    failing = httpx.MockTransport(lambda r: httpx.Response(503, json={"error_code": "X"}))
    monkeypatch.setattr(load_test.httpx, "AsyncClient", lambda **kw: real(transport=failing, **kw))
    assert load_test.main(["--agent", "a", "--requests", "2", "--users", "0"]) == 1


# ------------------------------------------------------------------ live canary


async def test_the_canary_passes_against_the_fake_and_cleans_up() -> None:

    kit = live_canary.fake_kit("canary-agent")
    async with kit:
        steps = await live_canary.run(kit, "canary-agent")
        assert [s.name for s in steps] == [
            "first call",
            "session is tracked",
            "stop then resume",
            "sync",
            "delete",
        ]
        assert all(s.ok for s in steps), [s for s in steps if not s.ok]
        assert not [s for s in await kit.reporting.sessions("canary-agent") if s.leased]


async def test_the_canary_reports_a_quota_error_when_the_platform_gives_one() -> None:

    from hosted_agent_kit.domain.errors import QUOTA_SESSION, FoundryQuotaExceeded
    from hosted_agent_kit.domain.models import FoundrySession
    from hosted_agent_kit.testing import DemoFoundry

    class LowQuota(DemoFoundry):
        async def create_session(
            self, agent_name: str, session_id: str | None = None, agent_version: str | None = None
        ) -> FoundrySession:
            if len(self.sessions) >= 3:
                raise FoundryQuotaExceeded(QUOTA_SESSION)
            return await super().create_session(agent_name, session_id, agent_version)

    kit = live_canary.fake_kit("canary-agent")
    kit._adapter = LowQuota()
    async with kit:
        steps = await live_canary.run(kit, "canary-agent", quota_probe=6)
    probe = next(s for s in steps if "quota error" in s.name)
    assert probe.ok and "SessionQuotaError" in probe.detail and "3 open" in probe.detail
    assert steps[-1].name == "delete" and steps[-1].ok


async def test_the_canary_flags_a_quota_that_is_too_high_for_the_probe() -> None:

    kit = live_canary.fake_kit("canary-agent")
    async with kit:
        steps = await live_canary.run(kit, "canary-agent", quota_probe=3)
    probe = next(s for s in steps if "quota error" in s.name)
    assert not probe.ok and "quota is too high" in probe.detail


async def test_the_canary_fails_loudly_when_a_step_breaks() -> None:

    from hosted_agent_kit.domain.errors import FoundryUnavailable

    kit = live_canary.fake_kit("canary-agent")
    async with kit:
        kit.runtime.adapter._inner.invoke_errors = [FoundryUnavailable()] * 10  # type: ignore[attr-defined]
        steps = await live_canary.run(kit, "canary-agent")
    assert (not steps[0].ok and "FoundryUnavailable" in steps[0].detail) or not steps[0].ok
    assert steps[-1].name == "delete"  # cleanup still ran


async def test_the_canary_main_prints_and_sets_the_exit_status(
    capsys: pytest.CaptureFixture[str],
) -> None:

    assert await live_canary.main(["--fake"]) == 0
    assert "PASS  first call" in capsys.readouterr().out
    assert await live_canary.main(["--fake", "--json"]) == 0
    assert json.loads(capsys.readouterr().out)[0]["ok"] is True
