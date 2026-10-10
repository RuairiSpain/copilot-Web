import json

import httpx
from fastapi.testclient import TestClient

from decision_router.app import create_app
from decision_router.testing import ranked

USER = [{"role": "user", "content": "Compare two vendors."}]


def client_for(make_pipeline, settings, **overrides):
    return TestClient(create_app(settings, pipeline=make_pipeline(**overrides)))


def chat_calls(fake):
    return [r for r in fake.requests if r["path"].endswith(("/chat/completions", "/anthropic/v1/messages"))]


def decision_calls(fake):
    return [r["body"] for r in fake.requests if r["path"].endswith("/systemone")]


def last_log(tmp_path):
    return json.loads((tmp_path / "decisions.jsonl").read_text().splitlines()[-1])


def test_model_router_shaped_request_returns_the_model_response_unchanged(make_pipeline, settings, fake, tmp_path,
                                                                         catalog):
    fake.decision_responses.append(ranked(["gpt-5.5", "gpt-5.6-terra"]))
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={
            "model": "model-router", "messages": USER, "max_completion_tokens": 64,
            "routing_mode": "balanced", "temperature": 0.3,
        }, headers={"x-request-id": "req-1"})
    assert response.status_code == 200
    body = response.json()
    assert body["object"] == "chat.completion" and body["model"] == "gpt-5.5"
    assert response.headers["x-router-served-model"] == "gpt-5.5"
    assert response.headers["x-router-compatibility"] == "old"
    assert response.headers["x-router-ranking"].startswith("gpt-5.5,gpt-5.6-terra")
    sent = chat_calls(fake)[0]
    assert sent["path"] == "/openai/v1/chat/completions"
    assert sent["body"]["model"] == "gpt-5.5" and "routing_mode" not in sent["body"]
    assert sent["body"]["max_completion_tokens"] == 64 and sent["body"]["temperature"] == 0.3
    # balanced mode offers the whole old catalog to Decision-1
    assert set(decision_calls(fake)[0]["questions"]["route"]["criteria"]) == {m.name for m in catalog.models}
    log = last_log(tmp_path)
    assert log["request_id"] == "req-1" and log["compatibility"] == "old"
    assert log["decision"]["ranking"][:2] == ["gpt-5.5", "gpt-5.6-terra"] and log["outcome"] == "success"
    assert [s["step"] for s in log["stage1"]["steps"]] == ["api", "lifecycle", "allowlist", "selection", "capabilities",
                                                           "location", "size", "price_band"]
    assert "Compare two vendors" not in (tmp_path / "decisions.jsonl").read_text()


def test_compatibility_new_uses_the_new_catalog(make_pipeline, settings, fake, new_catalog):
    with client_for(make_pipeline, settings) as client:
        body = client.post("/v1/route", json={"messages": USER, "compatibility": "new"}).json()
    assert body["compatibility"] == "new"
    assert set(body["candidates"]) == {m.name for m in new_catalog.models}


def test_unknown_compatibility_is_rejected(make_pipeline, settings):
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/route", json={"messages": USER, "compatibility": "legacy"})
    assert response.status_code == 400 and response.json()["error"]["code"] == "invalid_compatibility"


def test_cost_mode_offers_the_cheapest_band(make_pipeline, settings, fake, catalog):
    with client_for(make_pipeline, settings) as client:
        body = client.post("/v1/route", json={"messages": USER, "routing_mode": "cost"}).json()
    cheapest = [m.name for m in catalog.models[: len(body["candidates"])]]
    assert body["candidates"] == cheapest and 1 <= len(cheapest) < len(catalog.models)
    assert list(decision_calls(fake)[0]["questions"]["route"]["criteria"]) == cheapest
    assert not chat_calls(fake)


def test_constraints_narrow_the_choice_and_are_not_forwarded(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["gpt-5.4-mini"]))
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={
            "messages": USER,
            "routing_constraints": {"models": ["gpt-5.4-mini", "gpt-5.5", "claude-opus-5"], "region": "swedencentral",
                                    "inference_in_azure": True},
        })
    assert response.status_code == 200
    offered = set(decision_calls(fake)[0]["questions"]["route"]["criteria"])
    assert offered == {"gpt-5.4-mini", "gpt-5.5", "claude-opus-5"}
    assert "routing_constraints" not in chat_calls(fake)[0]["body"]


