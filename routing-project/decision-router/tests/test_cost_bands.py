import pytest
from fastapi.testclient import TestClient

from decision_router.app import create_app
from decision_router.stage1 import ConstraintError, Constraints, parse_cost_band, select
from decision_router.testing import ranked

USER = {"messages": [{"role": "user", "content": "hi"}]}
LOW = ["Phi-4-mini-instruct", "FW-GLM-5.3-Flash", "claude-haiku-5-5", "gpt-5.4-nano", "Phi-4", "gpt-oss-120b",
       "Phi-4-reasoning", "Llama-4-Maverick-17B-128E-Instruct-FP8", "grok-4-1-fast-reasoning", "DeepSeek-V4-Flash"]


def band(new_catalog, settings, raw, constraints=None):
    result = select(new_catalog, "balanced", constraints or Constraints(), USER, settings,
                    cost_band=parse_cost_band(raw, new_catalog))
    return [m.name for m in result.candidates], result


def test_named_bands_on_output_price(new_catalog, settings):
    assert band(new_catalog, settings, "low")[0] == LOW
    assert band(new_catalog, settings, "medium")[0] == ["gpt-5.4-mini", "gpt-6-luna", "FW-GLM-5.3", "FW-Kimi-K3"]
    assert band(new_catalog, settings, "high")[0] == ["claude-sonnet-5-5", "gpt-6.1-sol", "gpt-5.5", "grok-4.7",
                                                       "claude-opus-5-5"]
    assert band(new_catalog, settings, "TOP")[0] == ["gpt-6-astra"]


def test_models_in_the_gaps_are_in_no_named_band(new_catalog, settings):
    named = {n for b in ("low", "medium", "high", "top") for n in band(new_catalog, settings, b)[0]}
    assert {m.name for m in new_catalog.models} - named == {"DeepSeek-V4-Pro", "MAI-Thinking-1", "gpt-5.6-terra"}
    assert band(new_catalog, settings, "3-10")[0] == ["gpt-5.4-mini", "DeepSeek-V4-Pro", "gpt-5.6-terra",
                                                       "MAI-Thinking-1", "claude-sonnet-5-5", "gpt-6.1-sol"]


@pytest.mark.parametrize("raw", ["0-1", "$0-$1", "0..1", {"min": 0, "max": 1}, {"max": 1}, "-1"])
def test_dollar_ranges_equal_to_the_low_band(new_catalog, settings, raw):
    assert band(new_catalog, settings, raw)[0] == LOW


def test_open_ended_range_and_old_catalog(catalog, new_catalog, settings):
    assert band(new_catalog, settings, "30-")[0] == ["gpt-6-astra"]
    old = select(catalog, "balanced", Constraints(), USER, settings, cost_band=parse_cost_band("30-60", catalog))
    assert [m.name for m in old.candidates] == ["claude-fable-5-1", "gpt-6-astra"]


def test_band_runs_after_the_other_filters_and_can_empty(new_catalog, settings):
    names, result = band(new_catalog, settings, "top", Constraints(capabilities=("image_input",)))
    assert names == ["gpt-6-astra"]
    names, result = band(new_catalog, settings, "top", Constraints(providers=("anthropic",)))
    assert names == [] and result.empty_at == "price_band"
    assert result.steps[-1]["removed"][0]["reason"] == "outside cost band top ($30-$60)"


@pytest.mark.parametrize("raw, message", [
    ("cheap", r"band name \(low, medium, high, top\)"),
    ("3-1", "min is above max"),
    ({"min": 1, "low": 2}, '"min" and/or "max"'),
    ({"min": -1}, "non-negative"),
    ({"max": True}, "non-negative"),
    (5, "string or an object"),
])
def test_invalid_cost_bands(new_catalog, raw, message):
    with pytest.raises(ConstraintError, match=message):
        parse_cost_band(raw, new_catalog)


def test_old_catalog_has_no_named_bands(catalog):
    with pytest.raises(ConstraintError, match="'old' catalog has no named bands"):
        parse_cost_band("low", catalog)


def test_request_field_and_profiles(make_pipeline, settings, fake):
    pipe = make_pipeline()
    assert pipe.prepare({"model": "decision-router-top", "messages": USER["messages"]}).cost_band.name == "top"
    fake.decision_responses.append(ranked(["gpt-5.5", "claude-opus-5-5"]))
    with TestClient(create_app(settings, pipeline=make_pipeline())) as c:
        routed = c.post("/v1/route", json={**USER, "compatibility": "new", "routing_cost_band": "high"}).json()
        bad = c.post("/v1/route", json={**USER, "routing_cost_band": "low"})  # default catalog is old
    assert routed["cost_band"] == {"name": "high", "min": 10, "max": 25}
    assert routed["candidates"] == ["claude-sonnet-5-5", "gpt-6.1-sol", "gpt-5.5", "grok-4.7", "claude-opus-5-5"]
    assert "routing_cost_band" not in fake.requests[-1]["body"]
    assert bad.status_code == 400 and bad.json()["error"]["code"] == "invalid_routing_cost_band"
