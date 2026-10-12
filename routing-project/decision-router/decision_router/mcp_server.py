"""MCP server: the router's routing and chat as Model Context Protocol tools, at /mcp on the same port.

Transport: streamable HTTP, stateless, JSON responses (no server-side sessions, so any replica can
answer any request). Point an MCP client at http(s)://<host>:5001/mcp/.

Callers authenticate exactly as on the REST API (API key or Entra token, see inbound.py) and the
same caller policies, profiles, pinning, telemetry and degraded fallback apply, because every tool
goes through RouterPipeline.

Tools:
- route       what stage 1 keeps and how Decision-1 ranks it, without calling a model (= POST /v1/route)
- chat        route and answer (= POST /v1/chat/completions without streaming)
- list_models the catalog: prices, capabilities, actions and the cost bands each model is in
"""
from __future__ import annotations

import json
import uuid
from dataclasses import replace
from typing import Any

from mcp.server.mcpserver import Context, MCPServer
from mcp.server.mcpserver.exceptions import ToolError
from mcp.server.transport_security import TransportSecuritySettings
from starlette.datastructures import Headers

from . import __version__
from .catalog import CAPABILITY_TAGS
from .inbound import InboundAuth, Unauthorized
from .pinning import SESSION_HEADER
from .pipeline import ProviderError, RouterPipeline, RoutingError

INSTRUCTIONS = (
    "Routes chat requests to the most suitable Microsoft Foundry model. Use `chat` to get an answer, "
    "`route` to see which model would answer and why without calling it, and `list_models` to see the "
    "models, their prices, capabilities, actions and cost bands. Optional fields: routing_mode "
    "(cost|balanced|quality), compatibility (old|new catalog), routing_cost_band (low|medium|"
    "medium-high|high|top or a dollar range such as '1-3'), routing_constraints (models, providers, "
    "capabilities, actions, region, ...)."
)


def _body(messages: list[dict[str, Any]], **fields: Any) -> dict[str, Any]:
    """A chat-completions body from the tool arguments, leaving out the ones not given."""
    return {"messages": messages, **{k: v for k, v in fields.items() if v is not None}}


