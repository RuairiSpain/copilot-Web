"""Per-caller limits set by the operator, which a request can narrow but never widen.

Request fields (and profiles, which are only defaults) choose a catalog, mode, models and cost
band. A caller policy then bounds that choice for whoever is calling:

  ROUTER_CALLER_POLICIES='{
    "api_key:1":           {"catalogs": ["new"], "max_output_price": 10},
    "entra:<client-id>":   {"modes": ["cost", "balanced"], "models": ["gpt-5.4-mini", "claude-sonnet-5-5"]},
    "*":                   {"max_output_price": 25}
  }'

Keys are `api_key:<n>` (the key's position in ROUTER_API_KEYS), `entra:<client id>`, or `*` for any
caller without its own entry (including unauthenticated callers). A caller with no entry and no `*`
is unrestricted, which is the behaviour before policies existed.

- `catalogs`, `modes`: a request outside these is rejected with 403 `policy_denied`, because the
  router cannot pick a different catalog or mode on the caller's behalf.
- `models`, `max_output_price`: applied in stage 1's allow-list step, after the request's own
  choices, so the request can only narrow them. Models above the price (USD per million output
  tokens) or outside the list are removed, and the decision log says so.
"""
from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from .config import COMPATIBILITIES, ROUTING_MODES

POLICY_FIELDS = ("catalogs", "modes", "models", "max_output_price")


class PolicyError(ValueError):
    pass


@dataclass(frozen=True)
class CallerPolicy:
    name: str
    catalogs: tuple[str, ...] = ()
    modes: tuple[str, ...] = ()
    models: tuple[str, ...] = ()
    max_output_price: float | None = None

    def as_dict(self) -> dict[str, Any]:
        return {"name": self.name, **{k: v for k, v in self.__dict__.items() if k != "name" and v not in ((), None)}}


def _strings(name: str, field: str, value: Any, allowed: tuple[str, ...] | None = None) -> tuple[str, ...]:
    if value is None:
        return ()
    if not isinstance(value, list) or not value or not all(isinstance(v, str) and v for v in value):
        raise PolicyError(f"caller policy '{name}': {field} must be a non-empty list of strings")
    if allowed is not None and set(value) - set(allowed):
        raise PolicyError(f"caller policy '{name}': {field} must be drawn from {list(allowed)}")
    return tuple(value)


def load_policies(raw: dict[str, Any]) -> dict[str, CallerPolicy]:
    policies = {}
    for name, spec in raw.items():
        if name != "*" and not (name.startswith("api_key:") or name.startswith("entra:")):
            raise PolicyError(f"caller policy key '{name}' must be 'api_key:<n>', 'entra:<client id>' or '*'")
        if not isinstance(spec, dict) or set(spec) - set(POLICY_FIELDS):
            raise PolicyError(f"caller policy '{name}' must be an object with keys from {POLICY_FIELDS}")
        price = spec.get("max_output_price")
        if price is not None and (not isinstance(price, (int, float)) or isinstance(price, bool) or price <= 0):
            raise PolicyError(f"caller policy '{name}': max_output_price must be a positive number")
        policies[name.lower() if name.startswith("entra:") else name] = CallerPolicy(
            name,
            _strings(name, "catalogs", spec.get("catalogs"), COMPATIBILITIES),
            _strings(name, "modes", spec.get("modes"), ROUTING_MODES),
            _strings(name, "models", spec.get("models")),
            float(price) if price is not None else None,
        )
    return policies


def policy_for(policies: dict[str, CallerPolicy], caller: dict[str, Any] | None) -> CallerPolicy | None:
    """The caller's own policy, else the `*` policy, else None (unrestricted)."""
    caller = caller or {}
    method, identity = caller.get("method"), caller.get("identity")
    if method in ("api_key", "entra") and identity:
        key = f"{method}:{identity}".lower() if method == "entra" else f"{method}:{identity}"
        if key in policies:
            return policies[key]
    return policies.get("*")
