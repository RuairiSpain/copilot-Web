"""Diagnostics shared by every validation phase.

Codes ``XF001``-``XF025`` map one-to-one to the numbered semantic rules in the
specification. ``XF1xx`` codes cover parsing, schema and extra checks.
"""

from __future__ import annotations

from collections.abc import Iterable
from dataclasses import dataclass
from enum import StrEnum


class Severity(StrEnum):
    ERROR = "error"
    WARNING = "warning"


@dataclass(frozen=True, slots=True)
class Diagnostic:
    code: str
    message: str
    path: str = ""
    severity: Severity = Severity.ERROR

    def __str__(self) -> str:
        where = f" at {self.path}" if self.path else ""
        return f"{self.severity.value} {self.code}{where}: {self.message}"

    def to_dict(self) -> dict[str, str]:
        return {
            "code": self.code,
            "severity": self.severity.value,
            "path": self.path,
            "message": self.message,
        }


def error(code: str, message: str, path: str = "") -> Diagnostic:
    return Diagnostic(code, message, path, Severity.ERROR)


def warning(code: str, message: str, path: str = "") -> Diagnostic:
    return Diagnostic(code, message, path, Severity.WARNING)


def errors_only(diagnostics: Iterable[Diagnostic]) -> list[Diagnostic]:
    return [d for d in diagnostics if d.severity is Severity.ERROR]


class ValidationFailed(Exception):
    """Raised when a phase produced at least one error diagnostic."""

    def __init__(self, diagnostics: Iterable[Diagnostic]):
        self.diagnostics: list[Diagnostic] = list(diagnostics)
        count = len(errors_only(self.diagnostics))
        lines = "\n".join(f"  {d}" for d in self.diagnostics)
        super().__init__(f"x-foundry validation failed with {count} error(s):\n{lines}")