def test_tools_in_the_request_remove_models_without_tool_calling(make_pipeline, settings, fake, tmp_path):
    tools = [{"type": "function", "function": {"name": "lookup", "parameters": {"type": "object"}}}]
    with client_for(make_pipeline, settings) as client:
        client.post("/v1/route", json={"messages": USER, "tools": tools, "compatibility": "new"})
    offered = set(decision_calls(fake)[0]["questions"]["route"]["criteria"])
    assert "Phi-4" not in offered and "Llama-4-Maverick-17B-128E-Instruct-FP8" not in offered
    assert "gpt-5.5" in offered
    removed = {r["model"]: r["reason"] for s in last_log(tmp_path)["stage1"]["steps"] if s["step"] == "capabilities"
               for r in s["removed"]}
    assert removed["Phi-4"].startswith("tools no")


def test_no_eligible_models_returns_422_naming_the_step(make_pipeline, settings, fake):
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={
            "messages": USER, "routing_constraints": {"models": ["Phi-4"], "capabilities": ["tools"]},
            "compatibility": "new",
        })
    assert response.status_code == 422
    error = response.json()["error"]
    assert error["code"] == "no_eligible_models" and error["stage1"]["empty_at"] == "capabilities"
    assert not decision_calls(fake) and not chat_calls(fake)


def test_invalid_constraints_are_rejected(make_pipeline, settings):
    with client_for(make_pipeline, settings) as client:
        typo = client.post("/v1/route", json={"messages": USER, "routing_constraints": {"model": ["gpt-5.5"]}})
        unknown_model = client.post("/v1/route", json={"messages": USER, "routing_constraints": {"models": ["gpt-9"]}})
        bad_capability = client.post("/v1/route", json={"messages": USER,
                                                       "routing_constraints": {"capabilities": ["telepathy"]}})
    for response in (typo, unknown_model, bad_capability):
        assert response.status_code == 400 and response.json()["error"]["code"] == "invalid_routing_constraints"


def test_claude_is_called_through_the_messages_api_and_answers_as_chat_completions(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["claude-sonnet-5"]))
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={
            "messages": [{"role": "system", "content": "Be brief."}] + USER, "max_completion_tokens": 100})
    assert response.status_code == 200
    body = response.json()
    assert body["object"] == "chat.completion" and body["choices"][0]["message"]["content"] == "answer from claude-sonnet-5"
    assert body["usage"]["prompt_tokens"] == 12 and body["usage"]["completion_tokens"] == 5
    call = chat_calls(fake)[0]
    assert call["path"] == "/anthropic/v1/messages"
    assert call["headers"]["anthropic-version"] == "2023-06-01" and call["headers"]["x-api-key"] == "test-key"
    assert call["body"]["system"] == "Be brief." and call["body"]["max_tokens"] == 100


def test_claude_streaming_is_translated(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["claude-sonnet-5"]))
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={"messages": USER, "stream": True,
                                                              "stream_options": {"include_usage": True}})
    assert response.status_code == 200 and response.headers["content-type"].startswith("text/event-stream")
    events = [json.loads(line[6:]) for line in response.text.splitlines() if line.startswith("data: {")]
    text = "".join(e["choices"][0]["delta"].get("content", "") for e in events if e["choices"])
    assert text == "answer from claude-sonnet-5"
    assert events[-1]["usage"]["completion_tokens"] == 5 and response.text.rstrip().endswith("data: [DONE]")


NATIVE = {"model": "anything", "max_tokens": 200, "system": "Be brief.",
          "messages": [{"role": "user", "content": [{"type": "text", "text": "Compare two vendors."}]}]}


def test_claude_translation_off_forwards_the_messages_body_unchanged(make_pipeline, settings, fake, tmp_path):
    fake.decision_responses.append(ranked(["claude-opus-5"]))
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json=dict(NATIVE, claude_translation=False))
    assert response.status_code == 200
    body = response.json()
    assert body["type"] == "message" and body["content"][0]["text"] == "answer from claude-opus-5"  # native shape
    assert response.headers["x-router-served-model"] == "claude-opus-5"
    call = chat_calls(fake)[0]
    assert call["path"] == "/anthropic/v1/messages"
    assert call["body"] == dict(NATIVE, model="claude-opus-5")  # only `model` changes; no extension fields
    offered = set(decision_calls(fake)[0]["questions"]["route"]["criteria"])
    assert offered and all(name.startswith("claude-") for name in offered)
    state = decision_calls(fake)[0]["state"]
    assert "[system]\nBe brief." in state and "Compare two vendors." in state
    log = last_log(tmp_path)
    assert log["claude_translation"] is False and log["usage"]["prompt_tokens"] == 12
    api_step = next(s for s in log["stage1"]["steps"] if s["step"] == "api")
    assert {r["model"] for r in api_step["removed"]} >= {"gpt-5.5", "FW-GLM-5.3"}


