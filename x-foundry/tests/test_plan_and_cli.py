from __future__ import annotations

import json

import pytest

from xfoundry.cli import main
from xfoundry.diagnostics import Diagnostic, Severity, ValidationFailed, errors_only, warning
from xfoundry.parser import parse_mapping
from xfoundry.plan import (
    DeploymentPlan,
    analyse_file,
    analyse_mapping,
    analyse_parsed,
    analyse_text,
    build_plan,
)

from .conftest import EXAMPLES, mk


def test_build_plan_accepts_paths_text_and_mappings():
    from_path = build_plan(EXAMPLES / "standalone-minimal.yaml")
    from_str_path = build_plan(str(EXAMPLES / "standalone-minimal.yaml"))
    from_text = build_plan((EXAMPLES / "standalone-minimal.yaml").read_text())
    from_mapping = build_plan(mk(projects=[{"name": "finance"}]))
    assert isinstance(from_path, DeploymentPlan)
    assert from_path.order == from_str_path.order == from_text.order
    assert from_mapping.config.projects[0].name == "finance"


def test_build_plan_raises_with_all_diagnostics():
    with pytest.raises(ValidationFailed) as info:
        build_plan(mk(projects=[{"name": "aa"}, {"name": "aa"}], defaults={"location": "atlantis"}))
    assert {d.code for d in info.value.diagnostics} == {"XF001", "XF023"}


def test_analysis_stops_at_the_first_failing_phase():
    schema_bad = analyse_text("x-foundry: {}")
    assert not schema_bad.ok and {d.code for d in schema_bad.diagnostics} == {"XF102"}
    declared_bad = analyse_mapping(
        mk(
            projects=[{"name": "aa"}, {"name": "aa"}],
            iq={
                "knowledgeBases": [
                    {
                        "name": "kk",
                        "sources": [{"name": "ss", "type": "web", "url": "https://x.example"}],
                    }
                ],
            },
            search={"enabled": False},
        )
    )
    assert {d.code for d in declared_bad.diagnostics} == {"XF001"}
    normalise_bad = analyse_mapping(
        mk(
            search={"enabled": False},
            iq={
                "knowledgeBases": [
                    {
                        "name": "kk",
                        "sources": [{"name": "ss", "type": "web", "url": "https://x.example"}],
                    }
                ]
            },
        )
    )
    assert {d.code for d in normalise_bad.diagnostics} == {"XF020"}
    effective_bad = analyse_mapping(mk(models={"default": "nope"}))
    assert {d.code for d in effective_bad.diagnostics} == {"XF006"}


def test_analyse_file_missing(tmp_path):
    result = analyse_file(tmp_path / "missing.yaml")
    assert not result.ok and result.diagnostics[0].code == "XF100"
    with pytest.raises(ValidationFailed):
        result.raise_for_errors()


def test_warnings_travel_with_the_plan():
    plan = analyse_mapping(mk(security={"roles": {"admins": []}})).plan
    assert [w.code for w in plan.warnings] == ["XF114"]
    assert plan.warnings[0].severity == "warning"


def test_analyse_parsed_returns_all_diagnostics():
    result = analyse_parsed(parse_mapping(mk()))
    assert result.ok and result.diagnostics == []


def test_plan_json_is_stable_camel_case_and_round_trips():
    plan = build_plan(EXAMPLES / "hub-spoke.yaml")
    text = plan.to_json()
    data = json.loads(text)
    assert data["schemaVersion"] == "1.0"
    assert data["config"]["topologyMode"] == "hub-spoke"
    assert [n["id"] for n in data["nodes"]] == plan.order
    assert DeploymentPlan.model_validate_json(text).order == plan.order
    assert plan.to_json(indent=None) == json.dumps(
        json.loads(plan.to_json(indent=None)), separators=(",", ":")
    )


def test_plan_node_lookup():
    plan = build_plan(EXAMPLES / "standalone-minimal.yaml")
    assert plan.node("resource-group").depends_on == []
    with pytest.raises(StopIteration):
        plan.node("ghost")


def test_diagnostic_helpers():
    d = warning("XF999", "careful", "x-foundry.a")
    assert str(d) == "warning XF999 at x-foundry.a: careful"
    assert str(Diagnostic("XF1", "m")) == "error XF1: m"
    assert d.to_dict()["severity"] == "warning"
    assert errors_only([d, Diagnostic("XF1", "m")]) == [Diagnostic("XF1", "m", "", Severity.ERROR)]


# CLI -----------------------------------------------------------------------------------------------


def test_cli_validate_ok(capsys):
    assert main(["validate", str(EXAMPLES / "standalone-minimal.yaml")]) == 0
    assert "valid (" in capsys.readouterr().out


def test_cli_validate_reports_errors(tmp_path, capsys):
    path = tmp_path / "azure.yaml"
    path.write_text("x-foundry:\n  topology: {mode: standalone}\n")
    assert main(["validate", str(path)]) == 1
    assert "XF102" in capsys.readouterr().err


def test_cli_validate_json(tmp_path, capsys):
    path = tmp_path / "azure.yaml"
    path.write_text("name: x\n")
    assert main(["validate", str(path), "--json"]) == 1
    assert json.loads(capsys.readouterr().out)[0]["code"] == "XF101"


def test_cli_validate_prints_warnings(tmp_path, capsys):
    path = tmp_path / "azure.yaml"
    path.write_text(
        "x-foundry:\n  topology: {mode: standalone}\n  security: {roles: {admins: []}}\n  projects: [{name: finance}]\n"
    )
    assert main(["validate", str(path)]) == 0
    captured = capsys.readouterr()
    assert "XF114" in captured.err and "1 warning(s)" in captured.out
    assert main(["validate", str(path), "--json"]) == 0
    assert json.loads(capsys.readouterr().out)[0]["code"] == "XF114"


def test_cli_plan_text_and_json(capsys):
    assert main(["plan", str(EXAMPLES / "hub-spoke.yaml")]) == 0
    out = capsys.readouterr().out
    assert out.startswith("step 1: resource-group")
    assert main(["plan", str(EXAMPLES / "hub-spoke.yaml"), "--json"]) == 0
    assert json.loads(capsys.readouterr().out)["nodes"][0]["id"] == "resource-group"


def test_cli_schema(capsys):
    assert main(["schema"]) == 0
    assert json.loads(capsys.readouterr().out)["$defs"]["xFoundry"]


def test_cli_requires_a_command():
    with pytest.raises(SystemExit) as info:
        main([])
    assert info.value.code == 2


def test_module_entry_point(tmp_path):
    import subprocess
    import sys

    result = subprocess.run(
        [sys.executable, "-m", "xfoundry", "validate", str(EXAMPLES / "standalone-minimal.yaml")],
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0 and "valid" in result.stdout
