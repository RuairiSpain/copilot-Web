"""Azure resource-name constraints (shared by validation and, later, name generation).

Each rule describes what the resource provider accepts. Sources: "Naming rules and
restrictions for Azure resources" (Microsoft Learn).
"""

from __future__ import annotations

import re
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class NameRule:
    kind: str
    min_length: int
    max_length: int
    pattern: str
    description: str
    forbid_consecutive_hyphens: bool = False

    def problems(self, name: str) -> list[str]:
        found: list[str] = []
        if not self.min_length <= len(name) <= self.max_length:
            found.append(f"must be {self.min_length}-{self.max_length} characters long")
        if not re.fullmatch(self.pattern, name):
            found.append(self.description)
        if self.forbid_consecutive_hyphens and "--" in name:
            found.append("must not contain consecutive hyphens")
        return found


NAME_RULES: dict[str, NameRule] = {
    rule.kind: rule
    for rule in (
        NameRule("storage", 3, 24, r"[a-z0-9]+", "may only contain lowercase letters and digits"),
        NameRule(
            "key-vault",
            3,
            24,
            r"[A-Za-z][A-Za-z0-9-]*[A-Za-z0-9]",
            "must start with a letter, end with a letter or digit and contain only letters, digits and hyphens",
            forbid_consecutive_hyphens=True,
        ),
        NameRule(
            "search",
            2,
            60,
            r"[a-z0-9](?:[a-z0-9-]*[a-z0-9])?",
            "may only contain lowercase letters, digits and hyphens, and cannot start or end with a hyphen",
            forbid_consecutive_hyphens=True,
        ),
        NameRule(
            "redis",
            1,
            63,
            r"[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?",
            "may only contain letters, digits and hyphens, and cannot start or end with a hyphen",
            forbid_consecutive_hyphens=True,
        ),
        NameRule(
            "apim",
            1,
            50,
            r"[A-Za-z][A-Za-z0-9-]*[A-Za-z0-9]|[A-Za-z]",
            "must start with a letter, end with a letter or digit and contain only letters, digits and hyphens",
        ),
        NameRule(
            "container-app",
            2,
            32,
            r"[a-z](?:[a-z0-9-]*[a-z0-9])?",
            "may only contain lowercase letters, digits and hyphens, must start with a letter and end with a letter or digit",
            forbid_consecutive_hyphens=True,
        ),
        NameRule(
            "storage-container",
            3,
            63,
            r"[a-z0-9]+(?:-[a-z0-9]+)*",
            "may only contain lowercase letters, digits and single hyphens between letters or digits",
        ),
        NameRule("registry", 5, 50, r"[A-Za-z0-9]+", "may only contain letters and digits"),
        NameRule(
            "service-bus",
            6,
            50,
            r"[A-Za-z][A-Za-z0-9-]*[A-Za-z0-9]",
            "must start with a letter, end with a letter or digit and contain only letters, digits and hyphens",
        ),
        NameRule(
            "managed-identity",
            3,
            128,
            r"[A-Za-z0-9][A-Za-z0-9_-]*",
            "must start with a letter or digit and contain only letters, digits, hyphens and underscores",
        ),
        NameRule(
            "resource-group",
            1,
            90,
            r"[\w.()-]*[\w()-]",
            "may contain letters, digits, underscores, hyphens, periods and parentheses, and cannot end with a period",
        ),
    )
}


def name_problems(kind: str, name: str) -> list[str]:
    """Return why ``name`` is invalid for ``kind`` (empty when valid)."""
    return NAME_RULES[kind].problems(name)
