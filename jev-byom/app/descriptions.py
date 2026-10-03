"""Descriptions for a calibration, so a client can choose one it has never seen.

An MCP client listing calibrations gets names and nothing else unless something
writes a description. Asking the caller to supply one means most of them will
not, so the default is computed from the training data itself: what the
scenario decides, over which classes, on how many examples, how balanced they
were, how well the fit scored, and what an input actually looks like.

That is enough to choose between `loan-approval` and `loan-fraud` without
opening either. An optional pass through an LLM turns those facts into a
sentence a human would write; it is off unless `JEV_DESCRIBE_LLM_URL` is set,
and it falls back to the computed text on any failure, because a listing that
loses its descriptions when a side service is down is worse than a plain one.
"""

from __future__ import annotations

import json
import logging
import urllib.error
import urllib.request
from collections import Counter
from collections.abc import Sequence
from typing import Any

from app.schemas import FitMetrics, TrainSample
from config.settings import Settings

logger = logging.getLogger(__name__)

MAX_EXAMPLE_CHARS = 160
MAX_LISTED_CLASSES = 6


def _example_text(sample: TrainSample, text_fields: Sequence[str]) -> str:
    """The first readable string in a sample, trimmed for a one-line summary."""
    for field in list(text_fields) + sorted(sample.input):
        value = sample.input.get(field)
        if isinstance(value, str) and value.strip():
            text = " ".join(value.split())
            return text[:MAX_EXAMPLE_CHARS] + ("…" if len(text) > MAX_EXAMPLE_CHARS else "")
    return ""


def summarise_training_data(
    samples: Sequence[TrainSample],
    *,
    decision_type: str,
    class_names: list[str] | None,
    text_fields: Sequence[str],
) -> dict[str, Any]:
    """The facts a description is built from. Pure, and useful on its own."""
    labels = [sample.label for sample in samples]
    facts: dict[str, Any] = {
        "decision_type": decision_type,
        "num_samples": len(samples),
        "examples": [_example_text(sample, text_fields) for sample in samples[:3]],
    }

    if decision_type == "numeric":
        values = [float(label) for label in labels if isinstance(label, (int, float))]
        if values:
            facts["label_min"] = min(values)
            facts["label_max"] = max(values)
            facts["label_mean"] = sum(values) / len(values)
        return facts

    counts = Counter(str(label) for label in labels)
    total = sum(counts.values()) or 1
    ordered = counts.most_common()
    facts["num_classes"] = len(class_names) if class_names else len(counts)
    facts["class_names"] = class_names or [name for name, _ in ordered]
    facts["most_common_class"] = ordered[0][0]
    facts["most_common_share"] = ordered[0][1] / total
    facts["rarest_class"] = ordered[-1][0]
    facts["rarest_count"] = ordered[-1][1]
    return facts


