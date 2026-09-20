#!/usr/bin/env python3
"""Emit an OpenAPI 3.0 document describing POST /decision for Foundry.

    python scripts/foundry_tool_spec.py --base-url https://<app>.azurecontainerapps.io \
        --out foundry-decision-tool.json

Foundry's Agent Service consumes an OpenAPI document when you add an OpenAPI
tool to an agent. This script writes one **3.0.3** document containing only
``/decision``, with every schema inlined.

Why not just hand Foundry ``/openapi.json``? Two reasons:

  * FastAPI emits OpenAPI **3.1**, and the Agent Service's OpenAPI tool is
    documented against 3.0 — 3.1's ``type: ["string", "null"]`` unions and
    ``exclusiveMinimum`` as a number are the parts that trip it up;
  * the live document also advertises ``/posthoc_train``, and an agent that can
    retrain its own calibration is not what you want in production.

``--verify`` checks the emitted document against the running service: it sends
the example request and confirms the response matches the declared schema, so a
drift between this file and the app fails here rather than inside an agent run.
"""

from __future__ import annotations

import argparse
import json
import sys
import urllib.error
import urllib.request
from typing import Any

SECURITY_SCHEME = "ApiKeyAuth"


def build_spec(base_url: str, *, api_key_header: str = "x-api-key", secured: bool = True) -> dict[str, Any]:
    """Return the OpenAPI 3.0.3 document for the decision tool."""
    spec: dict[str, Any] = {
        "openapi": "3.0.3",
        "info": {
            "title": "Jev decision engine",
            "version": "1.0.0",
            "description": (
                "Returns a typed decision (boolean, enum or numeric) with a "
                "scenario-calibrated probability. Call it when a decision needs "
                "a probability you can threshold on, rather than a sentence."
            ),
        },
        "servers": [{"url": base_url.rstrip("/")}],
        "paths": {
            "/decision": {
                "post": {
                    "operationId": "make_decision",
                    "summary": "Return one typed, calibrated decision",
                    "description": (
                        "Scores the payload with the base model and applies the "
                        "calibration trained for this scenario. `probability` is "
                        "calibrated: 0.8 means roughly 80% of such decisions were "
                        "right in the training data."
                    ),
                    "requestBody": {
                        "required": True,
                        "content": {
                            "application/json": {
                                "schema": {
                                    "type": "object",
                                    "required": ["decision_type", "scenario", "data"],
                                    "properties": {
                                        "decision_type": {
                                            "type": "string",
                                            "enum": ["boolean", "enum", "numeric"],
                                            "description": "The shape of the answer you want.",
                                        },
                                        "scenario": {
                                            "type": "string",
                                            "description": (
                                                "Which trained calibration to use. "
                                                "GET /scenarios lists them."
                                            ),
                                        },
                                        "data": {
                                            "type": "object",
                                            "description": (
                                                "The payload to score, e.g. "
                                                '{"text": "..."}. Any string fields are used.'
                                            ),
                                            "additionalProperties": True,
                                        },
                                        "threshold": {
                                            "type": "number",
                                            "minimum": 0,
                                            "maximum": 1,
                                            "description": "Boolean only: decision cut-off.",
                                        },
                                        "include_probabilities": {
                                            "type": "boolean",
                                            "description": "Enum only: also return every class probability.",
                                        },
                                    },
                                },
                                "example": {
                                    "decision_type": "boolean",
                                    "scenario": "loan-approval",
                                    "data": {"text": "Applicant has 4 years of history and no arrears."},
                                },
                            }
                        },
                    },
                    "responses": {
                        "200": {
                            "description": "The decision.",
                            "content": {
                                "application/json": {
                                    "schema": {
                                        "type": "object",
                                        "required": [
                                            "value",
                                            "probability",
                                            "scenario",
                                            "decision_type",
                                            "calibrated",
                                            "latency_ms",
                                        ],
                                        "properties": {
                                            "value": {
                                                "description": (
                                                    "bool for boolean, class index for enum, "
                                                    "number for numeric."
                                                )
                                            },
                                            "probability": {
                                                "type": "number",
                                                "minimum": 0,
                                                "maximum": 1,
                                            },
                                            "scenario": {"type": "string"},
                                            "decision_type": {
                                                "type": "string",
                                                "enum": ["boolean", "enum", "numeric"],
                                            },
                                            "label": {
                                                "type": "string",
                                                "nullable": True,
                                                "description": "Enum only: the class name for value.",
                                            },
                                            "probabilities": {
                                                "type": "object",
                                                "nullable": True,
                                                "additionalProperties": {"type": "number"},
                                            },
                                            "calibrated": {"type": "boolean"},
                                            "calibration_version": {"type": "string", "nullable": True},
                                            "latency_ms": {"type": "number"},
                                        },
                                    }
                                }
                            },
                        },
                        "424": {"description": "The scenario has no calibration yet."},
                        "422": {"description": "The request body or the input could not be used."},
                    },
                }
            }
        },
    }

    if secured:
        spec["components"] = {
            "securitySchemes": {
                SECURITY_SCHEME: {"type": "apiKey", "in": "header", "name": api_key_header}
            }
        }
        spec["security"] = [{SECURITY_SCHEME: []}]
    return spec


