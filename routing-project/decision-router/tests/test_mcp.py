import json
from dataclasses import replace

from fastapi.testclient import TestClient

from decision_router.app import create_app
from decision_router.testing import ranked

HEADERS = {"accept": "application/json, text/event-stream", "content-type": "application/json"}
USER = [{"role": "user", "content": "Compare two vendors."}]


def rpc(client, method, params=None, key=None, rid=1):
    headers = {**HEADERS, **({"api-key": key} if key else {})}
    return client.post("/mcp/", headers=headers, json={"jsonrpc": "2.0", "id": rid, "method": method,
                                                       "params": params or {}})


def call(client, tool, arguments, key=None):
    response = rpc(client, "tools/call", {"name": tool, "arguments": arguments}, key)
    assert response.status_code == 200, response.text
    result = response.json()["result"]
    text = result["content"][0]["text"]
    return result["isError"], (text if result["isError"] else json.loads(text))


def test_mcp_lists_the_three_tools(make_pipeline, settings):
    with TestClient(create_app(settings, pipeline=make_pipeline())) as c:
        init = rpc(c, "initialize", {"protocolVersion": "2025-06-18", "capabilities": {},
                                     "clientInfo": {"name": "test", "version": "1"}})
        tools = rpc(c, "tools/list", rid=2).json()["result"]["tools"]
    assert init.json()["result"]["serverInfo"]["name"] == "decision-router"
    assert sorted(t["name"] for t in tools) == ["chat", "list_models", "route"]
    chat = next(t for t in tools if t["name"] == "chat")
    assert "messages" in chat["inputSchema"]["required"] and "routing_cost_band" in chat["inputSchema"]["properties"]


def test_chat_routes_and_answers(make_pipeline, settings, fake, tmp_path):
    fake.decision_responses.append(ranked(["gpt-5.6-terra", "gpt-5.5"]))
    with TestClient(create_app(settings, pipeline=make_pipeline())) as c:
        error, result = call(c, "chat", {"messages": USER})
    assert not error and result["model"] == "gpt-5.6-terra" and result["content"] == "answer from gpt-5.6-terra"
    assert result["usage"]["completion_tokens"] == 5 and result["routing"]["execution_order"][0] == "gpt-5.6-terra"
    assert json.loads((tmp_path / "decisions.jsonl").read_text().splitlines()[-1])["outcome"] == "success"


def test_route_and_list_models(make_pipeline, settings, fake):
    fake.decision_responses.append(ranked(["claude-sonnet-5-5"]))
    with TestClient(create_app(settings, pipeline=make_pipeline())) as c:
        _, routed = call(c, "route", {"messages": USER, "compatibility": "new", "routing_cost_band": "high"})
        _, coding = call(c, "list_models", {"compatibility": "new", "action": "code", "cost_band": "top"})
        bad, message = call(c, "list_models", {"action": "review"})
    assert sorted(routed["candidates"]) == ["claude-opus-5-5", "claude-sonnet-5-5", "gpt-5.5", "gpt-6.1-sol", "grok-4.7"]
    assert [m["name"] for m in coding["models"]] == ["gpt-6-astra"]
    assert coding["models"][0]["price_per_million"] == {"input": 10.0, "output": 50.0}
    assert bad and "action must be one of" in message
    assert not chat_called(fake)


def chat_called(fake):
    return [r for r in fake.requests if r["path"].endswith(("/chat/completions", "/anthropic/v1/messages"))]


def test_mcp_uses_the_rest_apis_auth_and_caller_policies(make_pipeline, settings, fake):
    s = replace(settings, allow_unauthenticated=False, api_keys=("k1",),
                caller_policies={"api_key:1": {"catalogs": ["new"]}})
    with TestClient(create_app(s, pipeline=make_pipeline(caller_policies=s.caller_policies))) as c:
        anonymous = rpc(c, "tools/list")
        denied, message = call(c, "route", {"messages": USER, "compatibility": "old"}, key="k1")
    assert anonymous.status_code == 401
    assert denied and "403 policy_denied" in message


def test_mcp_can_be_turned_off(make_pipeline, settings):
    s = replace(settings, mcp_enabled=False)
    with TestClient(create_app(s, pipeline=make_pipeline())) as c:
        assert rpc(c, "tools/list").status_code == 404


def test_mcp_answers_with_and_without_the_trailing_slash(make_pipeline, settings):
    with TestClient(create_app(settings, pipeline=make_pipeline())) as c:
        bare = c.post("/mcp", headers=HEADERS, follow_redirects=False,
                      json={"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {}})
    assert bare.status_code == 200 and len(bare.json()["result"]["tools"]) == 3
