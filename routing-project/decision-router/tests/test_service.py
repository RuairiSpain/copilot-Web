import json

import httpx
from fastapi.testclient import TestClient

from decision_router.app import create_app

BALANCED = ["gpt-5-nano", "gpt-5-mini", "deepseek-v4-flash", "gpt-5.5", "o4-mini", "gpt-5.6-terra"]


def ranked(order, all_options=BALANCED):
    """A Decision-1 answer whose probabilities rank `order` first, in that order."""
    rest = [n for n in all_options if n not in order]
    probabilities = {name: 0.01 for name in rest}
    weights = [0.5, 0.25, 0.12, 0.06, 0.04, 0.03][: len(order)]
    for name, weight in zip(order, weights, strict=True):
        probabilities[name] = weight
    probabilities[order[0]] += 1 - sum(probabilities.values())
    return {"answers": {"route": {"type": "choice", "choice": order[0], "probabilities": probabilities,
                                  "confidence": 0.5}}}


def client_for(make_pipeline, settings, **overrides):
    return TestClient(create_app(settings, pipeline=make_pipeline(**overrides)))


def chat_calls(fake):
    return [r for r in fake.requests if r["path"].endswith("/chat/completions")]


def test_model_router_shaped_request_returns_the_model_response_unchanged(make_pipeline, settings, fake, tmp_path):
    fake.decision_responses.append(ranked(["gpt-5.5", "o4-mini"]))
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={
            "model": "model-router", "messages": [{"role": "user", "content": "Compare two vendors."}],
            "max_completion_tokens": 64, "routing_mode": "balanced", "temperature": 0.3,
        }, headers={"x-request-id": "req-1"})
    assert response.status_code == 200
    body = response.json()
    assert body["object"] == "chat.completion" and body["model"] == "gpt-5.5"
    assert response.headers["x-router-served-model"] == "gpt-5.5"
    assert response.headers["x-router-ranking"].startswith("gpt-5.5,o4-mini")
    sent = chat_calls(fake)[0]["body"]
    assert sent["model"] == "gpt-5.5" and "routing_mode" not in sent
    assert sent["max_completion_tokens"] == 64 and sent["temperature"] == 0.3

    decision_request = next(r for r in fake.requests if r["path"].endswith("/systemone"))["body"]
    assert set(decision_request["questions"]["route"]["criteria"]) == set(BALANCED)
    log = [json.loads(line) for line in (tmp_path / "decisions.jsonl").read_text().splitlines()]
    assert log[-1]["request_id"] == "req-1" and log[-1]["decision"]["ranking"][:2] == ["gpt-5.5", "o4-mini"]
    assert log[-1]["served_model"] == "gpt-5.5" and log[-1]["outcome"] == "success"
    assert "Compare two vendors" not in (tmp_path / "decisions.jsonl").read_text()


def test_stage1_limits_what_decision1_sees(make_pipeline, settings, fake):
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/route", json={"messages": [{"role": "user", "content": "hi"}], "routing_mode": "cost"})
    assert response.status_code == 200
    assert response.json()["candidates"] == ["gpt-5-nano", "gpt-5-mini", "deepseek-v4-flash"]
    criteria = next(r for r in fake.requests if r["path"].endswith("/systemone"))["body"]["questions"]["route"]["criteria"]
    assert list(criteria) == ["gpt-5-nano", "gpt-5-mini", "deepseek-v4-flash"]
    assert not chat_calls(fake)


def test_default_mode_comes_from_the_catalog(make_pipeline, settings):
    with client_for(make_pipeline, settings) as client:
        body = client.post("/v1/route", json={"messages": [{"role": "user", "content": "hi"}]}).json()
    assert body["routing_mode"] == "balanced"


def test_failed_top_model_falls_back_to_next_ranked(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["gpt-5.5", "o4-mini"]))
    fake.fail("gpt-5.5", 503, 503)
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={"messages": [{"role": "user", "content": "x"}]})
    assert response.status_code == 200
    assert response.json()["model"] == "o4-mini"
    assert [c["body"]["model"] for c in chat_calls(fake)] == ["gpt-5.5", "gpt-5.5", "o4-mini"]