def verify(spec: dict[str, Any], base_url: str, api_key: str | None) -> list[str]:
    """Send the example request and check the response against the declared schema."""
    operation = spec["paths"]["/decision"]["post"]
    example = operation["requestBody"]["content"]["application/json"]["example"]
    schema = operation["responses"]["200"]["content"]["application/json"]["schema"]

    request = urllib.request.Request(
        f"{base_url.rstrip('/')}/decision",
        data=json.dumps(example).encode("utf-8"),
        method="POST",
    )
    request.add_header("content-type", "application/json")
    if api_key:
        request.add_header("x-api-key", api_key)

    try:
        with urllib.request.urlopen(request, timeout=60) as response:
            body = json.loads(response.read())
    except urllib.error.HTTPError as error:
        detail = error.read().decode("utf-8", "replace")
        return [f"HTTP {error.code} from /decision: {detail}"]
    except urllib.error.URLError as error:
        return [f"/decision unreachable: {error.reason}"]

    problems = [f"response is missing {name!r}" for name in schema["required"] if name not in body]
    problems += [
        f"response has {name!r}, which the spec does not declare"
        for name in body
        if name not in schema["properties"]
    ]
    if isinstance(body.get("probability"), (int, float)) and not 0.0 <= body["probability"] <= 1.0:
        problems.append(f"probability out of range: {body['probability']}")
    return problems


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", required=True, help="Public https URL of the service.")
    parser.add_argument("--out", default="-", help="Output file, or - for stdout.")
    parser.add_argument("--api-key-header", default="x-api-key")
    parser.add_argument(
        "--no-auth", action="store_true", help="Emit a spec with no security scheme."
    )
    parser.add_argument(
        "--verify",
        action="store_true",
        help="Call the live endpoint and check the response against this spec.",
    )
    parser.add_argument("--api-key", default=None, help="Key to use with --verify.")
    args = parser.parse_args()

    spec = build_spec(
        args.base_url, api_key_header=args.api_key_header, secured=not args.no_auth
    )

    if args.verify:
        problems = verify(spec, args.base_url, args.api_key)
        for problem in problems:
            print(f"MISMATCH: {problem}", file=sys.stderr)
        if problems:
            return 1
        print("verified against the live endpoint", file=sys.stderr)

    rendered = json.dumps(spec, indent=2)
    if args.out == "-":
        print(rendered)
    else:
        with open(args.out, "w", encoding="utf-8") as handle:
            handle.write(rendered + "\n")
        print(f"wrote {args.out}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
