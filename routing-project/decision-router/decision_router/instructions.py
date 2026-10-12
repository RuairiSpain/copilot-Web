"""The `instructions` and `criteria` Decision-1 receives, built from static templates.

No model writes this text. config/decision1_instructions.json holds fixed fragments; stage 1's
results (mode, profile, actions, cost band, what the request needs, token estimates, pin state)
decide which fragments apply and fill their placeholders. The same request and catalog always
give the same text, and every sentence Decision-1 can see is reviewable in one file.
"""
from __future__ import annotations

import hashlib
import json
import string
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from .catalog import CAPABILITY_TAGS, CostBand, ModelEntry
from .stage1 import COST_LABELS, SPANNING_LABELS, Constraints, estimated_tokens

CONDITIONS = ("mode", "profile", "action", "band", "has")
BAND_KINDS = ("named", "range", "group")
FLAGS = ("tools", "structured_output", "image_input", "reasoning", "long_prompt", "long_output",
         "pin_rerouted", "passthrough")
PLACEHOLDERS = ("actions", "band_label", "candidate_count", "price_min", "price_max", "tool_names", "image_count",
                "prompt_tokens", "output_tokens", "previous_model", "pin_reason")
CRITERIA_PLACEHOLDERS = ("description", "input_price", "output_price", "price_rank", "candidate_count")
ALWAYS_KEPT = ("base", "mode.")


class TemplateError(ValueError):
    pass


@dataclass(frozen=True)
class Fragment:
    id: str
    text: str
    when: dict[str, list[str]] = field(default_factory=dict)


@dataclass(frozen=True)
class Rendered:
    instructions: str
    fragments: list[str]
    criteria: dict[str, str]
    dropped: list[str] = field(default_factory=list)


def _fields(text: str) -> set[str]:
    return {name for _, name, _, _ in string.Formatter().parse(text) if name is not None}


def _money(value: float) -> str:
    """$ amounts: whole dollars without decimals, otherwise two decimals (2.5 -> 2.50), or three when the
    price has them (0.125 stays 0.125 rather than rounding to 0.12)."""
    if value == int(value):
        return f"{value:,.0f}"
    return f"{value:,.2f}" if round(value, 2) == value else f"{value:,.3f}"


def _range(band: CostBand) -> str:
    if band.max is None:
        return f"${_money(band.min or 0)} and above"
    return f"${_money(band.min or 0)} to ${_money(band.max)}"


