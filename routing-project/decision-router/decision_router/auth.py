from __future__ import annotations

from typing import Any

COGNITIVE_SERVICES_SCOPE = "https://cognitiveservices.azure.com/.default"
# Claude's Messages API on Foundry takes Entra tokens for this scope (Foundry Claude how-to).
AI_AZURE_SCOPE = "https://ai.azure.com/.default"


class FoundryAuth:
    """API key when one is configured, otherwise a Microsoft Entra ID bearer token."""

    def __init__(self, api_key: str | None = None, credential: Any = None):
        self.api_key = api_key
        self._credential = credential
        self._owns_credential = credential is None

    async def headers(self, *, key_header: str = "api-key", scope: str = COGNITIVE_SERVICES_SCOPE) -> dict[str, str]:
        if self.api_key:
            return {key_header: self.api_key}
        if self._credential is None:
            try:
                from azure.identity.aio import DefaultAzureCredential
            except ImportError as exc:  # pragma: no cover - depends on the image
                raise RuntimeError("no API key is configured and azure-identity is not installed") from exc
            self._credential = DefaultAzureCredential()
        token = await self._credential.get_token(scope)
        return {"Authorization": f"Bearer {token.token}"}

    async def close(self) -> None:
        if self._credential is not None and self._owns_credential:
            await self._credential.close()
