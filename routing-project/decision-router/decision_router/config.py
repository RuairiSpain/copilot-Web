from __future__ import annotations

import json
import os
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

PACKAGE_ROOT = Path(__file__).resolve().parents[1]
ROUTING_MODES = ("cost", "balanced", "quality")


def _bool(name: str, default: bool) -> bool:
    raw = os.getenv(name)
    return default if raw is None else raw.strip().lower() in {"1", "true", "yes", "on"}


def _json_object(name: str) -> dict[str, Any]:
    raw = os.getenv(name)
    if not raw:
        return {}
    value = json.loads(raw)
    if not isinstance(value, dict):
        raise ValueError(f"{name} must contain a JSON object")
    return value


@dataclass(frozen=True)
class Settings:
    # Foundry resource root, e.g. https://<resource>.services.ai.azure.com
    foundry_endpoint: str | None = None
    # When no key is set, requests authenticate with Microsoft Entra ID (managed identity, CLI, ...).
    foundry_api_key: str | None = None
    decision1_endpoint: str | None = None
    decision1_deployment: str = "microsoft-decision-1"
    decision1_api_key: str | None = None
    chat_completions_url: str | None = None
    catalog_path: Path = PACKAGE_ROOT / "config/model_catalog.json"
    pricing_path: Path = PACKAGE_ROOT / "config/model_pricing.json"
    deployment_overrides: dict[str, str] = field(default_factory=dict)
    default_mode: str | None = None
    state_max_chars: int = 24_000
    decision_timeout_seconds: float = 10.0
    decision_max_attempts: int = 3
    request_timeout_seconds: float = 60.0
    total_timeout_seconds: float = 90.0
    attempts_per_model: int = 2
    max_retry_after_seconds: float = 10.0
    decision_log_path: Path | None = None
    log_prompts: bool = False

    @classmethod
    def from_env(cls) -> Settings:
        log_path = os.getenv("ROUTER_DECISION_LOG_PATH")
        settings = cls(
            foundry_endpoint=os.getenv("FOUNDRY_ENDPOINT"),
            foundry_api_key=os.getenv("FOUNDRY_API_KEY"),
            decision1_endpoint=os.getenv("DECISION1_ENDPOINT"),
            decision1_deployment=os.getenv("DECISION1_DEPLOYMENT", "microsoft-decision-1"),
            decision1_api_key=os.getenv("DECISION1_API_KEY"),
            chat_completions_url=os.getenv("FOUNDRY_CHAT_COMPLETIONS_URL"),
            catalog_path=Path(os.getenv("ROUTER_CATALOG_PATH", str(PACKAGE_ROOT / "config/model_catalog.json"))),
            pricing_path=Path(os.getenv("ROUTER_PRICING_PATH", str(PACKAGE_ROOT / "config/model_pricing.json"))),
            deployment_overrides={str(k): str(v) for k, v in _json_object("ROUTER_DEPLOYMENT_MAP").items()},
            default_mode=os.getenv("ROUTER_DEFAULT_MODE") or None,
            state_max_chars=int(os.getenv("ROUTER_STATE_MAX_CHARS", "24000")),
            decision_timeout_seconds=float(os.getenv("ROUTER_DECISION_TIMEOUT_SECONDS", "10")),
            decision_max_attempts=int(os.getenv("ROUTER_DECISION_MAX_ATTEMPTS", "3")),
            request_timeout_seconds=float(os.getenv("ROUTER_REQUEST_TIMEOUT_SECONDS", "60")),
            total_timeout_seconds=float(os.getenv("ROUTER_TOTAL_TIMEOUT_SECONDS", "90")),
            attempts_per_model=int(os.getenv("ROUTER_ATTEMPTS_PER_MODEL", "2")),
            max_retry_after_seconds=float(os.getenv("ROUTER_MAX_RETRY_AFTER_SECONDS", "10")),
            decision_log_path=Path(log_path) if log_path else None,
            log_prompts=_bool("ROUTER_LOG_PROMPTS", False),
        )
        settings.validate()
        return settings

    def validate(self) -> None:
        if self.default_mode is not None and self.default_mode not in ROUTING_MODES:
            raise ValueError(f"ROUTER_DEFAULT_MODE must be one of {ROUTING_MODES}")
        if self.state_max_chars < 1000:
            raise ValueError("ROUTER_STATE_MAX_CHARS must be at least 1000")
        if self.decision_max_attempts < 1:
            raise ValueError("ROUTER_DECISION_MAX_ATTEMPTS must be at least 1")
        if self.attempts_per_model < 1:
            raise ValueError("ROUTER_ATTEMPTS_PER_MODEL must be at least 1")
        for name in ("decision_timeout_seconds", "request_timeout_seconds", "total_timeout_seconds"):
            if getattr(self, name) <= 0:
                raise ValueError(f"{name} must be positive")
        if self.max_retry_after_seconds < 0:
            raise ValueError("ROUTER_MAX_RETRY_AFTER_SECONDS must not be negative")

    @property
    def resolved_decision1_url(self) -> str | None:
        if self.decision1_endpoint:
            return self.decision1_endpoint
        if self.foundry_endpoint:
            return f"{self.foundry_endpoint.rstrip('/')}/providers/microsoft/v1/systemone"
        return None

    @property
    def resolved_chat_url(self) -> str | None:
        if self.chat_completions_url:
            return self.chat_completions_url
        if self.foundry_endpoint:
            return f"{self.foundry_endpoint.rstrip('/')}/openai/v1/chat/completions"
        return None

    @property
    def configured(self) -> bool:
        return bool(self.resolved_decision1_url and self.resolved_chat_url)
