"""Stage 1: the deterministic filter from catalog to the candidate list Decision-1 chooses from.

Steps run in a fixed order and each one is recorded, so the decision log shows which step
removed which model and why:

  0. api                with claude_translation=false the body is an Anthropic Messages request,
                        which only Claude models accept
  1. lifecycle          deprecated/retired/legacy always out; preview out unless allowed
  2. operator allow-list ROUTER_MODEL_ALLOWLIST
  3. request selection  routing_constraints.models / exclude_models / providers
  4. capabilities       routing_constraints.capabilities plus what the request itself needs
  5. location           routing_constraints.region / deployment_type / inference_in_azure
  6. size               estimated prompt + output tokens against the context window, input and output limits
  7. price band         the routing mode's share of what is left, by tier

No step may leave zero models silently: the caller turns an empty result into a 422 that names
the step.
"""
from __future__ import annotations

import json
import math
from dataclasses import dataclass, field
from typing import Any

from .catalog import EXCLUDED_LIFECYCLES, Catalog, ModelEntry
from .config import Settings

KNOWN_CAPABILITIES = ("tools", "parallel_tools", "structured_output", "json_object", "streaming", "reasoning",
                      "image_input", "computer_use")
DEPLOYMENT_TYPES = ("global_standard", "data_zone_standard", "standard")
CONSTRAINT_KEYS = ("models", "exclude_models", "providers", "capabilities", "region", "deployment_type",
                   "inference_in_azure", "min_context_tokens", "allow_preview")


class ConstraintError(ValueError):
    pass


@dataclass(frozen=True)
class Constraints:
    models: tuple[str, ...] = ()
    exclude_models: tuple[str, ...] = ()
    providers: tuple[str, ...] = ()
    capabilities: tuple[str, ...] = ()
    region: str | None = None
    deployment_type: str | None = None
    inference_in_azure: bool = False
    min_context_tokens: int | None = None
    allow_preview: bool | None = None

    def as_dict(self) -> dict[str, Any]:
        return {k: v for k, v in self.__dict__.items() if v not in ((), None, False)}


def _names(raw: dict[str, Any], key: str) -> tuple[str, ...]:
    value = raw.get(key, [])
    if not isinstance(value, list) or not all(isinstance(v, str) and v for v in value):
        raise ConstraintError(f"routing_constraints.{key} must be a list of strings")
    return tuple(value)


def parse_constraints(raw: Any, catalog: Catalog) -> Constraints:
    if raw is None:
        return Constraints()
    if not isinstance(raw, dict):
        raise ConstraintError("routing_constraints must be an object")
    unknown = set(raw) - set(CONSTRAINT_KEYS)
    if unknown:
        raise ConstraintError(f"unknown routing_constraints keys: {sorted(unknown)}; allowed: {list(CONSTRAINT_KEYS)}")
    models, excluded = _names(raw, "models"), _names(raw, "exclude_models")
    missing = [m for m in models + excluded if m not in catalog.by_name]
    if missing:
        raise ConstraintError(f"routing_constraints names models not in the '{catalog.compatibility}' catalog: {missing}")
    capabilities = _names(raw, "capabilities")
    bad = [c for c in capabilities if c not in KNOWN_CAPABILITIES]
    if bad:
        raise ConstraintError(f"unknown capabilities {bad}; allowed: {list(KNOWN_CAPABILITIES)}")
    deployment_type = raw.get("deployment_type")
    if deployment_type is not None and deployment_type not in DEPLOYMENT_TYPES:
        raise ConstraintError(f"routing_constraints.deployment_type must be one of {list(DEPLOYMENT_TYPES)}")
    region = raw.get("region")
    if region is not None and (not isinstance(region, str) or not region):
        raise ConstraintError("routing_constraints.region must be an Azure region name such as 'swedencentral'")
    min_context = raw.get("min_context_tokens")
    if min_context is not None and (not isinstance(min_context, int) or isinstance(min_context, bool) or min_context < 1):
        raise ConstraintError("routing_constraints.min_context_tokens must be a positive integer")
    for flag in ("inference_in_azure", "allow_preview"):
        if flag in raw and not isinstance(raw[flag], bool):
            raise ConstraintError(f"routing_constraints.{flag} must be true or false")
    return Constraints(models, excluded, _names(raw, "providers"), capabilities,
                       region.strip().lower().replace(" ", "") if region else None, deployment_type,
                       bool(raw.get("inference_in_azure", False)), min_context, raw.get("allow_preview"))


def requirements_from_messages_body(body: dict[str, Any]) -> dict[str, str]:
    """Same as requirements_from_request, for an Anthropic Messages body (claude_translation=false)."""
    needs: dict[str, str] = {}
    if body.get("tools"):
        needs["tools"] = "request has tools"
    if body.get("stream") is True:
        needs["streaming"] = "stream=true"
    for message in body.get("messages", []):
        content = message.get("content") if isinstance(message, dict) else None
        if isinstance(content, list) and any(isinstance(p, dict) and p.get("type") == "image" for p in content):
            needs["image_input"] = "messages contain images"
            break
    return needs


def requirements_from_request(body: dict[str, Any]) -> dict[str, str]:
    """Capabilities the request itself needs, with the field that implies each one."""
    needs: dict[str, str] = {}
    if body.get("tools"):
        needs["tools"] = "request has tools"
    if body.get("parallel_tool_calls") is True and body.get("tools"):
        needs["parallel_tools"] = "parallel_tool_calls=true"
    fmt = body.get("response_format")
    if isinstance(fmt, dict):
        if fmt.get("type") == "json_schema":
            needs["structured_output"] = "response_format=json_schema"
        elif fmt.get("type") == "json_object":
            needs["json_object"] = "response_format=json_object"
    if body.get("stream") is True:
        needs["streaming"] = "stream=true"
    for message in body.get("messages", []):
        content = message.get("content") if isinstance(message, dict) else None
        if isinstance(content, list) and any(isinstance(p, dict) and p.get("type") == "image_url" for p in content):
            needs["image_input"] = "messages contain images"
            break
    return needs


