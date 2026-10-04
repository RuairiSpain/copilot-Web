"""JSON Schema validation of the ``x-foundry`` subtree."""

from __future__ import annotations

import re
from collections.abc import Iterator, Sequence
from typing import Any

from jsonschema import Draft202012Validator
from jsonschema.exceptions import ValidationError, best_match

from xfoundry.diagnostics import Diagnostic, error
from xfoundry.schema import SUPPORTED_MAJOR, load_schema

ROOT = "x-foundry"

_SESSION_POOL = re.compile(r"session[-_ ]?pool|agent[-_ ]?pool|pooling", re.IGNORECASE)


def format_path(parts: Sequence[str | int], root: str = ROOT) -> str:
    out = root
    for part in parts:
        out += f"[{part}]" if isinstance(part, int) else f".{part}"
    return out


def _walk(node: Any, path: tuple[str | int, ...]) -> Iterator[tuple[tuple[str | int, ...], str]]:
    if isinstance(node, dict):
        for key, value in node.items():
            yield path, str(key)
            yield from _walk(value, (*path, str(key)))
    elif isinstance(node, list):
        for index, item in enumerate(node):
            yield from _walk(item, (*path, index))


def find_unsupported_keys(raw: Any) -> list[Diagnostic]:
    """Rules 18 and 19: session pools are out of scope; Redis is application caching only."""
    found: list[Diagnostic] = []
    for parent, key in _walk(raw, ()):
        if _SESSION_POOL.search(key):
            found.append(
                error(
                    "XF018",
                    f"'{key}' is not supported: session and agent pooling are separate Foundry "
                    "capabilities and are out of scope for x-foundry",
                    format_path((*parent, key)),
                )
            )
        elif parent and parent[-1] == "redis" and "session" in key.lower():
            found.append(
                error(
                    "XF019",
                    f"'{key}' is not supported: Redis is used for application caching only, "
                    "not as a session store",
                    format_path((*parent, key)),
                )
            )
    return found


def check_version(raw: dict[str, Any]) -> list[Diagnostic]:
    version = raw.get("schemaVersion")
    if not isinstance(version, str) or not re.fullmatch(r"\d+\.\d+", version):
        return []  # absent means default; malformed is reported by the schema pass
    major = int(version.split(".")[0])
    if major != SUPPORTED_MAJOR:
        return [
            error(
                "XF103",
                f"schemaVersion {version} is not supported; this release supports "
                f"{SUPPORTED_MAJOR}.x",
                f"{ROOT}.schemaVersion",
            )
        ]
    return []


def _message(err: ValidationError) -> str:
    message = err.message
    if len(message) > 200:
        message = message[:197] + "..."
    if err.context and err.validator in {"anyOf", "oneOf"}:
        detail = best_match(err.context)
        if detail is not None:
            message = f"{message} ({detail.message[:160]})"
    return message


def validate_schema(raw: dict[str, Any]) -> list[Diagnostic]:
    """Validate ``raw`` against the JSON Schema; returns one diagnostic per violation."""
    validator = Draft202012Validator(
        load_schema(), format_checker=Draft202012Validator.FORMAT_CHECKER
    )
    unsupported = find_unsupported_keys(raw)
    flagged = {d.path for d in unsupported}
    diagnostics: list[Diagnostic] = []
    for err in validator.iter_errors(raw):
        path = format_path(list(err.absolute_path))
        if err.validator == "additionalProperties" and any(
            f"{path}.{key}" in flagged for key in re.findall(r"'([^']+)'", err.message)
        ):
            continue  # already reported with a more specific message
        diagnostics.append(error("XF102", _message(err), path))
    diagnostics.sort(key=lambda d: (d.path, d.message))
    return unsupported + diagnostics
