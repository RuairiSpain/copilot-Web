"""The intake gate: refuses to start a build until the brief is complete and unambiguous.

The gate is code, not a prompt. A model that wants to skip the interview cannot, because
the tool that starts the build calls :func:`evaluate_brief` and stops on any issue.
"""

from __future__ import annotations

import json
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Any

from pydantic import ValidationError

from intake.brief import TESTS_PATTERN, VAGUE_PATTERN, ProjectBrief


@dataclass(frozen=True, slots=True)
class GateResult:
    """Outcome of checking a brief."""

    ok: bool
    issues: tuple[str, ...] = ()
    brief: ProjectBrief | None = None

    def render(self) -> str:
        """Text for the model: what to ask the user next."""
        if self.ok:
            return "The brief is complete. Read it back to the user and ask for confirmation."
        bullet_list = "\n".join(f"- {issue}" for issue in self.issues)
        return (
            "The brief is NOT ready. Ask the user about each point below, update the brief "
            f"with their answers (never invent them), then validate again:\n{bullet_list}"
        )


def _format_validation_error(error: ValidationError) -> list[str]:
    issues = []
    for item in error.errors():
        where = ".".join(str(part) for part in item["loc"]) or "brief"
        issues.append(f"{where}: {item['msg']}")
    return issues


def _vague(label: str, text: str) -> str | None:
    match = VAGUE_PATTERN.search(text)
    if match:
        return f"{label} is vague (contains {match.group(0)!r}). Ask for the specific detail."
    return None


def check_brief(brief: ProjectBrief) -> list[str]:
    """Return every reason the brief is not ready. An empty list means it is ready."""
    issues: list[str] = []

    texts: list[tuple[str, str]] = [
        ("summary", brief.summary),
        ("target_audience", brief.target_audience),
    ]
    texts += [
        (f"functional_requirements[{i}]", t) for i, t in enumerate(brief.functional_requirements)
    ]
    texts += [(f"out_of_scope[{i}]", t) for i, t in enumerate(brief.out_of_scope)]
    texts += [(f"definition_of_done[{i}]", t) for i, t in enumerate(brief.definition_of_done)]
    texts += [(f"dependencies[{i}].purpose", d.purpose) for i, d in enumerate(brief.dependencies)]
    issues += [issue for label, text in texts if (issue := _vague(label, text))]

    for i, requirement in enumerate(brief.functional_requirements):
        if len(requirement) < 15:
            issues.append(f"functional_requirements[{i}] is too short to be testable.")
    if len({r.lower() for r in brief.functional_requirements}) != len(
        brief.functional_requirements
    ):
        issues.append("functional_requirements contains duplicates.")

    if not brief.dependencies and not brief.no_dependencies_confirmed:
        issues.append(
            "dependencies is empty. Ask which libraries and other services are needed. Set "
            "no_dependencies_confirmed to true only if the user says there are none."
        )
    if brief.dependencies and brief.no_dependencies_confirmed:
        issues.append("no_dependencies_confirmed is true but dependencies are listed.")

    if not any(TESTS_PATTERN.search(item) for item in brief.definition_of_done):
        issues.append("definition_of_done must include a condition about the automated tests.")

    return issues


def evaluate_brief(raw: str | Mapping[str, Any]) -> GateResult:
    """Parse and check a brief supplied as JSON text or a mapping."""
    try:
        data = json.loads(raw) if isinstance(raw, str) else dict(raw)
    except json.JSONDecodeError as exc:
        return GateResult(False, (f"The brief is not valid JSON: {exc.msg} (line {exc.lineno}).",))
    if not isinstance(data, dict):
        return GateResult(False, ("The brief must be a JSON object.",))

    try:
        brief = ProjectBrief.model_validate(data)
    except ValidationError as exc:
        return GateResult(False, tuple(_format_validation_error(exc)))

    issues = check_brief(brief)
    if issues:
        return GateResult(False, tuple(issues), brief)
    return GateResult(True, (), brief)