class InstructionTemplates:
    def __init__(self, fragments: list[Fragment], criteria: dict[str, str], max_chars: int,
                 thresholds: dict[str, int], action_names: dict[str, str], fingerprint: str = ""):
        self.fragments = fragments
        self.action_names = action_names
        self.criteria_templates = criteria
        self.max_chars = max_chars
        self.thresholds = thresholds
        self.fingerprint = fingerprint
        self._validate()

    @classmethod
    def load(cls, path: Path) -> InstructionTemplates:
        raw = path.read_bytes()
        data = json.loads(raw)
        fragments = [Fragment(f["id"], f["text"], {k: list(v) for k, v in (f.get("when") or {}).items()})
                     for f in data["fragments"]]
        return cls(fragments, data["criteria"], int(data["max_chars"]), dict(data["thresholds"]),
                   dict(data["action_names"]), hashlib.sha256(raw).hexdigest())

    def _validate(self) -> None:
        ids = [f.id for f in self.fragments]
        if len(set(ids)) != len(ids):
            raise TemplateError("fragment ids must be unique")
        if not ids or ids[0] != "base":
            raise TemplateError("the first fragment must be 'base'")
        for f in self.fragments:
            if set(f.when) - set(CONDITIONS):
                raise TemplateError(f"fragment {f.id}: unknown condition {sorted(set(f.when) - set(CONDITIONS))}")
            if set(f.when.get("band", [])) - set(BAND_KINDS) or set(f.when.get("has", [])) - set(FLAGS):
                raise TemplateError(f"fragment {f.id}: unknown band kind or flag")
            if _fields(f.text) - set(PLACEHOLDERS):
                raise TemplateError(f"fragment {f.id}: unknown placeholder {sorted(_fields(f.text) - set(PLACEHOLDERS))}")
        if set(self.criteria_templates) != {"priced", "unpriced"}:
            raise TemplateError("criteria needs 'priced' and 'unpriced' templates")
        for name, text in self.criteria_templates.items():
            if _fields(text) - set(CRITERIA_PLACEHOLDERS):
                raise TemplateError(f"criteria.{name}: unknown placeholder")
        if set(self.action_names) != set(CAPABILITY_TAGS["actions"]):
            raise TemplateError(f"action_names must name exactly the actions {list(CAPABILITY_TAGS['actions'])}")
        if set(self.thresholds) != {"long_prompt_tokens", "long_output_tokens"}:
            raise TemplateError("thresholds needs long_prompt_tokens and long_output_tokens")

    # ------------------------------------------------------------------ facts
    def facts(self, *, mode: str, candidates: list[ModelEntry], constraints: Constraints,
              cost_band: CostBand | None, requirements: dict[str, str], body: dict[str, Any],
              chars_per_token: float, profile: str | None = None, passthrough: bool = False,
              pin: dict[str, Any] | None = None, tokens: tuple[int, int] | None = None) -> dict[str, Any]:
        """What stage 1 knows, as condition values and placeholder values."""
        needed = set(requirements) | set(constraints.capabilities)
        prompt_tokens, output_tokens = tokens or estimated_tokens(body, chars_per_token)
        prices = [m.output_price for m in candidates if m.output_price is not None]
        tools = [(t.get("function") or {}).get("name") or t.get("name")
                 for t in body.get("tools") or [] if isinstance(t, dict)]
        images = sum(1 for m in body.get("messages") or [] if isinstance(m, dict) and isinstance(m.get("content"), list)
                     for part in m["content"] if isinstance(part, dict) and part.get("type") in ("image_url", "image"))
        band_kind = None
        if cost_band is not None:
            labelled = cost_band.name in COST_LABELS or cost_band.name in SPANNING_LABELS
            band_kind = "group" if labelled and constraints.actions else "named" if cost_band.name else "range"
        rerouted = bool(pin and pin.get("status") == "rerouted")
        values = {
            "actions": " and ".join(self.action_names[a] for a in constraints.actions) or None,
            "band_label": (cost_band.name or _range(cost_band)) if cost_band else None,
            "candidate_count": str(len(candidates)),
            "price_min": _money(min(prices)) if prices else None,
            "price_max": _money(max(prices)) if prices else None,
            "tool_names": ", ".join(str(t) for t in tools if t) or None,
            "image_count": str(images) if images else None,
            "prompt_tokens": f"{prompt_tokens:,}",
            "output_tokens": f"{output_tokens:,}" if output_tokens else None,
            "previous_model": pin.get("previous_model") if rerouted else None,
            "pin_reason": pin.get("reason") if rerouted else None,
        }
        flags = {
            "tools": "tools" in needed, "structured_output": "structured_output" in needed,
            "image_input": "image_input" in needed, "reasoning": "reasoning" in needed,
            "long_prompt": prompt_tokens >= self.thresholds["long_prompt_tokens"],
            "long_output": output_tokens >= self.thresholds["long_output_tokens"],
            "pin_rerouted": rerouted, "passthrough": passthrough,
        }
        return {"mode": mode, "profile": profile, "actions": list(constraints.actions), "band": band_kind,
                "flags": flags, "values": values}

    # ------------------------------------------------------------------ rendering
    def _applies(self, fragment: Fragment, facts: dict[str, Any]) -> bool:
        when = fragment.when
        if "mode" in when and facts["mode"] not in when["mode"]:
            return False
        if "profile" in when and facts["profile"] not in when["profile"]:
            return False
        if "action" in when and not set(when["action"]) & set(facts["actions"]):
            return False
        if "band" in when and facts["band"] not in when["band"]:
            return False
        if "has" in when and not all(facts["flags"][flag] for flag in when["has"]):
            return False
        return all(facts["values"].get(name) is not None for name in _fields(fragment.text))

    def render(self, candidates: list[ModelEntry], **facts_args: Any) -> Rendered:
        facts = self.facts(candidates=candidates, **facts_args)
        used = [f for f in self.fragments if self._applies(f, facts)]
        texts = {f.id: f.text.format_map(facts["values"]) for f in used}
        dropped: list[str] = []
        while len(" ".join(texts[f.id] for f in used)) > self.max_chars:
            removable = [f for f in used if not f.id.startswith(ALWAYS_KEPT)]
            if not removable:
                break
            used.remove(removable[-1])
            dropped.append(removable[-1].id)
        return Rendered(" ".join(texts[f.id] for f in used), [f.id for f in used],
                        self.criteria(candidates), dropped)

    def criteria(self, candidates: list[ModelEntry]) -> dict[str, str]:
        """Option key -> description, with the option's prices and its price rank among the options.

        The rank follows `tier` (input price, then output price), the same order the routing modes'
        price bands use, so it is stated as such: a model can rank above one with a lower output price."""
        ranked = {m.name: i + 1 for i, m in enumerate(sorted(candidates, key=lambda m: m.tier))}
        out = {}
        for m in candidates:
            priced = m.output_price is not None and m.input_price is not None
            values = {"description": m.description, "price_rank": str(ranked[m.name]),
                      "candidate_count": str(len(candidates)),
                      "input_price": _money(m.input_price) if priced else None,
                      "output_price": _money(m.output_price) if priced else None}
            template = self.criteria_templates["priced" if priced else "unpriced"]
            out[m.name] = template.format_map(values)
        return out