def test_fallback_never_leaves_the_stage1_set(make_pipeline, settings, fake):
    cost = ["gpt-5-nano", "gpt-5-mini", "deepseek-v4-flash"]
    fake.decision_responses.append(ranked(cost, cost))
    for name in cost:
        fake.fail(name, 500, 500)
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={"messages": [{"role": "user", "content": "x"}],
                                                              "routing_mode": "cost"})
    assert response.status_code == 502
    assert response.json()["error"]["code"] == "all_models_failed"
    assert {c["body"]["model"] for c in chat_calls(fake)} == set(cost)


def test_missing_deployment_moves_on_without_retrying(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["gpt-5.5", "o4-mini"]))
    fake.fail("gpt-5.5", 404)
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={"messages": [{"role": "user", "content": "x"}]})
    assert response.json()["model"] == "o4-mini"
    assert [c["body"]["model"] for c in chat_calls(fake)] == ["gpt-5.5", "o4-mini"]


def test_client_errors_are_passed_through_without_fallback(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["gpt-5.5", "o4-mini"]))
    fake.fail("gpt-5.5", 400)
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={"messages": [{"role": "user", "content": "x"}]})
    assert response.status_code == 400
    assert response.json()["error"]["code"] == "400"
    assert len(chat_calls(fake)) == 1


def test_retry_after_is_honoured_up_to_the_cap(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["gpt-5.5", "o4-mini"]))
    pipe = make_pipeline()

    original = fake.handle
    state = {"n": 0}

    def throttled(request):
        if request.url.path.endswith("/chat/completions") and state["n"] == 0:
            state["n"] += 1
            return httpx.Response(429, headers={"retry-after-ms": "1500"}, json={"error": {}})
        return original(request)

    pipe.http._transport = httpx.MockTransport(throttled)
    with TestClient(create_app(settings, pipeline=pipe)) as client:
        response = client.post("/v1/chat/completions", json={"messages": [{"role": "user", "content": "x"}]})
    assert response.status_code == 200 and response.json()["model"] == "gpt-5.5"
    assert pipe.sleeps == [1.5]


def test_decision1_failure_returns_an_error_and_calls_no_model(make_pipeline, settings, fake, tmp_path):
    fake.decision_responses.append(httpx.Response(503, json={"error": "down"}))
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={"messages": [{"role": "user", "content": "x"}]})
    assert response.status_code == 502
    assert response.json()["error"]["code"] == "decision_unavailable"
    assert not chat_calls(fake)
    log = json.loads((tmp_path / "decisions.jsonl").read_text().splitlines()[-1])
    assert log["outcome"] == "decision_failed" and log["error_code"] == "http_error"


def test_streaming_is_passed_through(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["o4-mini", "gpt-5.5"]))
    with client_for(make_pipeline, settings) as client:
        response = client.post("/v1/chat/completions", json={"messages": [{"role": "user", "content": "x"}],
                                                              "stream": True})
    assert response.status_code == 200
    assert response.headers["content-type"].startswith("text/event-stream")
    assert "answer from o4-mini" in response.text and response.text.rstrip().endswith("data: [DONE]")


def test_request_validation(make_pipeline, settings):
    with client_for(make_pipeline, settings) as client:
        assert client.post("/v1/chat/completions", json={"messages": []}).status_code == 400
        bad_mode = client.post("/v1/chat/completions", json={"messages": [{"role": "user", "content": "x"}],
                                                              "routing_mode": "cheap"})
        assert bad_mode.status_code == 400 and bad_mode.json()["error"]["code"] == "invalid_routing_mode"
        assert client.post("/v1/chat/completions", content=b"{not json",
                           headers={"content-type": "application/json"}).status_code == 400


def test_health_and_metrics(make_pipeline, settings, fake):
    with client_for(make_pipeline, settings) as client:
        assert client.get("/health/live").status_code == 200
        assert client.get("/health/ready").json()["default_mode"] == "balanced"
        client.post("/v1/chat/completions", json={"messages": [{"role": "user", "content": "x"}]})
        metrics = client.get("/metrics").text
    assert "router_requests_total" in metrics and "router_decision_latency_ms_bucket" in metrics
