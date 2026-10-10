from dataclasses import replace

import pytest

from decision_router.stage1 import ConstraintError, Constraints, parse_constraints, requirements_from_request, select

USER = {"messages": [{"role": "user", "content": "hi"}]}


def names(result):
    return [m.name for m in result.candidates]


def removed(result, step):
    return {r["model"]: r["reason"] for s in result.steps if s["step"] == step for r in s["removed"]}


def test_no_constraints_keeps_the_whole_balanced_catalog(catalog, settings):
    result = select(catalog, "balanced", Constraints(), USER, settings)
    assert names(result) == [m.name for m in catalog.models] and result.empty_at is None


def test_preview_models_can_be_excluded(catalog, settings):
    result = select(catalog, "balanced", Constraints(allow_preview=False), USER, settings)
    assert {"grok-4.6", "claude-fable-5-1"} <= set(removed(result, "lifecycle"))
    off = select(catalog, "balanced", Constraints(), USER, replace(settings, allow_preview=False))
    assert "grok-4.6" not in names(off)


def test_operator_allowlist(catalog, settings):
    result = select(catalog, "balanced", Constraints(), USER, replace(settings, model_allowlist=("gpt-5.5", "gpt-5-mini")))
    assert names(result) == ["gpt-5-mini", "gpt-5.5"]


def test_developer_subset_and_providers(catalog, settings):
    subset = select(catalog, "balanced", Constraints(models=("gpt-5.5", "claude-opus-5", "grok-4")), USER, settings)
    assert set(names(subset)) == {"gpt-5.5", "claude-opus-5", "grok-4"}
    anthropic = select(catalog, "balanced", Constraints(providers=("anthropic",)), USER, settings)
    assert names(anthropic) and all(n.startswith("claude-") for n in names(anthropic))
    excluded = select(catalog, "balanced", Constraints(exclude_models=("gpt-5.5",)), USER, settings)
    assert "gpt-5.5" not in names(excluded)


def test_requirements_read_from_the_request():
    body = {"messages": [{"role": "user", "content": [{"type": "text", "text": "what is this"},
                                                       {"type": "image_url", "image_url": {"url": "https://x/y.png"}}]}],
            "tools": [{"type": "function", "function": {"name": "f"}}], "parallel_tool_calls": True,
            "response_format": {"type": "json_schema", "json_schema": {}}, "stream": True}
    assert set(requirements_from_request(body)) == {"tools", "parallel_tools", "structured_output", "streaming",
                                                     "image_input"}


def test_capability_filter_with_unknown_values(new_catalog, settings):
    body = dict(USER, tools=[{"type": "function", "function": {"name": "f"}}])
    strict = select(new_catalog, "balanced", Constraints(), body, settings)
    assert removed(strict, "capabilities")["Phi-4"].startswith("tools no")
    assert "gpt-6.1-sol" not in names(strict)  # tool calling is Responses-API only
    images = select(new_catalog, "balanced", Constraints(capabilities=("image_input",)), USER, settings)
    assert "DeepSeek-V4-Pro" not in names(images) and "gpt-5.5" in names(images)
    lenient = select(new_catalog, "balanced", Constraints(capabilities=("parallel_tools",)), USER,
                     replace(settings, unknown_capability="eligible"))
    assert "FW-GLM-5.3" in names(lenient)  # parallel_tools unknown, allowed when unknowns are eligible
    strict_parallel = select(new_catalog, "balanced", Constraints(capabilities=("parallel_tools",)), USER, settings)
    assert "FW-GLM-5.3" not in names(strict_parallel)


def test_structured_output_keeps_claude_out(catalog, settings):
    body = dict(USER, response_format={"type": "json_schema", "json_schema": {"name": "x", "schema": {}}})
    result = select(catalog, "balanced", Constraints(), body, settings)
    assert not any(n.startswith("claude-") for n in names(result))


def test_region_and_residency(catalog, settings):
    sweden = select(catalog, "balanced", Constraints(region="swedencentral"), USER, settings)
    reasons = removed(sweden, "location")
    assert reasons["FW-GLM-5.3"] == "regions not published"
    assert "gpt-5-nano" in names(sweden)
    in_azure = select(catalog, "balanced", Constraints(inference_in_azure=True), USER, settings)
    assert "claude-opus-4-7" not in names(in_azure) and "FW-Kimi-K3" not in names(in_azure)
    assert "claude-opus-5" in names(in_azure)
    data_zone = select(catalog, "balanced", Constraints(region="swedencentral", deployment_type="data_zone_standard"),
                       USER, settings)
    assert all("swedencentral" in m.regions["data_zone_standard"] for m in data_zone.candidates)


def test_size_filter(new_catalog, settings):
    long_prompt = {"messages": [{"role": "user", "content": "x" * 120_000}]}
    result = select(new_catalog, "balanced", Constraints(), long_prompt, settings)
    assert "Phi-4" not in names(result) and "gpt-5.5" in names(result)
    big_output = dict(USER, max_completion_tokens=20_000)
    assert "Phi-4-mini-instruct" not in names(select(new_catalog, "balanced", Constraints(), big_output, settings))
    assert "Phi-4-mini-instruct" not in names(select(new_catalog, "balanced", Constraints(min_context_tokens=200_000),
                                                     USER, settings))


def test_price_band_applies_after_the_other_filters(catalog, settings):
    subset = Constraints(models=("gpt-5-nano", "gpt-5.5", "claude-opus-5"))
    assert names(select(catalog, "cost", subset, USER, settings)) == ["gpt-5-nano"]
    assert names(select(catalog, "quality", subset, USER, settings)) == ["claude-opus-5"]


def test_empty_result_names_the_step(new_catalog, settings):
    result = select(new_catalog, "balanced", Constraints(models=("Phi-4",), capabilities=("tools",)), USER, settings)
    assert result.candidates == [] and result.empty_at == "capabilities"


@pytest.mark.parametrize("raw, message", [
    ({"model": ["x"]}, "unknown routing_constraints keys"),
    ({"models": ["gpt-9"]}, "not in the 'old' catalog"),
    ({"models": "gpt-5.5"}, "list of strings"),
    ({"capabilities": ["telepathy"]}, "unknown capabilities"),
    ({"deployment_type": "ptu"}, "deployment_type"),
    ({"min_context_tokens": -1}, "positive integer"),
    ({"inference_in_azure": "yes"}, "true or false"),
    ("everything", "must be an object"),
])
def test_invalid_constraints(catalog, raw, message):
    with pytest.raises(ConstraintError, match=message):
        parse_constraints(raw, catalog)


def test_region_names_are_normalised(catalog):
    assert parse_constraints({"region": "Sweden Central"}, catalog).region == "swedencentral"
