"""OpenAPI generation: error responses are documented as ``application/problem+json``."""

from __future__ import annotations

from typing import Any

from fastapi import FastAPI
from fastapi.openapi.utils import get_openapi

PROBLEM_JSON = "application/problem+json"


def _use_problem_media_type(schema: dict[str, Any]) -> None:
    for item in schema.get("paths", {}).values():
        for operation in item.values():
            for status, response in operation.get("responses", {}).items():
                if not status.startswith(("4", "5")):
                    continue
                content = response.get("content")
                if not content:
                    continue
                documented = next((c for c in content.values() if "schema" in c), None)
                if documented is not None:
                    response["content"] = {PROBLEM_JSON: documented}


def install_openapi(app: FastAPI) -> None:
    """Replace the default generator so error bodies carry the right media type."""

    def generate() -> dict[str, Any]:
        if app.openapi_schema is None:
            schema = get_openapi(
                title=app.title,
                version=app.version,
                openapi_version=app.openapi_version,
                summary=app.summary,
                description=app.description,
                routes=app.routes,
            )
            _use_problem_media_type(schema)
            app.openapi_schema = schema
        return app.openapi_schema

    app.openapi = generate  # type: ignore[method-assign]
