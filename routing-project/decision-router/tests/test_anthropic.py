import asyncio
import json

import httpx

from decision_router.anthropic import ChatCompletionStream, error_body, from_messages_response, to_messages_request

TOOLS = [{"type": "function", "function": {"name": "get_weather", "description": "Weather",
                                           "parameters": {"type": "object", "properties": {"city": {"type": "string"}}}}}]


def test_request_translation():
    body = {
        "messages": [
            {"role": "system", "content": "Be brief."},
            {"role": "developer", "content": "Use metric."},
            {"role": "user", "content": [{"type": "text", "text": "Weather?"},
                                         {"type": "image_url", "image_url": {"url": "data:image/png;base64,AAAA"}}]},
            {"role": "assistant", "content": None, "tool_calls": [
                {"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\": \"Oslo\"}"}}]},
            {"role": "tool", "tool_call_id": "call_1", "content": "5C"},
            {"role": "user", "content": "And tomorrow?"},
        ],
        "tools": TOOLS, "tool_choice": "required", "parallel_tool_calls": False,
        "max_completion_tokens": 300, "temperature": 0.2, "stop": "END", "user": "u1",
    }
    request = to_messages_request(body, "claude-deploy", 4096)
    assert request["model"] == "claude-deploy" and request["max_tokens"] == 300
    assert request["system"] == "Be brief.\n\nUse metric."
    assert request["stop_sequences"] == ["END"] and request["metadata"] == {"user_id": "u1"}
    assert request["tools"][0] == {"name": "get_weather", "description": "Weather",
                                   "input_schema": TOOLS[0]["function"]["parameters"]}
    assert request["tool_choice"] == {"type": "any", "disable_parallel_tool_use": True}
    roles = [m["role"] for m in request["messages"]]
    assert roles == ["user", "assistant", "user"]  # tool result and the next user turn merge
    assert request["messages"][0]["content"][1] == {"type": "image", "source": {"type": "base64",
                                                                               "media_type": "image/png", "data": "AAAA"}}
    assert request["messages"][1]["content"][0] == {"type": "tool_use", "id": "call_1", "name": "get_weather",
                                                    "input": {"city": "Oslo"}}
    assert request["messages"][2]["content"][0] == {"type": "tool_result", "tool_use_id": "call_1", "content": "5C"}


def test_default_max_tokens_and_specific_tool_choice():
    request = to_messages_request({"messages": [{"role": "user", "content": "hi"}], "tools": TOOLS,
                                   "tool_choice": {"type": "function", "function": {"name": "get_weather"}}}, "d", 777)
    assert request["max_tokens"] == 777 and request["tool_choice"] == {"type": "tool", "name": "get_weather"}


def test_json_schema_becomes_output_config_format():
    schema = {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"],
              "additionalProperties": False}
    request = to_messages_request({"messages": [{"role": "user", "content": "hi"}], "response_format": {
        "type": "json_schema", "json_schema": {"name": "place", "strict": True, "schema": schema}}}, "d", 100)
    assert request["output_config"] == {"format": {"type": "json_schema", "schema": schema}}
    plain = to_messages_request({"messages": [{"role": "user", "content": "hi"}],
                                 "response_format": {"type": "json_object"}}, "d", 100)
    assert "output_config" not in plain


def test_response_translation():
    message = {"id": "msg_1", "model": "claude-opus-5", "stop_reason": "tool_use",
               "content": [{"type": "thinking", "thinking": "..."}, {"type": "text", "text": "Checking."},
                           {"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {"city": "Oslo"}}],
               "usage": {"input_tokens": 10, "cache_read_input_tokens": 4, "output_tokens": 7}}
    out = from_messages_response(message)
    choice = out["choices"][0]
    assert out["object"] == "chat.completion" and out["model"] == "claude-opus-5"
    assert choice["finish_reason"] == "tool_calls" and choice["message"]["content"] == "Checking."
    assert choice["message"]["tool_calls"][0]["function"] == {"name": "get_weather", "arguments": "{\"city\": \"Oslo\"}"}
    assert out["usage"] == {"prompt_tokens": 14, "completion_tokens": 7, "total_tokens": 21,
                            "prompt_tokens_details": {"cached_tokens": 4}}


def test_error_translation():
    body = json.loads(error_body(b'{"type":"error","error":{"type":"overloaded_error","message":"busy"}}', 529))
    assert body == {"error": {"message": "busy", "type": "overloaded_error", "code": "529"}}
    assert json.loads(error_body(b"not json", 500))["error"]["code"] == "500"


def test_stream_translation_with_tool_calls():
    events = [
        {"type": "message_start", "message": {"id": "msg_1", "model": "claude-sonnet-5", "usage": {"input_tokens": 9}}},
        {"type": "content_block_start", "index": 0, "content_block": {"type": "text", "text": ""}},
        {"type": "content_block_delta", "index": 0, "delta": {"type": "text_delta", "text": "Hi"}},
        {"type": "content_block_start", "index": 1, "content_block": {"type": "tool_use", "id": "toolu_1",
                                                                      "name": "get_weather", "input": {}}},
        {"type": "content_block_delta", "index": 1, "delta": {"type": "input_json_delta", "partial_json": "{\"city\":"}},
        {"type": "content_block_delta", "index": 1, "delta": {"type": "input_json_delta", "partial_json": "\"Oslo\"}"}},
        {"type": "message_delta", "delta": {"stop_reason": "tool_use"}, "usage": {"output_tokens": 3}},
        {"type": "message_stop"},
    ]
    raw = "".join(f"event: {e['type']}\ndata: {json.dumps(e)}\n\n" for e in events).encode()

    async def run():
        upstream = httpx.Response(200, content=raw)
        chunks = [c async for c in ChatCompletionStream(upstream, include_usage=True)]
        return b"".join(chunks).decode()

    text = asyncio.run(run())
    payloads = [json.loads(line[6:]) for line in text.splitlines() if line.startswith("data: {")]
    deltas = [p["choices"][0]["delta"] for p in payloads if p["choices"]]
    assert deltas[0] == {"role": "assistant", "content": ""} and deltas[1] == {"content": "Hi"}
    assert deltas[2]["tool_calls"][0] == {"index": 0, "id": "toolu_1", "type": "function",
                                          "function": {"name": "get_weather", "arguments": ""}}
    arguments = "".join(d["tool_calls"][0]["function"]["arguments"] for d in deltas[3:5])
    assert arguments == "{\"city\":\"Oslo\"}"
    assert [p["choices"][0]["finish_reason"] for p in payloads if p["choices"]][-1] == "tool_calls"
    assert payloads[-1]["usage"]["prompt_tokens"] == 9 and payloads[-1]["usage"]["completion_tokens"] == 3
    assert text.rstrip().endswith("data: [DONE]")
