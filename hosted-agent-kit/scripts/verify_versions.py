"""Verify that the runtime and the Azure SDK match what the service was built and tested against.

Checks the Python version, the pinned ``azure-ai-projects`` version in pyproject.toml, uv.lock and
the installed package, and the SDK surface the adapter calls. Run it in CI and after any upgrade.

Usage:
    uv run python scripts/verify_versions.py [--json]
Exit status is 0 when every check passes and 1 otherwise.
"""

from __future__ import annotations

import argparse
import inspect
import json
import re
import sys
import tomllib
from dataclasses import asdict, dataclass
from importlib import metadata
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PACKAGE = "azure-ai-projects"
MIN_PYTHON = (3, 12)
REQUIRED_SESSION_METHODS = (
    "list_sessions",
    "get_session",
    "create_session",
    "stop_session",
    "delete_session",
)
REQUIRED_STATUSES = {
    "creating",
    "active",
    "idle",
    "updating",
    "failed",
    "deleting",
    "deleted",
    "expired",
}
REQUIRED_SESSION_FIELDS = {"agent_session_id", "version_indicator", "status", "created_at"}


@dataclass
class Check:
    name: str
    ok: bool
    detail: str


def pinned_version(pyproject: Path) -> str | None:
    data = tomllib.loads(pyproject.read_text(encoding="utf-8"))
    for requirement in data["project"]["dependencies"]:
        match = re.fullmatch(rf"{re.escape(PACKAGE)}\s*(?:==|~=)\s*([^\s;]+)", requirement)
        if match:
            return match.group(1)
    return None


def locked_version(lockfile: Path) -> str | None:
    data = tomllib.loads(lockfile.read_text(encoding="utf-8"))
    for package in data.get("package", []):
        if package.get("name") == PACKAGE:
            return str(package.get("version"))
    return None


def check_python() -> Check:
    current = sys.version_info[:2]
    return Check(
        "python",
        current >= MIN_PYTHON,
        f"{current[0]}.{current[1]} (need >= {MIN_PYTHON[0]}.{MIN_PYTHON[1]})",
    )


def check_versions(root: Path) -> list[Check]:
    pinned = pinned_version(root / "pyproject.toml")
    locked = locked_version(root / "uv.lock")
    try:
        installed: str | None = metadata.version(PACKAGE)
    except metadata.PackageNotFoundError:
        installed = None
    agree = pinned is not None and pinned == locked == installed
    return [
        Check(
            "pin",
            pinned is not None,
            f"pyproject requires {PACKAGE} {pinned}" if pinned else f"{PACKAGE} is not pinned",
        ),
        Check(
            "versions agree", agree, f"pyproject={pinned} uv.lock={locked} installed={installed}"
        ),
    ]


def check_sdk_surface() -> list[Check]:
    from azure.ai.projects import models
    from azure.ai.projects.aio import AIProjectClient
    from azure.ai.projects.aio.operations import AgentsOperations

    checks: list[Check] = []
    missing = [
        m for m in REQUIRED_SESSION_METHODS if not callable(getattr(AgentsOperations, m, None))
    ]
    checks.append(
        Check(
            "session operations",
            not missing,
            "all present" if not missing else f"missing: {missing}",
        )
    )

    create = inspect.signature(AgentsOperations.create_session).parameters
    checks.append(
        Check(
            "create_session(version_indicator=)",
            "version_indicator" in create,
            "keyword present" if "version_indicator" in create else "keyword missing",
        )
    )

    init = inspect.signature(AIProjectClient.__init__).parameters
    checks.append(
        Check(
            "AIProjectClient(allow_preview=)",
            "allow_preview" in init,
            "keyword present" if "allow_preview" in init else "keyword missing",
        )
    )

    openai_params = inspect.signature(AIProjectClient.get_openai_client).parameters
    checks.append(
        Check(
            "get_openai_client(agent_name=)",
            "agent_name" in openai_params,
            "keyword present" if "agent_name" in openai_params else "keyword missing",
        )
    )

    statuses = {s.value for s in models.AgentSessionStatus}
    absent = sorted(REQUIRED_STATUSES - statuses)
    extra = sorted(statuses - REQUIRED_STATUSES)
    detail = "all documented statuses present" if not absent else f"missing statuses: {absent}"
    if extra:
        detail += f"; new statuses the adapter maps to UNKNOWN: {extra}"
    checks.append(Check("session statuses", not absent, detail))

    fields = set(getattr(models.AgentSessionResource, "__annotations__", {}))
    lacking = sorted(REQUIRED_SESSION_FIELDS - fields)
    checks.append(
        Check(
            "AgentSessionResource fields",
            not lacking,
            "all present" if not lacking else f"missing: {lacking}",
        )
    )
    checks.append(
        Check(
            "VersionRefIndicator",
            hasattr(models, "VersionRefIndicator"),
            "model present" if hasattr(models, "VersionRefIndicator") else "model missing",
        )
    )
    return checks


def run_checks(root: Path = ROOT) -> list[Check]:
    return [check_python(), *check_versions(root), *check_sdk_surface()]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--json", action="store_true", help="print machine readable output")
    args = parser.parse_args(argv)
    checks = run_checks()
    if args.json:
        print(json.dumps([asdict(c) for c in checks], indent=2))
    else:
        for check in checks:
            print(f"{'ok  ' if check.ok else 'FAIL'}  {check.name:<36} {check.detail}")
    return 0 if all(c.ok for c in checks) else 1


if __name__ == "__main__":
    raise SystemExit(main())
