"""An MCP server that lets an agent use the decision engine as a tool.

    python -m mcp_server.server                      # stdio, for a local client
    JEV_MCP_TRANSPORT=streamable-http python -m mcp_server.server

Four tools, which together are the whole loop an agent needs:

    list_calibrations    what decisions this engine already knows how to make
    describe_calibration one calibration in detail, including its versions
    train_calibration    teach it a new one from labelled examples
    decide               ask for a decision, with a calibrated probability

The point of `list_calibrations` returning a description per calibration is
that an agent can *discover* a decision it did not know existed and use it
instead of asking a model — which is the whole reason to put this behind MCP
rather than behind a bespoke client.

This process holds no model and no state. It is a thin translation layer over
the HTTP service, so the engine can scale independently and several MCP
clients can share one calibration store.
"""

from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.request
from typing import Any

from mcp.server.mcpserver import MCPServer

BASE_URL = os.environ.get("JEV_ENGINE_URL", "http://localhost:8000").rstrip("/")
API_KEY = os.environ.get("JEV_API_KEY")
TIMEOUT = float(os.environ.get("JEV_MCP_TIMEOUT", "300"))

server = MCPServer(
    name="jev-decision-engine",
    title="Jev decision engine",
    instructions=(
        "Typed decisions with calibrated probabilities. Call list_calibrations "
        "first: if a calibration already covers the decision you face, use "
        "decide instead of reasoning it out — it is one small forward pass "
        "rather than a generation, and the probability it returns is "
        "calibrated, so you can threshold on it. Treat a probability below "
        "your threshold as 'ask a model', not as a weak answer."
    ),
)


class EngineError(RuntimeError):
    """The engine refused the request; the message is meant for the agent."""


def call(path: str, payload: dict[str, Any] | None = None, method: str = "GET") -> Any:
    """One HTTP call to the engine, with its error text preserved.

    A tool that swallows the engine's 422 and says "failed" makes an agent
    retry blindly. The detail field explains what to change, so it is passed
    through verbatim.
    """
    data = json.dumps(payload).encode("utf-8") if payload is not None else None
    request = urllib.request.Request(f"{BASE_URL}{path}", data=data, method=method)
    request.add_header("content-type", "application/json")
    if API_KEY:
        request.add_header("x-api-key", API_KEY)
    try:
        with urllib.request.urlopen(request, timeout=TIMEOUT) as response:
            return json.loads(response.read())
    except urllib.error.HTTPError as error:
        body = error.read().decode("utf-8", "replace")
        try:
            parsed = json.loads(body)
            detail = parsed.get("detail", body)
            code = parsed.get("error", str(error.code))
        except json.JSONDecodeError:
            detail, code = body, str(error.code)
        raise EngineError(f"{code}: {detail}") from None
    except urllib.error.URLError as error:
        raise EngineError(
            f"decision engine unreachable at {BASE_URL} ({error.reason}). "
            "Set JEV_ENGINE_URL if it is running elsewhere."
        ) from None


@server.tool(
    title="List calibrations",
    description=(
        "List every decision this engine can already make, with a description "
        "of what each one decides and how well it scored. Call this before "
        "reasoning about a decision yourself — one may already exist."
    ),
)
def list_calibrations() -> dict[str, Any]:
    """Return the available calibrations, newest-trained detail included."""
    rows = call("/scenarios")["scenarios"]
    return {
        "count": len(rows),
        "calibrations": [
            {
                "scenario": row["scenario"],
                "decision_type": row["decision_type"],
                "description": row.get("description"),
                "options": row.get("class_names"),
                "trained_on": row.get("num_samples"),
                "trained_at": row.get("trained_at"),
                "versions": row.get("num_versions"),
            }
            for row in rows
        ],
    }


@server.tool(
    title="Describe a calibration",
    description=(
        "Full detail for one calibration, including every stored version. Use "
        "it to check what a decision means before relying on it, or to find an "
        "older version to pin."
    ),
)
def describe_calibration(scenario: str, decision_type: str) -> dict[str, Any]:
    """Return one calibration's description, options and version history."""
    rows = [
        row
        for row in call("/scenarios")["scenarios"]
        if row["scenario"] == scenario and row["decision_type"] == decision_type
    ]
    if not rows:
        raise EngineError(
            f"no calibration named {scenario!r} of type {decision_type!r}; "
            "call list_calibrations to see what exists"
        )
    versions = call(f"/scenarios/{scenario}/{decision_type}/versions")
    return {**rows[0], "versions": versions["versions"]}


