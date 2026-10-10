import sys
from dataclasses import replace
from pathlib import Path

import httpx
import pytest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

from decision_router.auth import FoundryAuth  # noqa: E402
from decision_router.catalog import Catalog  # noqa: E402
from decision_router.config import Settings  # noqa: E402
from decision_router.decision1 import Decision1Client  # noqa: E402
from decision_router.foundry import ChatClient  # noqa: E402
from decision_router.pipeline import RouterPipeline  # noqa: E402
from decision_router.pricing import PriceTable  # noqa: E402
from decision_router.telemetry import Telemetry  # noqa: E402
from decision_router.testing import FakeFoundry  # noqa: E402

ENDPOINT = "https://unit.services.ai.azure.com"


@pytest.fixture
def root() -> Path:
    return ROOT


@pytest.fixture
def settings() -> Settings:
    return Settings(foundry_endpoint=ENDPOINT, foundry_api_key="test-key", attempts_per_model=2)


@pytest.fixture
def catalog(settings: Settings) -> Catalog:
    return Catalog.load(settings.catalog_path)


@pytest.fixture
def fake(catalog: Catalog) -> FakeFoundry:
    return FakeFoundry([m.deployment for m in catalog.models],
                       {"model-router": [m.name for m in catalog.models]})


@pytest.fixture
def make_pipeline(settings: Settings, catalog: Catalog, fake: FakeFoundry, tmp_path: Path):
    def build(**overrides) -> RouterPipeline:
        s = replace(settings, decision_log_path=tmp_path / "decisions.jsonl", **overrides)
        http = httpx.AsyncClient(transport=fake.transport())
        auth = FoundryAuth("test-key")
        sleeps: list[float] = []

        async def no_sleep(seconds: float) -> None:
            sleeps.append(seconds)

        pipe = RouterPipeline(
            s, catalog,
            Decision1Client(s.resolved_decision1_url, s.decision1_deployment, auth, http=http),
            ChatClient(s.resolved_chat_url, auth, http=http),
            Telemetry(s.decision_log_path), PriceTable(s.pricing_path), sleep=no_sleep,
        )
        pipe.sleeps = sleeps
        pipe.http = http
        return pipe
    return build