def test_claude_translation_off_streams_native_events(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["claude-sonnet-5"]))
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json=dict(NATIVE, stream=True, claude_translation=False))
    assert response.status_code == 200
    assert "event: message_start" in response.text and "chat.completion.chunk" not in response.text


def test_claude_translation_off_with_non_claude_subset_is_422(make_pipeline, settings, fake):
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json=dict(
            NATIVE, claude_translation=False, routing_constraints={"models": ["gpt-5.5"]}))
    assert response.status_code == 422 and response.json()["error"]["stage1"]["empty_at"] == "selection"


def test_claude_translation_must_be_boolean(make_pipeline, settings):
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/route", json={"messages": USER, "claude_translation": "off"})
    assert response.status_code == 400


def test_mai_models_use_the_mai_endpoint(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["MAI-Thinking-1"]))
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={"messages": USER, "compatibility": "new"})
    assert response.status_code == 200
    assert chat_calls(fake)[0]["path"] == "/mai/v1/chat/completions"


def test_low_confidence_top_model_is_skipped_for_the_second(make_pipeline, settings, fake, tmp_path):
    fake.decision_responses.append(ranked(["gpt-5.5", "gpt-5.6-terra"], top=0.35))
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={"messages": USER})
    assert response.json()["model"] == "gpt-5.6-terra"
    assert response.headers["x-router-low-confidence"] == "true"
    assert response.headers["x-router-ranking"].startswith("gpt-5.5,gpt-5.6-terra")
    line = last_log(tmp_path)
    assert line["decision"]["low_confidence"] is True and line["decision"]["top_probability"] < 0.5
    assert line["decision"]["execution_order"][:2] == ["gpt-5.6-terra", "gpt-5.5"] and line["fallback_used"] is False


def test_low_confidence_second_model_failing_falls_back_to_the_top_model(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["gpt-5.5", "gpt-5.6-terra"], top=0.35))
    fake.fail("gpt-5.6-terra", 500, 500)
    with client_for(make_pipeline, settings) as client:
        assert client.post("/v1/chat/completions", json={"messages": USER}).json()["model"] == "gpt-5.5"


def test_low_confidence_rule_can_be_turned_off(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["gpt-5.5", "gpt-5.6-terra"], top=0.35))
    with client_for(make_pipeline, settings, low_confidence_threshold=0.0) as client:
        response = client.post("/v1/chat/completions", json={"messages": USER})
    assert response.json()["model"] == "gpt-5.5" and response.headers["x-router-low-confidence"] == "false"


def test_failed_top_model_falls_back_to_next_ranked(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["gpt-5.5", "gpt-5.6-terra"]))
    fake.fail("gpt-5.5", 503, 503)
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={"messages": USER})
    assert response.status_code == 200 and response.json()["model"] == "gpt-5.6-terra"
    assert [c["body"]["model"] for c in chat_calls(fake)] == ["gpt-5.5", "gpt-5.5", "gpt-5.6-terra"]


def test_fallback_never_leaves_the_stage1_set(make_pipeline, settings, fake, catalog):
    with client_for(make_pipeline, settings) as client:
        cost = client.post("/v1/route", json={"messages": USER, "routing_mode": "cost"}).json()["candidates"]
        for name in cost:
            fake.fail(catalog.by_name[name].deployment, 500, 500)
        response = client.post("/v1/chat/completions", json={"messages": USER, "routing_mode": "cost"})
    assert response.status_code == 502 and response.json()["error"]["code"] == "all_models_failed"
    assert {c["body"]["model"] for c in chat_calls(fake)} == {catalog.by_name[n].deployment for n in cost}


def test_missing_deployment_moves_on_without_retrying(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["gpt-5.5", "gpt-5.6-terra"]))
    fake.fail("gpt-5.5", 404)
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={"messages": USER})
    assert response.json()["model"] == "gpt-5.6-terra"
    assert [c["body"]["model"] for c in chat_calls(fake)] == ["gpt-5.5", "gpt-5.6-terra"]


def test_client_errors_are_passed_through_without_fallback(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["gpt-5.5", "gpt-5.6-terra"]))
    fake.fail("gpt-5.5", 400)
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={"messages": USER})
    assert response.status_code == 400 and response.json()["error"]["code"] == "400"
    assert len(chat_calls(fake)) == 1


