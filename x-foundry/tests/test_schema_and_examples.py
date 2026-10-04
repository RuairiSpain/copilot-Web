from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import jsonschema
import pytest
import yaml

from xfoundry.plan import analyse_file
from xfoundry.schema import SCHEMA_VERSION, SUPPORTED_MAJOR, load_schema
from xfoundry.schema.models import XFoundry

from .conftest import EXAMPLES

ROOT = Path(__file__).resolve().parent.parent
EXAMPLE_FILES = sorted(EXAMPLES.glob("*.yaml"))


def test_schema_is_valid_draft_2020_12():
    jsonschema.Draft202012Validator.check_schema(load_schema())


def test_schema_has_no_session_pool_settings():
    text = json.dumps(load_schema()).lower()
    assert "sessionpool" not in text
    assert "session_pool" not in text


def test_version_is_pinned():
    assert SCHEMA_VERSION == "1.0"
    assert SUPPORTED_MAJOR == 1
    assert "/1.0.0/" in load_schema()["$id"]


def test_every_pydantic_model_field_is_in_the_schema():
    """The Pydantic root and JSON Schema root declare the same keys."""
    schema_keys = set(load_schema()["$defs"]["xFoundry"]["properties"])
    from pydantic.alias_generators import to_camel

    model_keys = {to_camel(n) for n in XFoundry.model_fields}
    assert schema_keys == model_keys


@pytest.mark.parametrize("path", EXAMPLE_FILES, ids=lambda p: p.stem)
def test_examples_are_valid_and_plan(path):
    analysis = analyse_file(path)
    assert analysis.ok, [str(d) for d in analysis.diagnostics]
    assert not [d for d in analysis.diagnostics if d.severity.value == "error"]
    plan = analysis.plan
    position = {n.id: i for i, n in enumerate(plan.nodes)}
    for node in plan.nodes:
        assert all(position[d] < position[node.id] for d in node.depends_on)
    assert sorted(i for layer in plan.layers for i in layer) == sorted(position)


@pytest.mark.parametrize("path", EXAMPLE_FILES, ids=lambda p: p.stem)
def test_examples_plan_is_deterministic(path):
    assert analyse_file(path).plan.to_json() == analyse_file(path).plan.to_json()


def test_published_schemas_are_current():
    result = subprocess.run(
        [sys.executable, str(ROOT / "scripts" / "build_schemas.py"), "--check"],
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0, result.stderr


@pytest.mark.parametrize("path", EXAMPLE_FILES, ids=lambda p: p.stem)
def test_examples_validate_against_the_azure_yaml_wrapper_schema(path):
    wrapper = json.loads((ROOT / "schemas" / "azure-yaml-x-foundry.schema.json").read_text())
    validator = jsonschema.Draft202012Validator(
        wrapper, format_checker=jsonschema.Draft202012Validator.FORMAT_CHECKER
    )
    validator.validate(yaml.safe_load(path.read_text()))


def test_wrapper_requires_x_foundry_but_allows_native_azd_keys():
    wrapper = json.loads((ROOT / "schemas" / "azure-yaml-x-foundry.schema.json").read_text())
    validator = jsonschema.Draft202012Validator(wrapper)
    assert not validator.is_valid({"name": "app", "services": {}})
    doc = yaml.safe_load((EXAMPLES / "standalone-minimal.yaml").read_text())
    doc["services"] = {"api": {"project": "./api", "language": "py", "host": "containerapp"}}
    assert validator.is_valid(doc)
