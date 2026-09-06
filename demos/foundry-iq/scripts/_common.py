"""Shared helpers for the Foundry IQ demo.

Everything here is deliberately thin: the demo's value is in showing the real
REST payloads that Foundry IQ takes, so the scripts call the search service's
control and data planes directly rather than hiding them behind a wrapper.

Auth is Entra-only (no keys). The search service expects a token for the
`https://search.azure.com/.default` scope; the same `DefaultAzureCredential`
that `az login` populates works for local runs and for a managed identity in
CI without changes.
"""

from __future__ import annotations

import json
import os
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import requests
from azure.identity import DefaultAzureCredential

SEARCH_SCOPE = "https://search.azure.com/.default"

# Agentic retrieval is GA on 2026-04-01, but knowledge-source kinds beyond the
# GA set, answer synthesis, and the `activity` token counters the trace viewer
# renders are preview-only — so the demo pins the preview surface throughout.
API_VERSION = os.environ.get("SEARCH_API_VERSION", "2026-08-01-preview")

REPO_ROOT = Path(__file__).resolve().parents[1]
CORPUS_DIR = REPO_ROOT / "corpus"


class ConfigError(RuntimeError):
    """Raised when required configuration is missing, with a fix in the text."""


def odata(name: str) -> str:
    """Quote a resource name for a search data-plane path.

    The data plane is OData-shaped: every addressable resource is
    `/collection('name')`, not `/collection/name` — see the paths in
    search.json for 2026-08-01-preview. A single quote inside a name is
    escaped by doubling it, per OData literal rules.
    """
    return "('" + name.replace("'", "''") + "')"


@dataclass(frozen=True)
class Settings:
    search_endpoint: str
    aoai_endpoint: str
    aoai_embedding_deployment: str
    aoai_embedding_model: str
    aoai_chat_deployment: str
    aoai_chat_model: str
    storage_account: str
    storage_container: str
    storage_resource_id: str
    knowledge_base: str
    blob_source: str
    index_source: str
    hr_index: str

    @property
    def kb_path(self) -> str:
        """Path of the knowledge base, OData-quoted."""
        return f"/knowledgebases{odata(self.knowledge_base)}"

    @property
    def kb_url(self) -> str:
        return f"{self.search_endpoint}{self.kb_path}"


def _require(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise ConfigError(f"{name} is not set — copy .env.example to .env and fill it in, then `set -a && . ./.env`")
    return value


def load_settings() -> Settings:
    return Settings(
        search_endpoint=_require("SEARCH_ENDPOINT").rstrip("/"),
        aoai_endpoint=_require("AOAI_ENDPOINT").rstrip("/"),
        aoai_embedding_deployment=os.environ.get("AOAI_EMBEDDING_DEPLOYMENT", "text-embedding-3-large"),
        aoai_embedding_model=os.environ.get("AOAI_EMBEDDING_MODEL", "text-embedding-3-large"),
        aoai_chat_deployment=os.environ.get("AOAI_CHAT_DEPLOYMENT", "gpt-5.4-mini"),
        aoai_chat_model=os.environ.get("AOAI_CHAT_MODEL", "gpt-5.4-mini"),
        storage_account=os.environ.get("STORAGE_ACCOUNT", ""),
        storage_container=os.environ.get("STORAGE_CONTAINER", "eu-directives"),
        storage_resource_id=os.environ.get("STORAGE_RESOURCE_ID", ""),
        knowledge_base=os.environ.get("KNOWLEDGE_BASE", "es-employment-kb"),
        blob_source=os.environ.get("BLOB_KNOWLEDGE_SOURCE", "eu-directives-ks"),
        index_source=os.environ.get("INDEX_KNOWLEDGE_SOURCE", "hr-templates-ks"),
        hr_index=os.environ.get("HR_INDEX", "hr-templates-index"),
    )


class SearchClient:
    """Bearer-authenticated REST client for the search service.

    Tokens are cached until shortly before expiry — a provisioning run makes
    a dozen calls and re-minting a token per call is pure latency.
    """

    def __init__(self, endpoint: str, credential: DefaultAzureCredential | None = None) -> None:
        self.endpoint = endpoint.rstrip("/")
        self._credential = credential or DefaultAzureCredential()
        self._token: str | None = None
        self._expires_at: float = 0.0

    def _headers(self) -> dict[str, str]:
        if self._token is None or time.time() > self._expires_at - 120:
            token = self._credential.get_token(SEARCH_SCOPE)
            self._token, self._expires_at = token.token, float(token.expires_on)
        return {"Authorization": f"Bearer {self._token}", "Content-Type": "application/json"}

    def request(self, method: str, path: str, body: Any | None = None, *, timeout: int = 120) -> Any:
        url = f"{self.endpoint}/{path.lstrip('/')}"
        sep = "&" if "?" in url else "?"
        response = requests.request(
            method,
            f"{url}{sep}api-version={API_VERSION}",
            headers=self._headers(),
            json=body,
            timeout=timeout,
        )
        if not response.ok:
            raise RuntimeError(f"{method} {path} -> HTTP {response.status_code}: {response.text[:1500]}")
        if not response.content:
            return None
        return response.json()

    def put(self, path: str, body: Any) -> Any:
        return self.request("PUT", path, body)

    def post(self, path: str, body: Any, *, timeout: int = 120) -> Any:
        return self.request("POST", path, body, timeout=timeout)

    def get(self, path: str) -> Any:
        return self.request("GET", path)


def embed(texts: list[str], settings: Settings, credential: DefaultAzureCredential | None = None) -> list[list[float]]:
    """Client-side embeddings for the documents we push into the HR index.

    Query-time vectorisation is handled server-side by the vectorizer declared
    on the index's vector profile, so callers only ever send text.
    """
    credential = credential or DefaultAzureCredential()
    token = credential.get_token("https://cognitiveservices.azure.com/.default").token
    url = f"{settings.aoai_endpoint}/openai/v1/embeddings"
    response = requests.post(
        url,
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
        json={"model": settings.aoai_embedding_deployment, "input": texts},
        timeout=180,
    )
    if not response.ok:
        raise RuntimeError(f"embeddings -> HTTP {response.status_code}: {response.text[:800]}")
    payload = response.json()
    return [item["embedding"] for item in sorted(payload["data"], key=lambda d: d["index"])]


def dump(label: str, payload: Any) -> None:
    """Echo a REST payload so the demo audience sees the real request."""
    print(f"\n\033[2m--- {label} ---\033[0m")
    print(json.dumps(payload, indent=2)[:4000])


def fail(message: str) -> None:
    print(f"\033[31merror:\033[0m {message}", file=sys.stderr)
    raise SystemExit(1)