def test_retry_after_is_honoured_up_to_the_cap(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["gpt-5.5", "gpt-5.6-terra"]))
    pipe = make_pipeline()
    original, state = fake.handle, {"n": 0}

    def throttled(request):
        if request.url.path.endswith("/chat/completions") and state["n"] == 0:
            state["n"] += 1
            return httpx.Response(429, headers={"retry-after-ms": "1500"}, json={"error": {}})
        return original(request)

    pipe.http._transport = httpx.MockTransport(throttled)
    with TestClient(create_app(settings, pipeline=pipe)) as client:
        response = client.post("/v1/chat/completions", json={"messages": USER})
    assert response.status_code == 200 and response.json()["model"] == "gpt-5.5"
    assert pipe.sleeps == [1.5]


def test_decision1_failure_returns_an_error_and_calls_no_model(make_pipeline, settings, fake, tmp_path):
    fake.decision_responses.append(httpx.Response(401, json={"error": "unauthorized"}))
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={"messages": USER})
    assert response.status_code == 502 and response.json()["error"]["code"] == "decision_unavailable"
    assert not chat_calls(fake)
    log = last_log(tmp_path)
    assert log["outcome"] == "decision_failed" and log["error_code"] == "http_error"


def test_decision1_throttling_is_retried_then_succeeds(make_pipeline, settings, fake, tmp_path):
    fake.decision_responses.append(httpx.Response(429, headers={"retry-after-ms": "200"}, json={}))
    fake.decision_responses.append(ranked(["gpt-5.5", "gpt-5.6-terra"]))
    pipe = make_pipeline()
    with TestClient(create_app(settings, pipeline=pipe)) as client:
        response = client.post("/v1/chat/completions", json={"messages": USER})
    assert response.status_code == 200 and response.json()["model"] == "gpt-5.5"
    assert pipe.sleeps == [0.2] and last_log(tmp_path)["decision"]["attempts"] == 2


def test_decision1_throttling_that_persists_returns_503(make_pipeline, settings, fake):
    for _ in range(3):
        fake.decision_responses.append(httpx.Response(429, json={}))
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={"messages": USER})
    assert response.status_code == 503 and response.json()["error"]["code"] == "decision_unavailable"
    assert not chat_calls(fake)


def test_decision1_retry_after_beyond_the_budget_reports_the_429(make_pipeline, settings, fake):
    fake.decision_responses.append(httpx.Response(429, headers={"retry-after": "60"}, json={}))
    with client_for(make_pipeline, settings) as client:
        assert client.post("/v1/chat/completions", json={"messages": USER}).status_code == 503


def test_decision1_usage_model_and_cost_are_logged(make_pipeline, settings, fake, tmp_path):
    with client_for(make_pipeline, settings) as client:
        client.post("/v1/route", json={"messages": USER})
    decision = last_log(tmp_path)["decision"]
    assert decision["response_model"] == "microsoft-decision-1"
    assert decision["cost"]["amount"] == round(decision["usage"]["prompt_tokens"] * 0.042 / 1_000_000, 10)
    sent = next(r for r in fake.requests if r["path"].endswith("/systemone"))
    assert sent["headers"]["accept"] == "application/json" and sent["headers"]["api-key"] == "test-key"


def test_streaming_is_passed_through(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["gpt-5.6-terra", "gpt-5.5"]))
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={"messages": USER, "stream": True})
    assert response.status_code == 200 and response.headers["content-type"].startswith("text/event-stream")
    assert "answer from gpt-5.6-terra" in response.text and response.text.rstrip().endswith("data: [DONE]")


def test_request_validation(make_pipeline, settings):
    with client_for(make_pipeline, settings) as client:
        assert client.post("/v1/chat/completions", json={"messages": []}).status_code == 400
        bad_mode = client.post("/v1/chat/completions", json={"messages": USER, "routing_mode": "cheap"})
        assert bad_mode.status_code == 400 and bad_mode.json()["error"]["code"] == "invalid_routing_mode"
        assert client.post("/v1/chat/completions", content=b"{not json",
                           headers={"content-type": "application/json"}).status_code == 400


def test_health_and_metrics(make_pipeline, settings, fake):
    with client_for(make_pipeline, settings) as client:
        assert client.get("/health/live").status_code == 200
        ready = client.get("/health/ready").json()
        assert ready["default_compatibility"] == "old" and set(ready["catalogs"]) == {"old", "new"}
        client.post("/v1/chat/completions", json={"messages": USER})
        metrics = client.get("/metrics").text
    assert "router_requests_total" in metrics and "router_decision_latency_ms_bucket" in metrics
