import pytest
from fastapi.testclient import TestClient

from decision_router.app import create_app
from decision_router.stage1 import (
    ConstraintError,
    Constraints,
    action_cost_groups,
    parse_constraints,
    parse_cost_band,
    select,
)
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


def test_medium_high_closes_the_gap(new_catalog, settings):
    assert band(new_catalog, settings, "medium-high")[0] == ["gpt-5.4-mini", "DeepSeek-V4-Pro", "gpt-5.6-terra",
                                                              "MAI-Thinking-1", "claude-sonnet-5-5", "gpt-6.1-sol"]
    assert band(new_catalog, settings, "medium-high")[0] == band(new_catalog, settings, "3-10")[0]
    named = {n for b in new_catalog.cost_bands for n in band(new_catalog, settings, b)[0]}
    assert named == {m.name for m in new_catalog.models}  # every model is in at least one named band


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
    assert result.steps[-1]["removed"][0]["reason"] == "outside cost band top ($25-$60)"


@pytest.mark.parametrize("raw, message", [
    ("cheap", r"band name \(low, medium, medium-high, high, top\)"),
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
    assert pipe.prepare({"model": "decision-router-medium-high", "messages": USER["messages"]}).cost_band.max == 10
    fake.decision_responses.append(ranked(["gpt-5.5", "claude-opus-5-5"]))
    with TestClient(create_app(settings, pipeline=make_pipeline())) as c:
        routed = c.post("/v1/route", json={**USER, "compatibility": "new", "routing_cost_band": "high"}).json()
        bad = c.post("/v1/route", json={**USER, "routing_cost_band": "low"})  # default catalog is old
    assert routed["cost_band"] == {"name": "high", "min": 10, "max": 25}
    assert routed["candidates"] == ["claude-sonnet-5-5", "gpt-6.1-sol", "gpt-5.5", "grok-4.7", "claude-opus-5-5"]
    assert "routing_cost_band" not in fake.requests[-1]["body"]
    assert bad.status_code == 400 and bad.json()["error"]["code"] == "invalid_routing_cost_band"


def grouped(new_catalog, settings, action, label, **extra):
    constraints = parse_constraints({"actions": [action], **extra}, new_catalog)
    result = select(new_catalog, "balanced", constraints, USER, settings, cost_band=parse_cost_band(label, new_catalog))
    return sorted(m.name for m in result.candidates)


def test_action_labels_split_the_action_into_even_groups(new_catalog, settings):
    assert grouped(new_catalog, settings, "plan", "low") == ["DeepSeek-V4-Pro", "FW-GLM-5.3"]
    assert grouped(new_catalog, settings, "plan", "top") == ["claude-opus-5-5", "gpt-6-astra"]
    # write's top group is $15-$20: relative to the action, not the dollar band ($25-$60) where write has nothing
    assert grouped(new_catalog, settings, "write", "top") == ["claude-opus-5-5", "grok-4.7"]
    # models in the dollar gap ($3-$10) are in a group
    assert grouped(new_catalog, settings, "code", "medium") == ["DeepSeek-V4-Pro", "FW-GLM-5.3", "FW-Kimi-K3",
                                                                "gpt-5.4-mini"]


def test_equal_prices_stay_in_one_group(new_catalog):
    for action in ("code", "orchestrate", "reason", "summarize"):
        pool = [m for m in new_catalog.models if action in m.capabilities["actions"]]
        groups = action_cost_groups(pool)
        assert sum(map(len, groups)) == len(pool) and min(map(len, groups)) >= 2
        for low, high in zip(groups, groups[1:], strict=False):
            assert max(m.output_price for m in low) < min(m.output_price for m in high), action


def test_medium_high_with_an_action_joins_the_middle_groups(new_catalog, settings):
    both = grouped(new_catalog, settings, "code", "medium-high")
    assert both == sorted(grouped(new_catalog, settings, "code", "medium") + grouped(new_catalog, settings, "code", "high"))
    assert grouped(new_catalog, settings, "write", "medium-high") == ["Llama-4-Maverick-17B-128E-Instruct-FP8", "Phi-4",
                                                                      "gpt-5.4-nano", "gpt-6-luna"]
    # translate has one group: every label, medium-high included, returns it
    assert grouped(new_catalog, settings, "translate", "medium-high") == grouped(new_catalog, settings, "translate", "low")


def test_fewer_groups_share_labels(new_catalog, settings):
    # evaluate has 7 models -> 3 groups: medium and high share the middle one
    assert grouped(new_catalog, settings, "evaluate", "medium") == grouped(new_catalog, settings, "evaluate", "high")
    assert grouped(new_catalog, settings, "summarize", "low") == ["FW-GLM-5.3-Flash", "Phi-4-mini-instruct"]
    assert grouped(new_catalog, settings, "translate", "top") == grouped(new_catalog, settings, "translate", "low")


def test_groups_are_formed_after_the_other_filters(new_catalog, settings):
    # all 14 code models: gpt-6-astra shares the top group; with tools required, gpt-5.6-terra drops out first
    everything = grouped(new_catalog, settings, "code", "high")
    with_tools = grouped(new_catalog, settings, "code", "high", capabilities=["tools"])
    assert "gpt-5.6-terra" in everything and "gpt-5.6-terra" not in with_tools and everything != with_tools


def test_actions_filter_and_dollar_ranges(new_catalog, settings):
    plan = select(new_catalog, "balanced", parse_constraints({"actions": ["plan", "code"]}, new_catalog), USER, settings)
    assert sorted(m.name for m in plan.candidates) == ["DeepSeek-V4-Pro", "FW-GLM-5.3", "MAI-Thinking-1",
                                                       "claude-opus-5-5", "gpt-5.5", "gpt-5.6-terra", "gpt-6-astra"]
    # a dollar range with actions stays a dollar range
    assert grouped(new_catalog, settings, "write", "10-25") == ["claude-opus-5-5", "grok-4.7"]


def test_invalid_actions(catalog, new_catalog):
    with pytest.raises(ConstraintError, match="unknown actions"):
        parse_constraints({"actions": ["review"]}, new_catalog)
    with pytest.raises(ConstraintError, match="'old' catalog has no action tags"):
        parse_constraints({"actions": ["code"]}, catalog)
