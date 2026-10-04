from __future__ import annotations

import pytest
from azure.core.credentials import AccessToken

PROJECT = "https://acct.services.ai.azure.com/api/projects/proj"
TOOLBOX = f"{PROJECT}/toolboxes/search-and-code/mcp?api-version=v1"


class StubCredential:
    """A credential that never touches the network."""

    def get_token(self, *scopes: str, **kwargs: object) -> AccessToken:
        return AccessToken("stub-token", 9_999_999_999)


@pytest.fixture
def credential() -> StubCredential:
    return StubCredential()


@pytest.fixture
def env() -> dict[str, str]:
    return {
        "FOUNDRY_PROJECT_ENDPOINT": PROJECT,
        "AZURE_AI_MODEL_DEPLOYMENT_NAME": "gpt-5.4-mini",
        "TOOLBOX_ENDPOINT": TOOLBOX,
    }
