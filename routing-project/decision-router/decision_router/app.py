from __future__ import annotations

import json
import uuid
from contextlib import asynccontextmanager
from dataclasses import replace
from typing import Any

from fastapi import FastAPI, Request, Response
from fastapi.responses import JSONResponse, StreamingResponse

from . import __version__
from .auth import FoundryAuth
from .catalog import Catalogs
from .config import Settings
from .decision1 import Decision1Client
from .foundry import ChatClient
from .inbound import InboundAuth, Unauthorized
from .pipeline import ProviderError, Routed, RouterPipeline, RoutingError
from .pricing import PriceTable
from .telemetry import Telemetry


def build_pipeline(settings: Settings) -> RouterPipeline:
    catalogs = Catalogs.load(settings.catalog_dir, settings.compatibility, settings.deployment_overrides,
                             settings.default_mode)
    if not settings.configured:
        raise RuntimeError("set FOUNDRY_ENDPOINT (or DECISION1_ENDPOINT and FOUNDRY_CHAT_COMPLETIONS_URL)")
    chat_auth = FoundryAuth(settings.foundry_api_key)
    decision_auth = FoundryAuth(settings.decision1_api_key) if settings.decision1_api_key else chat_auth
    return RouterPipeline(
        settings,
        catalogs,
        Decision1Client(settings.resolved_decision1_url or "", settings.decision1_deployment, decision_auth,
                        timeout_seconds=settings.decision_timeout_seconds,
                        max_attempts=settings.decision_max_attempts),
        ChatClient(settings, chat_auth),
        Telemetry(settings.decision_log_path, settings.log_prompts),
        PriceTable(settings.pricing_path),
    )


def _routing_headers(routed: Routed, served: str | None = None) -> dict[str, str]:
    headers = {
        "x-request-id": routed.request_id,
        "x-router-mode": routed.mode,
        "x-router-compatibility": routed.catalog.compatibility,
        "x-router-ranking": ",".join(routed.decision.ranking),
        "x-router-low-confidence": str(routed.low_confidence).lower(),
    }
    if served:
        headers["x-router-served-model"] = served
    return headers


def build_inbound_auth(settings: Settings) -> InboundAuth:
    return InboundAuth(settings.api_keys, settings.entra_tenant_id, settings.entra_audiences,
                       settings.entra_allowed_client_ids, settings.allow_unauthenticated)


def create_app(settings: Settings | None = None, *, pipeline: RouterPipeline | None = None,
               inbound: InboundAuth | None = None) -> FastAPI:
    settings = settings or Settings.from_env()
    gate = inbound or build_inbound_auth(settings)  # refuses to start with no inbound auth configured
    pipe = pipeline or build_pipeline(settings)

    @asynccontextmanager
    async def lifespan(_: FastAPI):
        yield
        await pipe.close()

    app = FastAPI(
        title="Decision Router",
        version=__version__,
        description="Model-Router-compatible chat completions: stage-1 filter, Microsoft-Decision-1 ranking, "
                    "then the top-ranked Foundry model.",
        lifespan=lifespan,
    )
    app.state.pipeline = pipe

    def request_id(request: Request) -> str:
        value = request.headers.get("x-request-id") or request.headers.get("x-ms-client-request-id")
        return value[:128] if value else str(uuid.uuid4())

    async def read_body(request: Request) -> Any:
        try:
            return await request.json()
        except (json.JSONDecodeError, UnicodeDecodeError) as exc:
            raise RoutingError(400, "invalid_json", "request body is not valid JSON") from exc

    @app.exception_handler(RoutingError)
    async def routing_error(_: Request, exc: RoutingError) -> JSONResponse:
        return JSONResponse(exc.body(), status_code=exc.status_code)

    @app.exception_handler(Unauthorized)
    async def unauthorized(_: Request, exc: Unauthorized) -> JSONResponse:
        return JSONResponse({"error": {"message": exc.message, "type": "authentication_error", "code": "unauthorized"}},
                            status_code=401, headers={"WWW-Authenticate": "Bearer"})

    async def prepare(request: Request) -> Any:
        caller = await gate.check(request.headers)
        return replace(pipe.prepare(await read_body(request)), caller=caller.log())

    @app.exception_handler(ProviderError)
    async def provider_error(_: Request, exc: ProviderError) -> Response:
        return Response(exc.content, status_code=exc.status_code, media_type=exc.content_type)

    @app.post("/v1/chat/completions")
    @app.post("/openai/v1/chat/completions")
    async def chat_completions(request: Request) -> Response:
        rid = request_id(request)
        prepared = await prepare(request)
        routed = await pipe.route(prepared, rid)
        body = prepared.body
        if body.get("stream"):
            result = await pipe.stream(routed, body)
            return StreamingResponse(result.chunks, status_code=result.status_code, media_type=result.content_type,
                                     headers=_routing_headers(routed, result.served.name))
        completion = await pipe.complete(routed, body)
        return Response(completion.content, status_code=completion.status_code, media_type=completion.content_type,
                        headers=_routing_headers(routed, completion.served.name))

    @app.post("/v1/route")
    async def route_only(request: Request) -> JSONResponse:
        """Stages 1 and 2 only: what would be called, without calling it."""
        rid = request_id(request)
        prepared = await prepare(request)
        routed = await pipe.route(prepared, rid)
        pipe.record_route_only(routed, prepared.body)
        decision = routed.decision
        return JSONResponse(
            {
                "request_id": rid,
                "routing_mode": routed.mode,
                "compatibility": routed.catalog.compatibility,
                "candidates": [m.name for m in routed.candidates],
                "stage1": routed.event["stage1"],
                "ranking": [{"model": name, "probability": decision.probabilities[name]} for name in decision.ranking],
                "execution_order": routed.order,
                "low_confidence": routed.low_confidence,
                "decision": {"choice": decision.choice, "confidence": decision.confidence,
                             "latency_ms": decision.latency_ms, "skipped": decision.skipped},
            },
            headers=_routing_headers(routed),
        )

    @app.get("/health/live")
    async def live() -> dict[str, Any]:
        return {"status": "ok", "version": __version__}

    @app.get("/health/ready")
    async def ready() -> dict[str, Any]:
        return {
            "status": "ready",
            "inbound_auth": gate.methods or ["none"],
            "profiles": sorted(settings.profiles),
            "default_compatibility": pipe.catalogs.default,
            "catalogs": {name: c.describe() for name, c in pipe.catalogs.catalogs.items()},
            "decision1_deployment": pipe.decision.deployment,
        }

    @app.get("/metrics")
    async def metrics() -> Response:
        return Response(pipe.telemetry.render(), media_type="text/plain; version=0.0.4")

    return app
