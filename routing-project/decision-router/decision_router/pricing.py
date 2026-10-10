from __future__ import annotations

import json
from pathlib import Path
from typing import Any


class PriceTable:
    def __init__(self, path: Path | None):
        self.data: dict[str, Any] = json.loads(path.read_text()) if path and path.exists() else {}

    @property
    def version(self) -> str | None:
        return self.data.get("version")

    def decision_cost(self, usage: dict[str, Any] | None) -> dict[str, Any]:
        """Decision-1 bills input tokens only."""
        price = (self.data.get("decision1") or {}).get("input_per_million")
        result = {"currency": self.data.get("currency", "USD"), "amount": None, "price_version": self.version}
        tokens = (usage or {}).get("prompt_tokens", (usage or {}).get("input_tokens"))
        if price is not None and isinstance(tokens, int):
            result["amount"] = round(tokens * price / 1_000_000, 10)
        return result

    def cost(self, model: str | None, usage: dict[str, Any] | None) -> dict[str, Any]:
        """Estimated cost of one completion. amount is None whenever a price is missing."""
        record = (self.data.get("models") or {}).get(model or "")
        result = {"currency": self.data.get("currency", "USD"), "amount": None, "price_version": self.version}
        if not record or not usage:
            return result
        input_price, output_price = record.get("input_per_million"), record.get("output_per_million")
        if input_price is None or output_price is None:
            return result
        prompt = int(usage.get("prompt_tokens") or 0)
        completion = int(usage.get("completion_tokens") or 0)
        cached = int((usage.get("prompt_tokens_details") or {}).get("cached_tokens") or 0)
        cached_price = record.get("cached_input_per_million")
        if cached_price is None:
            cached_price, cached = input_price, 0
        amount = ((prompt - cached) * input_price + cached * cached_price + completion * output_price) / 1_000_000
        result["amount"] = round(amount, 10)
        return result
