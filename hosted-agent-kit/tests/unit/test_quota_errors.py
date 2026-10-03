"""Session quota 429s are different from request throttling."""

from __future__ import annotations

from types import SimpleNamespace
from typing import Any

import httpx2
import openai
import pytest
from azure.core.exceptions import HttpResponseError

from hosted_agent_kit.adapters.foundry_sdk import translate_exception
from hosted_agent_kit.domain.errors import (
    QUOTA_REGIONAL,
    QUOTA_SESSION,
    FoundryQuotaExceeded,
    FoundryThrottled,
    RegionalCapacityError,
    SessionQuotaError,
)
from tests.conftest import make_harness, make_request

REQUEST = httpx2.Request("POST", "https://x.invalid")


def azure_error(status: int, code: str | None, headers: dict[str, str] | None = None) -> Any:
    response = SimpleNamespace(status_code=status, headers=headers or {}, reason="x")
    exc = HttpResponseError(message="boom", response=response)  # type: ignore[arg-type]
    if code is not None:
        exc.error = SimpleNamespace(code=code)  # type: ignore[assignment]
    return exc


def openai_error(code: str | None, body: Any = None) -> openai.RateLimitError:
    response = httpx2.Response(429, request=REQUEST, headers={"retry-after": "4"})
    exc = openai.RateLimitError("boom", response=response, body=body)
    exc.code = code
    return exc


@pytest.mark.parametrize(
    ("exc", "scope"),
    [
        (azure_error(429, "regional_session_quota_exceeded"), QUOTA_REGIONAL),
        (azure_error(429, "session_quota_exceeded"), QUOTA_SESSION),
        (azure_error(429, "Regional_Session_Quota_Exceeded"), QUOTA_REGIONAL),
        (openai_error("session_quota_exceeded"), QUOTA_SESSION),
        (
            openai_error(None, {"error": {"code": "regional_session_quota_exceeded"}}),
            QUOTA_REGIONAL,
        ),
        (openai_error(None, {"code": "session_quota_exceeded"}), QUOTA_SESSION),
    ],
)
def test_quota_codes_are_recognised(exc: BaseException, scope: str) -> None:
    translated = translate_exception(exc)
    assert isinstance(translated, FoundryQuotaExceeded)
    assert translated.scope == scope


@pytest.mark.parametrize(
    "exc",
    [azure_error(429, None), azure_error(429, "rate_limit_exceeded"), openai_error("rate_limit")],
)
def test_other_429s_are_still_throttling(exc: BaseException) -> None:
    assert isinstance(translate_exception(exc), FoundryThrottled)


def test_a_quota_code_on_another_status_is_not_a_quota_error() -> None:
    assert not isinstance(
        translate_exception(azure_error(503, "session_quota_exceeded")), FoundryQuotaExceeded
    )


def test_retry_after_is_kept() -> None:
    translated = translate_exception(
        azure_error(429, "regional_session_quota_exceeded", {"retry-after": "7"})
    )
    assert isinstance(translated, FoundryQuotaExceeded) and translated.retry_after_seconds == 7.0


async def test_a_regional_quota_error_on_create_is_retried_then_succeeds() -> None:
    h = make_harness()
    h.fake.create_errors = [FoundryQuotaExceeded(QUOTA_REGIONAL, 0.01)]
    result = await h.pool.execute(make_request())
    assert result.status_code == 200
    assert len(h.fake.created) == 1
    assert h.clock.sleeps  # it waited before the retry


async def test_a_regional_quota_error_that_persists_maps_to_regional_capacity() -> None:
    h = make_harness(defaults={"mode": "stateless", "max_sessions": 2, "create_retries": 1})
    h.fake.create_errors = [FoundryQuotaExceeded(QUOTA_REGIONAL, 0.02)] * 5
    with pytest.raises(RegionalCapacityError) as info:
        await h.pool.execute(make_request())
    assert info.value.retry_after_seconds == 1  # the hint, rounded up
    assert info.value.status == 429 and info.value.retry_safe
    assert len(h.fake.create_requests) == 2  # one try and one retry


async def test_a_session_quota_error_on_create_is_not_retried() -> None:
    h = make_harness()
    h.fake.create_errors = [FoundryQuotaExceeded(QUOTA_SESSION)] * 5
    with pytest.raises(SessionQuotaError) as info:
        await h.pool.execute(make_request())
    assert len(h.fake.create_requests) == 1
    assert info.value.code == "SESSION_QUOTA_EXCEEDED"


async def test_quota_errors_do_not_open_the_circuit_breaker() -> None:
    from hosted_agent_kit.adapters.circuit_breaking import CircuitBreakingAdapter
    from hosted_agent_kit.config.loader import build_config
    from hosted_agent_kit.services.circuit_breaker import CircuitBreakers
    from hosted_agent_kit.services.metrics import InMemoryMetrics
    from tests.fakes.clock import FakeClock
    from tests.fakes.foundry import FakeFoundry

    clock = FakeClock()
    config = build_config(
        {
            "agentPool": {
                "agents": {"a": {"mode": "stateless", "circuit_breaker": {"failure_threshold": 1}}}
            }
        }
    )
    breakers = CircuitBreakers(config, clock, InMemoryMetrics())
    fake = FakeFoundry(clock)
    fake.create_errors = [FoundryQuotaExceeded(QUOTA_REGIONAL)] * 3
    adapter = CircuitBreakingAdapter(fake, breakers)
    for _ in range(3):
        with pytest.raises(FoundryQuotaExceeded):
            await adapter.create_session("a")
    assert breakers.state("a") == "closed"


async def test_a_regional_quota_error_when_resuming_is_retried_on_invoke() -> None:
    h = make_harness()
    await h.pool.execute(make_request())  # creates the session
    h.fake.invoke_errors = [FoundryQuotaExceeded(QUOTA_REGIONAL, 0.01)]
    result = await h.pool.execute(make_request())
    assert result.status_code == 200
    assert len(h.fake.invocations) == 3  # first call, the refused resume, the retry


async def test_a_session_quota_error_when_resuming_is_not_retried() -> None:
    h = make_harness()
    await h.pool.execute(make_request())
    h.fake.invoke_errors = [FoundryQuotaExceeded(QUOTA_SESSION)]
    with pytest.raises(SessionQuotaError):
        await h.pool.execute(make_request())
    assert len(h.fake.invocations) == 2


async def test_the_quota_callback_is_told_about_each_refusal() -> None:
    h = make_harness()
    seen: list[tuple[str, str]] = []
    h.pool.remote._on_quota_exceeded = lambda agent, scope: seen.append((agent, scope))
    h.fake.create_errors = [
        FoundryQuotaExceeded(QUOTA_REGIONAL, 0),
        FoundryQuotaExceeded(QUOTA_REGIONAL, 0),
    ]
    await h.pool.execute(make_request())
    assert seen == [("stateless-agent", QUOTA_REGIONAL)] * 2