def estimated_tokens(body: dict[str, Any], chars_per_token: float) -> tuple[int, int]:
    """(prompt tokens, requested output tokens). A deliberately simple estimate: characters / chars_per_token."""
    text = (json.dumps(body.get("messages", []), ensure_ascii=False) + json.dumps(body.get("tools") or [])
            + json.dumps(body.get("system") or ""))
    output = body.get("max_completion_tokens") or body.get("max_tokens") or 0
    return math.ceil(len(text) / chars_per_token), int(output) if isinstance(output, int) else 0


@dataclass
class Stage1Result:
    candidates: list[ModelEntry]
    steps: list[dict[str, Any]] = field(default_factory=list)
    requirements: dict[str, str] = field(default_factory=dict)
    empty_at: str | None = None

    def log(self) -> dict[str, Any]:
        return {"steps": self.steps, "requirements": self.requirements, "empty_at": self.empty_at}


def select(catalog: Catalog, mode: str, constraints: Constraints, body: dict[str, Any], settings: Settings,
           passthrough: bool = False) -> Stage1Result:
    remaining = list(catalog.models)
    requirements = requirements_from_messages_body(body) if passthrough else requirements_from_request(body)
    result = Stage1Result([], requirements=requirements)

    def step(name: str, keep) -> bool:
        nonlocal remaining
        kept, removed = [], []
        for model in remaining:
            reason = keep(model)
            (removed.append({"model": model.name, "reason": reason}) if reason else kept.append(model))
        result.steps.append({"step": name, "removed": removed, "remaining": len(kept)})
        remaining = kept
        if not kept:
            result.empty_at = name
            return False
        return True

    allow_preview = settings.allow_preview if constraints.allow_preview is None else constraints.allow_preview

    def api(m: ModelEntry) -> str | None:
        if passthrough and m.api != "anthropic_messages":
            return "cannot take an Anthropic Messages body (claude_translation=false)"
        return None
    unknown_ok = settings.unknown_capability == "eligible"

    def lifecycle(m: ModelEntry) -> str | None:
        if m.lifecycle in EXCLUDED_LIFECYCLES:
            return f"lifecycle {m.lifecycle}"
        if m.lifecycle == "preview" and not allow_preview:
            return "preview models not allowed"
        return None

    def allowlist(m: ModelEntry) -> str | None:
        return None if not settings.model_allowlist or m.name in settings.model_allowlist else "not in ROUTER_MODEL_ALLOWLIST"

    def selection(m: ModelEntry) -> str | None:
        if constraints.models and m.name not in constraints.models:
            return "not in routing_constraints.models"
        if m.name in constraints.exclude_models:
            return "in routing_constraints.exclude_models"
        if constraints.providers and m.provider.lower() not in {p.lower() for p in constraints.providers}:
            return f"provider {m.provider} not requested"
        return None

    needed = {c: "routing_constraints.capabilities" for c in constraints.capabilities} | requirements

    def capabilities(m: ModelEntry) -> str | None:
        for capability, why in needed.items():
            value = m.capability(capability)
            if value == "no" or (value == "unknown" and not unknown_ok):
                return f"{capability} {value} (needed: {why})"
        return None

    def location(m: ModelEntry) -> str | None:
        if constraints.inference_in_azure and m.infrastructure != "azure":
            return f"inference runs on {m.infrastructure} infrastructure"
        if m.region_status == "global":
            # Deployable from any region, as a Global Standard deployment only.
            if constraints.deployment_type and constraints.deployment_type != "global_standard":
                return f"global deployment only, no {constraints.deployment_type}"
            return None
        if constraints.deployment_type and constraints.deployment_type not in m.regions and m.regions:
            return f"no {constraints.deployment_type} deployment"
        if constraints.region:
            if not m.regions:
                return "regions not published"
            types = [constraints.deployment_type] if constraints.deployment_type else list(m.regions)
            if not any(constraints.region in m.regions.get(t, ()) for t in types):
                return f"not available in {constraints.region}"
        elif constraints.deployment_type and not m.regions:
            return "deployment types not published"
        return None

    prompt_tokens, output_tokens = estimated_tokens(body, settings.chars_per_token)
    needed_context = max(prompt_tokens + output_tokens, constraints.min_context_tokens or 0)

    def size(m: ModelEntry) -> str | None:
        if m.context_tokens is None:
            return None if unknown_ok else "context window unknown"
        if needed_context > m.context_tokens:
            return f"needs ~{needed_context} tokens, context is {m.context_tokens}"
        if m.max_input_tokens and prompt_tokens > m.max_input_tokens:
            return f"prompt is ~{prompt_tokens} tokens, input limit is {m.max_input_tokens}"
        if output_tokens and m.max_output_tokens and output_tokens > m.max_output_tokens:
            return f"asks for {output_tokens} output tokens, limit is {m.max_output_tokens}"
        return None

    for name, keep in (("api", api), ("lifecycle", lifecycle), ("allowlist", allowlist), ("selection", selection),
                       ("capabilities", capabilities), ("location", location), ("size", size)):
        if not step(name, keep):
            return result

    banded = {m.name for m in catalog.price_band(mode, remaining)}
    step("price_band", lambda m: None if m.name in banded else f"outside the {mode} price band")
    result.candidates = remaining
    return result
