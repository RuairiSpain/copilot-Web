"""Shared fixtures for repository-level contract tests."""

from __future__ import annotations

from pathlib import Path
from typing import Any

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[1]


@pytest.fixture(scope="session")
def repo_root() -> Path:
    return REPO_ROOT


@pytest.fixture(scope="session")
def azure_yaml_text(repo_root: Path) -> str:
    return (repo_root / "azure.yaml").read_text(encoding="utf-8")


@pytest.fixture(scope="session")
def azure_yaml(azure_yaml_text: str) -> dict[str, Any]:
    loaded = yaml.safe_load(azure_yaml_text)
    assert isinstance(loaded, dict), "azure.yaml must be a mapping"
    return loaded


@pytest.fixture(scope="session")
def services(azure_yaml: dict[str, Any]) -> dict[str, dict[str, Any]]:
    return azure_yaml["services"]
