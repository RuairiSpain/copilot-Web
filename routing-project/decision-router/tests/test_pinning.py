import json

from fastapi.testclient import TestClient

from decision_router.app import create_app
from decision_router.pinning import PinStore, conversation_key
from decision_router.testing import ranked

SYSTEM = {"role": "system", "content": "You are a coding agent."}
TURN1 = [SYSTEM, {"role": "user", "content": "Fix the failing test in utils.py"}]
TURN2 = TURN1 + [{"role": "assistant", "content": "Looking at it."}, {"role": "user", "content": "Go on"}]
TOOLS = [{"type": "function", "function": {"name": "read_file", "parameters": {"type": "object"}}}]


def decision_calls(fake):
    return [r for r in fake.requests if r["path"].endswith("/systemone")]


def served(response):
    return response.json()["model"]


def log(tmp_path):
    return [json.loads(line) for line in (tmp_path / "decisions.jsonl").read_text().splitlines()]


def client(make_pipeline, settings, **overrides):
    return TestClient(create_app(settings, pipeline=make_pipeline(**overrides)))


def test_later_turns_follow_the_first_turns_model_without_decision1(make_pipeline, settings, fake, tmp_path):
    fake.decision_responses.append(ranked(["gpt-5.5", "gpt-5.6-terra"]))
    with client(make_pipeline, settings) as c:
        first = c.post("/v1/chat/completions", json={"messages": TURN1, "routing_pin": True})
        second = c.post("/v1/chat/completions", json={"messages": TURN2, "routing_pin": True})
    assert served(first) == served(second) == "gpt-5.5"
    assert len(decision_calls(fake)) == 1
    assert (first.headers["x-router-pin"], second.headers["x-router-pin"]) == ("new", "followed")
    events = log(tmp_path)
    assert events[0]["pin"]["pinned_to"] == "gpt-5.5" and events[1]["decision"]["pinned"] is True
    assert "routing_pin" not in fake.requests[-1]["body"]


def test_without_pinning_every_turn_is_routed(make_pipeline, settings, fake):
    fake.decision_responses.extend([ranked(["gpt-5.5"]), ranked(["gpt-5.6-terra"])])
    with client(make_pipeline, settings) as c:
        first = c.post("/v1/chat/completions", json={"messages": TURN1})
        second = c.post("/v1/chat/completions", json={"messages": TURN2})
    assert (served(first), served(second)) == ("gpt-5.5", "gpt-5.6-terra") and len(decision_calls(fake)) == 2
    assert "x-router-pin" not in second.headers


def test_pinned_model_failing_falls_back_and_repins(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["gpt-5.5", "gpt-5.6-terra"]))
    with client(make_pipeline, settings, pin_default=True) as c:
        c.post("/v1/chat/completions", json={"messages": TURN1})
        fake.fail("gpt-5.5", 503, 503)
        second = c.post("/v1/chat/completions", json={"messages": TURN2})
        third = c.post("/v1/chat/completions", json={"messages": TURN2 + [{"role": "user", "content": "and?"}]})
    assert served(second) == "gpt-5.6-terra"  # next model from the pinned ranking
    assert served(third) == "gpt-5.6-terra" and len(decision_calls(fake)) == 1


def test_pin_is_dropped_when_the_model_no_longer_passes_stage1(make_pipeline, settings, fake, tmp_path):
    fake.decision_responses.extend([ranked(["Llama-4-Maverick-17B-128E-Instruct-FP8"]), ranked(["gpt-5.5"])])
    with client(make_pipeline, settings, pin_default=True) as c:
        c.post("/v1/chat/completions", json={"messages": TURN1})
        second = c.post("/v1/chat/completions", json={"messages": TURN2, "tools": TOOLS})  # Llama has no tools
    assert served(second) == "gpt-5.5" and second.headers["x-router-pin"] == "rerouted"
    pin = log(tmp_path)[1]["pin"]
    assert pin["previous_model"] == "Llama-4-Maverick-17B-128E-Instruct-FP8" and pin["reason"].startswith("tools no")


