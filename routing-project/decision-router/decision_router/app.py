from __future__ import annotations

import json
import uuid
from contextlib import asynccontextmanager
from typing import Any

from fastapi import FastAPI, Request, Response
from fastapi.responses import JSONResponse, StreamingResponse

from . import __version__
from .auth import FoundryAuth
from .catalog import Catalog
from .config import Settings
from .decision1 import Decision1Client
from .foundry import ChatClient
from .pipeline import ProviderError, Routed, RouterPipeline, RoutingError
from .pricing import PriceTable
from .telemetry import Telemetry


def build_pipeline(settings: Settings) -> RouterPipeline:
    catalog = Catalog.load(settings.catalog_path, settings.deployment_overrides, settings.default_mode)
    if not settings.configured:
        raise RuntimeError("set FOUNDRY_ENDPOINT (or DECISION1_ENDPOINT and FOUNDRY_CHAT_COMPLETIONS_URL)")
    chat_auth = FoundryAuth(settings.foundry_api_key)
    decision_auth = FoundryAuth(settings.decision1_api_key) if settings.decision1_api_key else chat_auth
    return RouterPipeline(
        settings,
        catalog,
        Decision1Client(settings.resolved_decision1_url or "", settings.decision1_deployment, decision_auth,
                        timeout_seconds=settings.decision_timeout_seconds),
        ChatClient(settings.resolved_chat_url or "", chat_auth),
        Telemetry(settings.decision_log_path, settings.log_prompts),
        PriceTable(settings.pricing_path),
    )


def _routing_headers(routed: Routed, served: str | None = None) -> dict[str, str]:
    headers = {
        "x-request-id": routed.request_id,
        "x-router-mode": routed.mode,
        "x-router-ranking": ",".join(routed.decision.ranking),
    }
    if served:
        headers["x-router-served-model"] = served
    return headers


def create_app(settings: Settings | None = None, *, pipeline: RouterPipeline | None = None) -> FastAPI:
    settings = settings or Settings.from_env()
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

    @app.exception_handler(ProviderError)
    async def provider_error(_: Request, exc: ProviderError) -> Response:
        return Response(exc.content, status_code=exc.status_code, media_type=exc.content_type)

    @app.post("/v1/chat/completions")
    @app.post("/openai/v1/chat/completions")
    async def chat_completions(request: Request) -> Response:
        rid = request_id(request)
        mode, body = pipe.prepare(await read_body(request))
        routed = await pipe.route(body, mode, rid)
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
        mode, body = pipe.prepare(await read_body(request))
        routed = await pipe.route(body, mode, rid)
        pipe.record_route_only(routed, body)
        decision = routed.decision
        return JSONResponse(
            {
                "request_id": rid,
                "routing_mode": mode,
                "candidates": [m.name for m in routed.candidates],
                "ranking": [{"model": name, "probability": decision.probabilities[name]} for name in decision.ranking],
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
            "catalog_sha256": pipe.catalog.fingerprint,
            "models": [m.name for m in pipe.catalog.models],
            "default_mode": pipe.catalog.default_mode,
            "decision1_deployment": pipe.decision.deployment,
        }

    @app.get("/metrics")
    async def metrics() -> Response:
        return Response(pipe.telemetry.render(), media_type="text/plain; version=0.0.4")

    return app