@server.tool(
    title="Train a calibration",
    description=(
        "Teach the engine a new decision from labelled examples. Each example "
        "is {input: {text: ...}, label: ...} — a bool for boolean, a class name "
        "for enum, a number for numeric. If the name is taken, on_conflict "
        "decides: new_version supersedes it (the old one stays pinnable), "
        "new_scenario trains the next free name instead, reject refuses."
    ),
)
def train_calibration(
    scenario: str,
    decision_type: str,
    training_data: list[dict[str, Any]],
    class_names: list[str] | None = None,
    description: str | None = None,
    on_conflict: str = "new_version",
) -> dict[str, Any]:
    """Fit and store a calibration; returns where it landed and how it scored."""
    body: dict[str, Any] = {
        "decision_type": decision_type,
        "scenario": scenario,
        "training_data": training_data,
        "on_conflict": on_conflict,
    }
    if class_names:
        body["class_names"] = class_names
    if description:
        body["description"] = description

    result = call("/posthoc_train", body, method="POST")
    out = {
        "scenario": result["scenario"],
        "decision_type": result["decision_type"],
        "description": result.get("description"),
        "calibration_version": result["calibration_version"],
        "num_samples": result["num_samples"],
        "temperature_clamped": result.get("temperature_clamped"),
        "metrics_after": result.get("metrics_after"),
    }
    if result.get("requested_scenario"):
        out["note"] = (
            f"{result['requested_scenario']!r} was already taken, so this was "
            f"trained as {result['scenario']!r}; the existing one is untouched."
        )
    if result.get("temperature_clamped"):
        out["warning"] = (
            "The fit ran to the edge of its temperature range, which usually "
            "means the labels carry little signal the base model can see. "
            "Check metrics_after before relying on this."
        )
    return out


@server.tool(
    title="Make a decision",
    description=(
        "Get one typed decision with a calibrated probability. The probability "
        "is meaningful: 0.8 means decisions like this were right about 80% of "
        "the time in training. Below your threshold, fall back to reasoning "
        "rather than trusting the value."
    ),
)
def decide(
    scenario: str,
    decision_type: str,
    text: str,
    threshold: float | None = None,
    include_probabilities: bool = False,
    calibration_version: str | None = None,
) -> dict[str, Any]:
    """Return {value, probability, ...} for one prompt."""
    body: dict[str, Any] = {
        "decision_type": decision_type,
        "scenario": scenario,
        "data": {"text": text},
        "include_probabilities": include_probabilities,
    }
    if threshold is not None:
        body["threshold"] = threshold
    if calibration_version:
        body["calibration_version"] = calibration_version

    result = call("/decision", body, method="POST")
    if not result["calibrated"]:
        # The HTTP API may be configured to fall back to the raw model for an
        # untrained scenario. That is a reasonable default for a human holding
        # the request, and a bad one for an agent that asked for a named
        # calibration: it would read an uncalibrated guess as a calibrated
        # answer. Fail here instead.
        raise EngineError(
            f"{scenario!r} ({decision_type}) has no calibration, so this would be an "
            "uncalibrated guess from the base model. Call list_calibrations to see "
            "what exists, or train_calibration to create it."
        )
    return {
        "value": result["value"],
        "probability": result["probability"],
        "label": result.get("label"),
        "probabilities": result.get("probabilities"),
        "calibrated": result["calibrated"],
        "calibration_version": result.get("calibration_version"),
        "latency_ms": round(result["latency_ms"], 1),
    }


def main() -> None:  # pragma: no cover - process entry point
    transport = os.environ.get("JEV_MCP_TRANSPORT", "stdio")
    if transport not in ("stdio", "sse", "streamable-http"):
        print(f"unknown JEV_MCP_TRANSPORT {transport!r}", file=sys.stderr)
        raise SystemExit(2)
    server.run(transport=transport)  # type: ignore[arg-type]


if __name__ == "__main__":  # pragma: no cover
    main()
