import json
from dataclasses import replace

import pytest
from fastapi.testclient import TestClient

from decision_router.app import create_app
from decision_router.policy import PolicyError, load_policies, policy_for
from decision_router.testing import ranked

USER = [{"role": "user", "content": "Compare two vendors."}]


def secured(settings, policies):
    return replace(settings, allow_unauthenticated=False, api_keys=("k1", "k2"), caller_policies=policies)


def post(client, key, body):
    return client.post("/v1/route", headers={"api-key": key}, json={"messages": USER, **body})


def test_price_limit_and_model_list_narrow_stage1(make_pipeline, settings, fake):
    s = secured(settings, {"api_key:1": {"max_output_price": 10},
                           "api_key:2": {"models": ["gpt-5.5", "gpt-5.6-terra"]}})
    fake.decision_responses.extend([ranked(["gpt-5.6-terra"]), ranked(["gpt-5.5"])])
    with TestClient(create_app(s, pipeline=make_pipeline(caller_policies=s.caller_policies))) as c:
        capped = post(c, "k1", {"compatibility": "new"}).json()
        listed = post(c, "k2", {}).json()
    assert capped["candidates"] and "gpt-6-astra" not in capped["candidates"]
    removed = {r["model"]: r["reason"] for st in capped["stage1"]["steps"] for r in st["removed"]}
    assert removed["gpt-6-astra"] == "output price above caller policy 'api_key:1' limit ($10)"
    assert sorted(listed["candidates"]) == ["gpt-5.5", "gpt-5.6-terra"]


def test_request_cannot_widen_the_policy(make_pipeline, settings, fake, tmp_path):
    # the request names an expensive model; the policy's price limit still removes it
    s = secured(settings, {"api_key:1": {"max_output_price": 10, "catalogs": ["new"], "modes": ["cost", "balanced"]}})
    fake.decision_responses.append(ranked(["claude-sonnet-5-5"]))
    with TestClient(create_app(s, pipeline=make_pipeline(caller_policies=s.caller_policies))) as c:
        narrowed = post(c, "k1", {"compatibility": "new",
                                  "routing_constraints": {"models": ["gpt-6-astra", "claude-sonnet-5-5"]}})
        other_catalog = post(c, "k1", {"compatibility": "old"})
        other_mode = post(c, "k1", {"compatibility": "new", "routing_mode": "quality"})
    assert narrowed.json()["candidates"] == ["claude-sonnet-5-5"]
    assert other_catalog.status_code == 403 and other_catalog.json()["error"]["code"] == "policy_denied"
    assert other_mode.status_code == 403
    event = json.loads((tmp_path / "decisions.jsonl").read_text().splitlines()[0])
    assert event["policy"]["name"] == "api_key:1"


def test_policy_lookup_falls_back_to_star():
    policies = load_policies({"entra:ABC": {"modes": ["cost"]}, "*": {"max_output_price": 25}})
    assert policy_for(policies, {"method": "entra", "identity": "abc"}).name == "entra:ABC"
    assert policy_for(policies, {"method": "api_key", "identity": "3"}).name == "*"
    assert policy_for(policies, None).name == "*"
    assert policy_for(load_policies({}), {"method": "api_key", "identity": "1"}) is None


@pytest.mark.parametrize("raw, message", [
    ({"key:1": {}}, "must be 'api_key:<n>'"),
    ({"*": {"budget": 1}}, "keys from"),
    ({"*": {"modes": ["fast"]}}, "drawn from"),
    ({"*": {"max_output_price": 0}}, "positive number"),
])
def test_invalid_policies(raw, message):
    with pytest.raises(PolicyError, match=message):
        load_policies(raw)


def test_unknown_policy_model_fails_at_startup(make_pipeline):
    with pytest.raises(ValueError, match="unknown models"):
        make_pipeline(caller_policies={"*": {"models": ["gpt-9"]}})
