"""Parse ``azure.yaml`` and extract the validated ``x-foundry`` configuration."""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from pydantic import ValidationError

from xfoundry.diagnostics import Diagnostic, ValidationFailed, error
from xfoundry.parser.loader import load_yaml, read_text
from xfoundry.parser.schema_validation import (
    ROOT,
    check_version,
    format_path,
    validate_schema,
)
from xfoundry.schema.models import XFoundry

__all__ = [
    "ParsedDocument",
    "parse_file",
    "parse_mapping",
    "parse_text",
    "parse_x_foundry",
    "pydantic_diagnostics",
]


@dataclass(slots=True)
class ParsedDocument:
    """Result of parsing: the typed configuration plus the author-supplied mapping."""

    source: str
    raw: dict[str, Any]
    config: XFoundry
    diagnostics: list[Diagnostic] = field(default_factory=list)


def pydantic_diagnostics(exc: ValidationError) -> list[Diagnostic]:
    return [
        error("XF110", e["msg"].removeprefix("Value error, "), format_path(list(e["loc"])))
        for e in exc.errors()
    ]


def parse_x_foundry(raw: Any, source: str = "<mapping>") -> ParsedDocument:
    """Validate an ``x-foundry`` subtree (JSON Schema first, then Pydantic)."""
    if not isinstance(raw, dict):
        raise ValidationFailed([error("XF101", "'x-foundry' must be a mapping", ROOT)])
    problems = check_version(raw)
    if problems:
        raise ValidationFailed(problems)
    problems = validate_schema(raw)
    if problems:
        raise ValidationFailed(problems)
    try:
        config = XFoundry.model_validate(raw)
    except ValidationError as exc:
        raise ValidationFailed(pydantic_diagnostics(exc)) from exc
    return ParsedDocument(source=source, raw=raw, config=config)


def parse_mapping(document: Any, source: str = "<mapping>") -> ParsedDocument:
    """Parse an already-loaded ``azure.yaml`` mapping."""
    if not isinstance(document, dict):
        raise ValidationFailed(
            [error("XF101", "azure.yaml must contain a mapping at the top level")]
        )
    if "x-foundry" not in document:
        raise ValidationFailed([error("XF101", "azure.yaml has no 'x-foundry' section", ROOT)])
    return parse_x_foundry(document["x-foundry"], source)


def parse_text(text: str, source: str = "<string>") -> ParsedDocument:
    return parse_mapping(load_yaml(text), source)


def parse_file(path: str | Path) -> ParsedDocument:
    return parse_text(read_text(path), str(path))
