import json
import re
import sys
from dataclasses import replace
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from decision_router.app import create_app
from decision_router.catalog import CAPABILITY_TAGS
from decision_router.instructions import InstructionTemplates, TemplateError
from decision_router.stage1 import Constraints, parse_constraints, parse_cost_band, select
from decision_router.testing import ranked

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
from stage1_examples import run  # noqa: E402
from stage1_examples_data import EXAMPLES  # noqa: E402

USER = {"messages": [{"role": "user", "content": "hi"}]}
TOOL = {"type": "function", "function": {"name": "run_tests", "parameters": {"type": "object"}}}


@pytest.fixture
def templates(settings):
    return InstructionTemplates.load(settings.instructions_path)


def render(templates, catalog, settings, body=USER, mode="balanced", constraints=None, band=None, **extra):
    constraints = constraints or Constraints()
    cost_band = parse_cost_band(band, catalog) if band else None
    result = select(catalog, mode, constraints, body, settings, passthrough=extra.get("passthrough", False),
                    cost_band=cost_band)
    return templates.render(result.candidates, mode=mode, constraints=constraints, cost_band=cost_band,
                            requirements=result.requirements, body=body, chars_per_token=settings.chars_per_token,
                            **extra)


@pytest.mark.parametrize("mode", ["cost", "balanced", "quality"])
def test_a_plain_request_renders_the_catalogs_mode_instructions(templates, catalogs, settings, mode):
    for catalog in catalogs.catalogs.values():
        rendered = render(templates, catalog, settings, mode=mode)
        assert rendered.instructions == catalog.modes[mode].instructions
        assert rendered.fragments == ["base", f"mode.{mode}"]


def test_action_band_and_tools_fill_their_placeholders(templates, new_catalog, settings):
    body = dict(USER, tools=[TOOL])
    rendered = render(templates, new_catalog, settings, body=body, band="medium",
                      constraints=parse_constraints({"actions": ["code"]}, new_catalog))
    assert rendered.fragments == ["base", "mode.balanced", "action.code", "band.group", "req.tools"]
    assert "the medium price group of the models suited to coding: 3 options costing $2.50 to $3" in rendered.instructions
    assert "tools (run_tests)" in rendered.instructions


def test_named_band_and_dollar_range(templates, new_catalog, settings):
    named = render(templates, new_catalog, settings, band="high")
    assert "the high cost band: the 5 options cost $10 to $20 per million output tokens" in named.instructions
    ranged = render(templates, new_catalog, settings, band="2.5-33.33")
    assert "output prices of $2.50 to $33.33 per million tokens" in ranged.instructions
    open_ended = render(templates, new_catalog, settings, band="10-")
    assert "output prices of $10 and above per million tokens: the 6 options cost $10 to $50." in open_ended.instructions


def test_size_reasoning_profile_and_passthrough(templates, new_catalog, settings):
    long_body = {"messages": [{"role": "user", "content": "x" * 400_000}], "max_completion_tokens": 20_000}
    sized = render(templates, new_catalog, settings, body=long_body)
    assert {"size.long_prompt", "size.long_output"} <= set(sized.fragments)
    assert re.search(r"about 100,0\d\d tokens", sized.instructions) and "run to 20,000 tokens" in sized.instructions
    reasoning = render(templates, new_catalog, settings, constraints=Constraints(capabilities=("reasoning",)))
    assert "req.reasoning" in reasoning.fragments
    coding = render(templates, new_catalog, settings, profile="decision-router-coding")
    assert "profile.coding" in coding.fragments
    native = {"messages": [{"role": "user", "content": [{"type": "text", "text": "hi"}]}], "max_tokens": 100}
    assert "passthrough" in render(templates, new_catalog, settings, body=native, passthrough=True).fragments


def test_pin_reroute_names_the_previous_model(templates, new_catalog, settings):
    pin = {"status": "rerouted", "previous_model": "Phi-4", "reason": "tools no (needed: request has tools)"}
    rendered = render(templates, new_catalog, settings, body=dict(USER, tools=[TOOL]), pin=pin)
    assert "answered by Phi-4, which can no longer serve it (tools no (needed: request has tools))" in rendered.instructions
    followed = render(templates, new_catalog, settings, pin={"status": "followed", "model": "gpt-5.5"})
    assert "pin.rerouted" not in followed.fragments


