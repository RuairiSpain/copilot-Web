# MCP server

The router's container serves the REST API and a Model Context Protocol (MCP) server on the
same port (5001). Code: `decision-router/decision_router/mcp_server.py`.

| | |
|---|---|
| Endpoint | `http(s)://<host>:5001/mcp` (with or without the trailing slash) |
| Transport | Streamable HTTP, stateless, JSON responses: no server-side sessions, so any replica can answer any request |
| Authentication | The same as the REST API: `api-key: <key>` or `Authorization: Bearer <key or Entra token>` (`AGENT_SERVICE.md`). Requests without valid credentials get 401 |
| Turn off | `ROUTER_MCP_ENABLED=false` |

Every tool goes through the same pipeline as the REST API, so caller policies, routing
profiles, conversation pinning, the decision log, the degraded fallback and Prometheus metrics
all apply unchanged.

## Tools

### `chat`

Routes a chat request and returns the answer, as `POST /v1/chat/completions` does without
streaming.

| Argument | |
|---|---|
| `messages` (required) | Chat-completions messages |
| `model` | A routing profile such as `decision-router-coding`; other values are ignored |
| `routing_mode`, `compatibility`, `routing_cost_band`, `routing_constraints`, `routing_pin` | As on the REST API (`STAGE1_FILTERING.md`) |
| `tools`, `response_format`, `max_completion_tokens`, `temperature`, `reasoning_effort` | Passed to the model |
| `session_id` | Keys conversation pinning, like the `x-router-session-id` header |

Returns `model` (the model that answered), `content`, `tool_calls`, `finish_reason`, `usage`
and `routing`: the same summary `route` returns.

### `route`

Shows which models pass stage 1, how Decision-1 ranks them, the order they would be called in
and the exact instructions and criteria Decision-1 received, without calling a model. Same body
as `POST /v1/route`. Takes the routing arguments of `chat` (no sampling parameters).

### `list_models`

Lists a catalog's models with their input and output price (USD per million tokens), price
tier, yes/no capabilities, actions, execution tags, context window and the named cost bands
each model is in.

| Argument | |
|---|---|
| `compatibility` | `new` (default) or `old` |
| `action` | Only models tagged for this action (`code`, `write`, `plan`, ...) |
| `cost_band` | Only models in this named band (`low`, `medium`, `medium-high`, `high`, `top`) |

## Connecting a client

Any MCP client that supports streamable HTTP and custom headers can connect. A typical client
configuration (`mcp.json`-style):

```json
{"servers": {"decision-router": {
  "type": "http",
  "url": "https://router.example.com:5001/mcp",
  "headers": {"api-key": "<one of ROUTER_API_KEYS>"}}}}
```

Raw JSON-RPC, for testing:

```bash
curl -s -X POST localhost:5001/mcp -H "api-key: $KEY" -H 'content-type: application/json' \
  -H 'accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_models","arguments":{"action":"code","cost_band":"medium"}}}'
```

Tool errors (an invalid argument, a request a caller policy denies, no eligible models, a model
error) come back as tool results with `isError: true` and the router's error code in the text,
for example `403 policy_denied: ...` or `422 no_eligible_models: ...`.

## Notes

- Host-header checks (DNS-rebinding protection) are off: the SDK's default allows only
  `localhost`, which would reject every request reaching the container under its service name.
  The server is reachable only with valid credentials.
- Request bodies are limited to `ROUTER_MAX_BODY_BYTES`, as on the REST API.
- Streaming answers are not offered over MCP; use the REST API for streaming.
