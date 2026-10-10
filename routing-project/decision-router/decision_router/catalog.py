"""Model pool and stage 1: the deterministic filter from routing mode to candidates."""
from __future__ import annotations

import hashlib
import json
import re
from dataclasses import dataclass
from pathlib import Path

from .config import ROUTING_MODES


@dataclass(frozen=True)
class ModelEntry:
    name: str
    deployment: str
    tier: int
    description: str
    aliases: tuple[str, ...] = ()


@dataclass(frozen=True)
class RoutingMode:
    name: str
    models: tuple[str, ...]
    instructions: str


class CatalogError(ValueError):
    pass


class Catalog:
    def __init__(self, models: list[ModelEntry], modes: dict[str, RoutingMode], default_mode: str,
                 fingerprint: str = ""):
        self.models = sorted(models, key=lambda m: m.tier)
        self.by_name = {m.name: m for m in self.models}
        self.modes = modes
        self.default_mode = default_mode
        self.fingerprint = fingerprint
        self._validate()

    @classmethod
    def load(cls, path: Path, deployment_overrides: dict[str, str] | None = None,
             default_mode: str | None = None) -> Catalog:
        raw_bytes = path.read_bytes()
        data = json.loads(raw_bytes)
        overrides = deployment_overrides or {}
        unknown = set(overrides) - {m["name"] for m in data["models"]}
        if unknown:
            raise CatalogError(f"ROUTER_DEPLOYMENT_MAP names models not in the catalog: {sorted(unknown)}")
        models = [
            ModelEntry(
                name=m["name"],
                deployment=overrides.get(m["name"], m.get("deployment") or m["name"]),
                tier=int(m["tier"]),
                description=m["description"].strip(),
                aliases=tuple(m.get("aliases", [])),
            )
            for m in data["models"]
        ]
        modes = {
            name: RoutingMode(name, tuple(spec["models"]), spec["instructions"].strip())
            for name, spec in data["routing_modes"].items()
        }
        return cls(models, modes, default_mode or data.get("default_mode", "balanced"),
                   hashlib.sha256(raw_bytes).hexdigest())

    def _validate(self) -> None:
        names = [m.name for m in self.models]
        if len(set(names)) != len(names):
            raise CatalogError("model names must be unique")
        tiers = [m.tier for m in self.models]
        if len(set(tiers)) != len(tiers):
            raise CatalogError("model tiers must be unique so that ranking ties break deterministically")
        if any(not m.description for m in self.models):
            raise CatalogError("every model needs a description; Decision-1 reads it")
        if set(self.modes) != set(ROUTING_MODES):
            raise CatalogError(f"routing_modes must define exactly {ROUTING_MODES}")
        for mode in self.modes.values():
            if not mode.models:
                raise CatalogError(f"routing mode '{mode.name}' allows no models")
            missing = set(mode.models) - set(self.by_name)
            if missing:
                raise CatalogError(f"routing mode '{mode.name}' names unknown models: {sorted(missing)}")
            if len(set(mode.models)) != len(mode.models):
                raise CatalogError(f"routing mode '{mode.name}' lists a model twice")
            if not mode.instructions:
                raise CatalogError(f"routing mode '{mode.name}' has no instructions")
        if self.default_mode not in self.modes:
            raise CatalogError(f"default_mode must be one of {ROUTING_MODES}")

    def candidates(self, mode: str) -> list[ModelEntry]:
        """Stage 1. Same mode in, same ordered candidate list out (cheapest first)."""
        if mode not in self.modes:
            raise CatalogError(f"routing_mode must be one of {list(self.modes)}")
        allowed = set(self.modes[mode].models)
        return [m for m in self.models if m.name in allowed]

    def criteria(self, candidates: list[ModelEntry]) -> dict[str, str]:
        """Option key -> description, with each model's price rank in the whole pool."""
        total = len(self.models)
        rank = {m.name: i + 1 for i, m in enumerate(self.models)}
        return {
            m.name: f"{m.description} Price rank {rank[m.name]} of {total} in the pool (1 is cheapest)."
            for m in candidates
        }

    def resolve(self, served: str | None) -> str | None:
        """Map a model string returned by Foundry (often version-suffixed) to a pool name."""
        if not served:
            return None
        value = served.strip().lower()
        for m in self.models:
            names = {m.name.lower(), m.deployment.lower(), *(a.lower() for a in m.aliases)}
            if value in names:
                return m.name
        # Longest name first, so "gpt-5-mini-2025-08-07" is not claimed by a shorter "gpt-5" entry.
        for m in sorted(self.models, key=lambda e: len(e.name), reverse=True):
            if re.fullmatch(re.escape(m.name.lower()) + r"-\d{4}-\d{2}-\d{2}", value):
                return m.name
        return None
