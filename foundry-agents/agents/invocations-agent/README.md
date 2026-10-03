# invocations-agent

Hosted agent using the **Invocations** protocol. Choose it when callers need their own JSON
contract instead of the OpenAI Responses format. Same tools as `responses-agent`.

## Contract

```http
POST /invocations
{"question": "How do I rotate an API key?", "stream": false, "max_output_tokens": 256}
```

* `question` is required, 1 to 8000 characters. Unknown fields are rejected.
* `stream: true` returns server-sent events: `event: delta` frames, then `event: done`.
* Invalid input returns HTTP 400 and costs no model call.

| File | Purpose |
| --- | --- |
| `schemas.py` | Pydantic request and response models, and the generated OpenAPI document. |
| `parsing.py` | Turns a request into an agent run. |
| `main.py` | Hosts the agent with `InvocationsHostServer`. |
| `agent.py`, `settings.py`, `instructions.md` | As in `responses-agent`. |

The end-to-end tests in `tests/test_http.py` drive the real host over HTTP with a fake model.

```bash
uv sync && uv run pytest
```