def test_every_fragment_is_used_and_no_placeholder_is_left(templates, new_catalog, settings):
    used = set()
    for example in EXAMPLES:
        out = run(example)
        if out.get("rendered"):
            used |= set(out["rendered"].fragments)
            assert not re.search(r"[{}]", out["rendered"].instructions), example["title"]
            assert len(out["rendered"].instructions) <= templates.max_chars
    for action in CAPABILITY_TAGS["actions"]:
        used |= set(render(templates, new_catalog, settings,
                           constraints=parse_constraints({"actions": [action]}, new_catalog)).fragments)
    pin = {"status": "rerouted", "previous_model": "Phi-4", "reason": "x"}
    used |= set(render(templates, new_catalog, settings, pin=pin).fragments)
    assert used == {f.id for f in templates.fragments}


def test_long_text_drops_fragments_from_the_end_but_keeps_base_and_mode(templates, new_catalog, settings):
    small = InstructionTemplates(templates.fragments, templates.criteria_templates, 300, templates.thresholds,
                                 templates.action_names)
    body = dict(USER, tools=[TOOL], max_completion_tokens=20_000)
    rendered = render(small, new_catalog, settings, body=body, band="high",
                      constraints=parse_constraints({"actions": ["code"]}, new_catalog))
    assert rendered.fragments[:2] == ["base", "mode.balanced"] and rendered.dropped
    assert rendered.dropped[0] == "size.long_output"


def test_criteria_rank_among_the_options_and_state_the_price(templates, catalog):
    picked = [catalog.by_name["gpt-6-astra"], catalog.by_name["gpt-5.4-nano"]]
    criteria = templates.criteria(picked)
    assert list(criteria) == ["gpt-6-astra", "gpt-5.4-nano"]
    assert criteria["gpt-6-astra"].endswith("Price per million tokens: $10 input, $50 output; 2 of 2 by price among "
                                            "these options, ordered by output price then input price (1 is cheapest).")
    assert "$0.10 input, $0.80 output; 1 of 2" in criteria["gpt-5.4-nano"]


def test_criteria_rank_follows_output_price(templates, new_catalog, settings):
    # tiers order by output price, so a cheaper output price never ranks below a dearer one
    result = select(new_catalog, "cost", Constraints(), USER, settings)
    criteria = templates.criteria(result.candidates)
    ranked = sorted(result.candidates, key=lambda m: int(re.search(r"; (\d+) of", criteria[m.name]).group(1)))
    prices = [m.output_price for m in ranked]
    assert prices == sorted(prices)
    assert all("ordered by output price then input price" in text for text in criteria.values())
    unpriced = templates.criteria([replace(new_catalog.by_name["gpt-5.5"], output_price=None)])
    assert unpriced["gpt-5.5"].endswith("1 of 1 by price among these options (1 is cheapest).")


@pytest.mark.parametrize("change, message", [
    (lambda d: d["fragments"].append({"id": "x", "text": "{nope}"}), "unknown placeholder"),
    (lambda d: d["fragments"].append({"id": "x", "when": {"weather": ["sun"]}, "text": "t"}), "unknown condition"),
    (lambda d: d["fragments"].append({"id": "x", "when": {"has": ["telepathy"]}, "text": "t"}), "unknown band kind or flag"),
    (lambda d: d["fragments"].append(dict(d["fragments"][1])), "unique"),
    (lambda d: d["action_names"].pop("code"), "action_names"),
])
def test_invalid_templates_fail_to_load(settings, tmp_path, change, message):
    data = json.loads(settings.instructions_path.read_text())
    change(data)
    path = tmp_path / "t.json"
    path.write_text(json.dumps(data))
    with pytest.raises(TemplateError, match=message):
        InstructionTemplates.load(path)


def test_route_reports_what_decision1_received_and_the_log_records_it(make_pipeline, settings, fake, tmp_path):
    fake.decision_responses.append(ranked(["claude-sonnet-5-5", "gpt-5.5"]))
    body = {**USER, "compatibility": "new", "routing_cost_band": "high", "routing_constraints": {"actions": ["code"]}}
    with TestClient(create_app(settings, pipeline=make_pipeline())) as c:
        routed = c.post("/v1/route", json=body).json()
    sent = next(r["body"] for r in fake.requests if r["path"].endswith("/systemone"))["questions"]["route"]
    assert sent["instructions"] == routed["decision1_input"]["instructions"]
    assert sent["criteria"] == routed["decision1_input"]["criteria"]
    assert "action.code" in routed["decision1_input"]["fragments"]
    event = json.loads((tmp_path / "decisions.jsonl").read_text().splitlines()[-1])
    assert event["instructions"]["text"] == sent["instructions"] and len(event["instructions"]["template_sha256"]) == 64
