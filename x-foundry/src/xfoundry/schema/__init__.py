"""JSON Schema access and the Pydantic models."""

from __future__ import annotations

import json
from functools import cache
from importlib import resources
from typing import Any

SCHEMA_VERSION = "1.0"
SUPPORTED_MAJOR = int(SCHEMA_VERSION.split(".")[0])
SCHEMA_FILE = "x-foundry.schema.json"


@cache
def load_schema() -> dict[str, Any]:
    """Return the ``x-foundry`` subtree JSON Schema (treat as read-only)."""
    text = resources.files(__package__).joinpath(SCHEMA_FILE).read_text(encoding="utf-8")
    return json.loads(text)