def build_mcp(pipe: RouterPipeline, gate: InboundAuth) -> tuple[MCPServer, Any]:
    """The MCP server and the ASGI app to mount at /mcp (authenticated with the REST API's gate)."""
    mcp: MCPServer = MCPServer(name="decision-router", title="Decision Router", instructions=INSTRUCTIONS,
                               version=__version__)

    async def prepared(ctx: Context, body: dict[str, Any], session_id: str | None):
        caller = await gate.check(Headers(ctx.headers or {}))
        session = session_id or (ctx.headers or {}).get(SESSION_HEADER)
        try:
            prepared = replace(pipe.prepare(body), caller=caller.log(), session_id=session or None)
            return pipe.apply_policy(prepared)
        except RoutingError as exc:
            raise ToolError(_error(exc)) from exc

    @mcp.tool(description="Route a chat request and return the answer from the model the router picks. "
                          "Same as POST /v1/chat/completions (no streaming).")
    async def chat(ctx: Context, messages: list[dict[str, Any]], model: str | None = None,
                   routing_mode: str | None = None, compatibility: str | None = None,
                   routing_cost_band: str | dict[str, float] | None = None,
                   routing_constraints: dict[str, Any] | None = None, routing_pin: bool | None = None,
                   tools: list[dict[str, Any]] | None = None, response_format: dict[str, Any] | None = None,
                   max_completion_tokens: int | None = None, temperature: float | None = None,
                   reasoning_effort: str | None = None, session_id: str | None = None) -> dict[str, Any]:
        """`model` may name a routing profile (e.g. decision-router-coding). `session_id` keys pinning."""
        body = _body(messages, model=model, routing_mode=routing_mode, compatibility=compatibility,
                     routing_cost_band=routing_cost_band, routing_constraints=routing_constraints,
                     routing_pin=routing_pin, tools=tools, response_format=response_format,
                     max_completion_tokens=max_completion_tokens, temperature=temperature,
                     reasoning_effort=reasoning_effort)
        prep = await prepared(ctx, body, session_id)
        try:
            routed = await pipe.route(prep, f"mcp-{uuid.uuid4()}")
            completion = await pipe.complete(routed, prep.body)
        except RoutingError as exc:
            raise ToolError(_error(exc)) from exc
        except ProviderError as exc:
            raise ToolError(f"the model returned {exc.status_code}: {exc.content.decode('utf-8', 'replace')[:500]}") from exc
        response = json.loads(completion.content)
        choice = (response.get("choices") or [{}])[0]
        message = choice.get("message") or {}
        return {
            "model": completion.served.name,
            "content": message.get("content"),
            "tool_calls": message.get("tool_calls"),
            "finish_reason": choice.get("finish_reason"),
            "usage": response.get("usage"),
            "routing": routed.summary(),
        }

    @mcp.tool(description="Show which models pass the stage-1 filter for a request, how Decision-1 ranks "
                          "them and the order they would be called in, without calling a model. "
                          "Same as POST /v1/route.")
    async def route(ctx: Context, messages: list[dict[str, Any]], model: str | None = None,
                    routing_mode: str | None = None, compatibility: str | None = None,
                    routing_cost_band: str | dict[str, float] | None = None,
                    routing_constraints: dict[str, Any] | None = None,
                    tools: list[dict[str, Any]] | None = None, response_format: dict[str, Any] | None = None,
                    max_completion_tokens: int | None = None) -> dict[str, Any]:
        body = _body(messages, model=model, routing_mode=routing_mode, compatibility=compatibility,
                     routing_cost_band=routing_cost_band, routing_constraints=routing_constraints, tools=tools,
                     response_format=response_format, max_completion_tokens=max_completion_tokens)
        prep = await prepared(ctx, body, None)
        try:
            routed = await pipe.route(prep, f"mcp-{uuid.uuid4()}")
        except RoutingError as exc:
            raise ToolError(_error(exc)) from exc
        pipe.record_route_only(routed, prep.body)
        return routed.summary()

    @mcp.tool(description="List the models in a catalog with their output and input prices (USD per "
                          "million tokens), price tier, capabilities, actions and the named cost bands "
                          "each is in. Optionally only those tagged for an action or in a cost band.")
    async def list_models(ctx: Context, compatibility: str = "new", action: str | None = None,
                          cost_band: str | None = None) -> dict[str, Any]:
        await gate.check(Headers(ctx.headers or {}))
        try:
            catalog = pipe.catalogs.get(compatibility)
        except ValueError as exc:
            raise ToolError(str(exc)) from exc
        if action is not None and action not in CAPABILITY_TAGS["actions"]:
            raise ToolError(f"action must be one of {list(CAPABILITY_TAGS['actions'])}")
        if cost_band is not None and cost_band not in catalog.cost_bands:
            raise ToolError(f"the '{compatibility}' catalog's cost bands are {list(catalog.cost_bands) or 'none'}")
        models = []
        for m in catalog.models:
            bands = [n for n, b in catalog.cost_bands.items() if m.output_price is not None and b.contains(m.output_price)]
            actions = list(m.capabilities.get("actions", []))
            if (action and action not in actions) or (cost_band and cost_band not in bands):
                continue
            models.append({
                "name": m.name, "provider": m.provider, "tier": m.tier, "lifecycle": m.lifecycle,
                "price_per_million": {"input": m.input_price, "output": m.output_price},
                "capabilities": {k: v for k, v in m.capabilities.items() if k not in CAPABILITY_TAGS},
                "actions": actions, "execution": list(m.capabilities.get("execution", [])),
                "cost_bands": bands, "context_tokens": m.context_tokens, "description": m.description,
            })
        return {"compatibility": catalog.compatibility, "count": len(models), "models": models,
                "cost_bands": {n: b.as_dict() for n, b in catalog.cost_bands.items()}}

    inner = mcp.streamable_http_app(
        streamable_http_path="/", stateless_http=True, json_response=True,
        max_request_body_size=pipe.settings.max_body_bytes,
        # Host-header checks protect servers bound to localhost from DNS rebinding; this one is a
        # network service behind authentication, reached under whatever host name the deployment uses.
        transport_security=TransportSecuritySettings(enable_dns_rebinding_protection=False),
    )

    async def authenticated(scope, receive, send):
        """Reject unauthenticated MCP requests before they reach the MCP server."""
        if scope["type"] == "http":
            try:
                await gate.check(Headers(scope=scope))
            except Unauthorized as exc:
                body = json.dumps({"error": {"code": "unauthorized", "message": exc.message}}).encode()
                await send({"type": "http.response.start", "status": 401,
                            "headers": [(b"content-type", b"application/json"),
                                        (b"www-authenticate", b"Bearer")]})
                await send({"type": "http.response.body", "body": body})
                return
        await inner(scope, receive, send)

    return mcp, authenticated


def _error(exc: RoutingError) -> str:
    error = exc.body().get("error", {})
    return f"{exc.status_code} {error.get('code')}: {error.get('message')}"