def compose_description(facts: dict[str, Any], metrics: FitMetrics | None = None) -> str:
    """Render the facts as a short paragraph. Deterministic, no network."""
    decision_type = facts["decision_type"]
    parts: list[str] = []

    if decision_type == "boolean":
        parts.append(f"Answers a yes/no question, trained on {facts['num_samples']:,} labelled examples.")
    elif decision_type == "enum":
        names = facts.get("class_names") or []
        shown = ", ".join(names[:MAX_LISTED_CLASSES])
        if len(names) > MAX_LISTED_CLASSES:
            shown += f", and {len(names) - MAX_LISTED_CLASSES} more"
        parts.append(
            f"Chooses one of {facts['num_classes']} options ({shown}), "
            f"trained on {facts['num_samples']:,} labelled examples."
        )
    else:
        low, high = facts.get("label_min"), facts.get("label_max")
        span = f" in the range {low:,.4g} to {high:,.4g}" if low is not None else ""
        parts.append(f"Estimates a number{span}, trained on {facts['num_samples']:,} examples.")

    if decision_type != "numeric" and "most_common_share" in facts:
        share = facts["most_common_share"]
        parts.append(
            f"The most common answer is {facts['most_common_class']!r} at {share:.0%} of the "
            f"training data, so anything at or below that share is no better than guessing it."
        )
        if facts.get("rarest_count", 0) < 10:
            parts.append(
                f"Note {facts['rarest_class']!r} has only {facts['rarest_count']} examples; "
                "its probabilities are the least trustworthy."
            )

    if metrics is not None:
        bits = []
        if metrics.accuracy is not None:
            bits.append(f"accuracy {metrics.accuracy:.0%}")
        if metrics.expected_calibration_error is not None:
            bits.append(f"calibration error {metrics.expected_calibration_error:.3f}")
        if metrics.mean_absolute_error is not None:
            bits.append(f"mean absolute error {metrics.mean_absolute_error:,.4g}")
        if metrics.coverage is not None:
            bits.append(f"coverage {metrics.coverage:.0%}")
        if bits:
            parts.append(f"On the {metrics.split} split: {', '.join(bits)}.")

    example = next((text for text in facts.get("examples", []) if text), "")
    if example:
        parts.append(f'Example input: "{example}"')

    return " ".join(parts)


def enrich_with_llm(description: str, facts: dict[str, Any], settings: Settings) -> str:
    """Rewrite the computed description through an LLM, if one is configured.

    Deliberately provider-agnostic: it posts an OpenAI-shaped chat completion to
    whatever URL it is given, which covers Azure OpenAI / Foundry's v1 surface —
    the convention this repository already uses in its shunt hook — as well as
    anything else that speaks the same wire format.

    The model is asked to rephrase facts, never to add any, because a
    description that invents a capability is worse than a clumsy one. Any
    failure returns the computed text unchanged.
    """
    if not settings.describe_llm_url:
        return description

    prompt = (
        "Rewrite the following description of a decision model as two or three "
        "plain sentences for a developer choosing between models. Keep every "
        "number. Add no capability, domain or claim that is not stated. Do not "
        "speculate about what it could be used for.\n\n"
        f"Facts: {json.dumps(facts, default=str)[:2000]}\n\nDescription: {description}"
    )
    payload = json.dumps(
        {
            "model": settings.describe_llm_model,
            "messages": [{"role": "user", "content": prompt}],
            "max_tokens": settings.describe_llm_max_tokens,
            "temperature": 0,
        }
    ).encode("utf-8")

    request = urllib.request.Request(settings.describe_llm_url, data=payload, method="POST")
    request.add_header("content-type", "application/json")
    if settings.describe_llm_token:
        request.add_header("authorization", f"Bearer {settings.describe_llm_token}")
    elif settings.describe_llm_api_key:
        request.add_header("api-key", settings.describe_llm_api_key)

    try:
        with urllib.request.urlopen(request, timeout=settings.describe_llm_timeout) as response:
            body = json.loads(response.read())
        text = body["choices"][0]["message"]["content"].strip()
        return text or description
    except (urllib.error.URLError, KeyError, IndexError, ValueError, TimeoutError) as error:
        logger.warning(
            "description enrichment failed, keeping the computed text",
            extra={"error": str(error)},
        )
        return description


def build_description(
    samples: Sequence[TrainSample],
    *,
    decision_type: str,
    class_names: list[str] | None,
    metrics: FitMetrics | None,
    settings: Settings,
    supplied: str | None = None,
) -> str:
    """The description stored with a calibration.

    A caller-supplied description always wins: they know why the scenario
    exists, and this code only knows what the data looks like.
    """
    if supplied and supplied.strip():
        return supplied.strip()
    facts = summarise_training_data(
        samples,
        decision_type=decision_type,
        class_names=class_names,
        text_fields=settings.model_text_fields,
    )
    description = compose_description(facts, metrics)
    if settings.describe_llm_url:
        description = enrich_with_llm(description, facts, settings)
    return description