def test_session_header_and_scope(make_pipeline, settings, fake):
    fake.decision_responses.extend([ranked(["gpt-5.5"]), ranked(["gpt-5.6-terra"]), ranked(["gpt-5.4-mini"])])
    with client(make_pipeline, settings, pin_default=True) as c:
        a = {"x-router-session-id": "task-1"}
        c.post("/v1/chat/completions", headers=a, json={"messages": [{"role": "user", "content": "one"}]})
        # different opening messages, same session id: still the same conversation
        same = c.post("/v1/chat/completions", headers=a, json={"messages": [{"role": "user", "content": "two"}]})
        other = c.post("/v1/chat/completions", headers={"x-router-session-id": "task-2"}, json={"messages": TURN1})
        quality = c.post("/v1/chat/completions", headers=a,
                         json={"messages": TURN1, "routing_mode": "quality"})  # another mode never shares a pin
    assert served(same) == "gpt-5.5" and served(other) == "gpt-5.6-terra"
    assert quality.headers["x-router-pin"] == "new"


def test_profile_can_turn_pinning_on(make_pipeline, settings, fake):
    profiles = {**settings.profiles, "agent-coding": {"routing_mode": "quality", "routing_pin": True}}
    fake.decision_responses.append(ranked(["claude-opus-5"]))
    with client(make_pipeline, settings, profiles=profiles) as c:
        c.post("/v1/chat/completions", json={"model": "agent-coding", "messages": TURN1})
        second = c.post("/v1/chat/completions", json={"model": "agent-coding", "messages": TURN2})
    assert second.headers["x-router-pin"] == "followed" and len(decision_calls(fake)) == 1


def test_route_only_does_not_create_pins(make_pipeline, settings, fake):
    fake.decision_responses.extend([ranked(["gpt-5.5"]), ranked(["gpt-5.6-terra"])])
    with client(make_pipeline, settings, pin_default=True) as c:
        c.post("/v1/route", json={"messages": TURN1})
        second = c.post("/v1/route", json={"messages": TURN2})
    assert second.json()["pin"]["status"] == "new" and len(decision_calls(fake)) == 2


def test_invalid_routing_pin(make_pipeline, settings):
    with client(make_pipeline, settings) as c:
        response = c.post("/v1/chat/completions", json={"messages": TURN1, "routing_pin": "yes"})
    assert response.status_code == 400


def test_conversation_key_ignores_later_turns():
    scope = ("api_key", "1", "old", "balanced", False)
    assert conversation_key({"messages": TURN1}, session_id=None, scope=scope) == \
        conversation_key({"messages": TURN2}, session_id=None, scope=scope)
    assert conversation_key({"messages": TURN1, "user": "u2"}, session_id=None, scope=scope) != \
        conversation_key({"messages": TURN1}, session_id=None, scope=scope)
    assert conversation_key({"messages": [SYSTEM]}, session_id=None, scope=scope) is None


def test_store_expiry_and_size_limit():
    now = [0.0]
    store = PinStore(ttl_seconds=10, max_entries=2, clock=lambda: now[0])
    store.set("a", "m1", ["m1", "m2"], {})
    now[0] = 8
    assert store.get("a").model == "m1"  # use extends the pin
    now[0] = 15
    assert store.get("a") is not None
    now[0] = 30
    assert store.get("a") is None
    for key in ("x", "y", "z"):
        store.set(key, "m", [], {})
    assert len(store) == 2 and store.get("x") is None


def test_builtin_coding_profile(make_pipeline, settings, fake):
    pipe = make_pipeline()
    prepared = pipe.prepare({"model": "decision-router-coding", "messages": TURN1, "tools": TOOLS})
    assert prepared.pin and prepared.mode == "balanced" and prepared.catalog.compatibility == "new"
    assert {"tools", "parallel_tools"} <= set(prepared.constraints.capabilities)
    fake.decision_responses.append(ranked(["claude-sonnet-5-5"]))
    with client(make_pipeline, settings) as c:
        body = {"model": "decision-router-coding", "messages": TURN1, "tools": TOOLS, "parallel_tool_calls": True}
        routed = c.post("/v1/route", json=body).json()
    assert set(routed["candidates"]) <= set(prepared.constraints.models) and len(routed["candidates"]) >= 5
    assert "gpt-6.1-sol" not in routed["candidates"]
