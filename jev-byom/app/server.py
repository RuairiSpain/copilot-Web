"""FastAPI service: the BYOM endpoint Foundry calls.

    POST /posthoc_train   fit and persist calibration for one scenario
    POST /decision        one typed, calibrated decision
    GET  /scenarios       what has been trained (debugging and tooling)
    GET  /health          liveness, model and registry state

Handlers are ``def``, not ``async def``: scoring a transformer and talking to
blob storage both block, and FastAPI runs sync handlers in a worker thread,
which keeps the event loop free. Making them ``async`` would stall every other
request for the duration of a forward pass.
"""

from __future__ import annotations

import logging
import os
import secrets
import time
from contextlib import asynccontextmanager
from typing import Annotated, Any, AsyncIterator

from fastapi import Depends, FastAPI, Header, Request, status
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from app import model as model_module
from app.calibration_registry import (
    CalibrationMismatchError,
    CalibrationRegistry,
    RegistryError,
    build_object_store,
)
from app.engine import EngineError, decide, train_scenario
from app.logging_config import configure_logging
from app.model import ModelInputError
from app.schemas import (
    DecisionRequest,
    DecisionResponse,
    ErrorResponse,
    HealthResponse,
    PosthocTrainRequest,
    PosthocTrainResponse,
    ScenarioInfo,
    ScenarioListResponse,
)
from config.settings import Settings, get_settings

logger = logging.getLogger(__name__)

API_TITLE = "Jev BYOM decision engine"
API_VERSION = "1.0.0"


def _error(status_code: int, error: str, detail: str, **extra: Any) -> JSONResponse:
    body = ErrorResponse(error=error, detail=detail, **extra)
    return JSONResponse(status_code=status_code, content=body.model_dump(exclude_none=True))


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    """Build the registry and (optionally) load the model before serving."""
    settings = get_settings()
    configure_logging(settings.log_level, settings.log_json, settings.service_name)

    app.state.settings = settings
    app.state.registry = CalibrationRegistry(build_object_store(settings), settings)
    logger.info("registry ready", extra={"registry": app.state.registry.description})

    if settings.model_eager_load:
        started = time.perf_counter()
        model_module.set_model(model_module.build_model(settings))
        logger.info(
            "base model ready",
            extra={
                "model_backend": settings.model_backend,
                "model_name": settings.model_name,
                "load_ms": round((time.perf_counter() - started) * 1000.0, 1),
            },
        )
    try:
        yield
    finally:
        app.state.registry.clear_cache()


app = FastAPI(
    title=API_TITLE,
    version=API_VERSION,
    summary="Typed, scenario-calibrated decisions for Azure AI Foundry BYOM.",
    lifespan=lifespan,
)


# ----------------------------------------------------------------------------
# Dependencies
# ----------------------------------------------------------------------------
def get_app_settings(request: Request) -> Settings:
    return getattr(request.app.state, "settings", None) or get_settings()


def get_registry(request: Request) -> CalibrationRegistry:
    registry = getattr(request.app.state, "registry", None)
    if registry is None:  # pragma: no cover - lifespan always sets it
        registry = CalibrationRegistry(build_object_store(get_settings()), get_settings())
        request.app.state.registry = registry
    return registry


def require_api_key(
    settings: Annotated[Settings, Depends(get_app_settings)],
    x_api_key: Annotated[str | None, Header()] = None,
    authorization: Annotated[str | None, Header()] = None,
) -> None:
    """Check the shared secret, when one is configured.

    Foundry's model connection sends either header depending on how the
    connection is set up, so both are accepted. The comparison is constant-time.
    """
    if not settings.api_key:
        return
    presented = x_api_key
    if not presented and authorization and authorization.lower().startswith("bearer "):
        presented = authorization[7:].strip()
    if not presented or not secrets.compare_digest(presented, settings.api_key):
        raise EngineError("missing or invalid API key", status_code=401, error="unauthorized")


SettingsDep = Annotated[Settings, Depends(get_app_settings)]
RegistryDep = Annotated[CalibrationRegistry, Depends(get_registry)]
AuthDep = Annotated[None, Depends(require_api_key)]


# ----------------------------------------------------------------------------
# Error handlers
# ----------------------------------------------------------------------------
@app.exception_handler(EngineError)
async def _engine_error_handler(request: Request, exc: EngineError) -> JSONResponse:
    logger.warning("request rejected", extra={"error": exc.error, "detail": str(exc)})
    return _error(exc.status_code, exc.error, str(exc))


@app.exception_handler(CalibrationMismatchError)
async def _mismatch_handler(request: Request, exc: CalibrationMismatchError) -> JSONResponse:
    logger.warning("calibration mismatch", extra={"detail": str(exc)})
    return _error(status.HTTP_409_CONFLICT, "model_mismatch", str(exc))


@app.exception_handler(RegistryError)
async def _registry_error_handler(request: Request, exc: RegistryError) -> JSONResponse:
    logger.error("registry failure", exc_info=exc)
    return _error(status.HTTP_503_SERVICE_UNAVAILABLE, "registry_unavailable", str(exc))


@app.exception_handler(ModelInputError)
async def _model_input_handler(request: Request, exc: ModelInputError) -> JSONResponse:
    return _error(status.HTTP_422_UNPROCESSABLE_CONTENT, "invalid_input", str(exc))


@app.exception_handler(RequestValidationError)
async def _validation_handler(request: Request, exc: RequestValidationError) -> JSONResponse:
    first = exc.errors()[0] if exc.errors() else {}
    location = ".".join(str(part) for part in first.get("loc", ())[1:]) or "body"
    return _error(
        status.HTTP_422_UNPROCESSABLE_CONTENT,
        "validation_error",
        f"{location}: {first.get('msg', 'invalid request body')}",
    )


# ----------------------------------------------------------------------------
# Endpoints
# ----------------------------------------------------------------------------
@app.post(
    "/posthoc_train",
    response_model=PosthocTrainResponse,
    responses={422: {"model": ErrorResponse}, 409: {"model": ErrorResponse}},
    summary="Fit and persist calibration for one scenario",
)
def posthoc_train(
    request: PosthocTrainRequest,
    settings: SettingsDep,
    registry: RegistryDep,
    _: AuthDep,
) -> PosthocTrainResponse:
    return train_scenario(
        request,
        model=model_module.get_model(settings),
        registry=registry,
        settings=settings,
    )


@app.post(
    "/decision",
    response_model=DecisionResponse,
    responses={
        409: {"model": ErrorResponse},
        424: {"model": ErrorResponse},
        422: {"model": ErrorResponse},
    },
    summary="Return one typed, calibrated decision",
)
def decision(
    request: DecisionRequest,
    settings: SettingsDep,
    registry: RegistryDep,
    _: AuthDep,
) -> DecisionResponse:
    response = decide(
        request,
        model=model_module.get_model(settings),
        registry=registry,
        settings=settings,
    )
    logger.info(
        "decision served",
        extra={
            "scenario": response.scenario,
            "decision_type": response.decision_type,
            "calibration_version": response.calibration_version,
            "calibrated": response.calibrated,
            "probability": response.probability,
            "latency_ms": round(response.latency_ms, 2),
        },
    )
    return response


@app.get("/scenarios", response_model=ScenarioListResponse, summary="List trained scenarios")
def scenarios(registry: RegistryDep, _: AuthDep) -> ScenarioListResponse:
    return ScenarioListResponse(scenarios=[ScenarioInfo(**row) for row in registry.list_scenarios()])


@app.get("/health", response_model=HealthResponse, summary="Liveness and readiness")
def health(settings: SettingsDep, registry: RegistryDep) -> HealthResponse:
    loaded = model_module.is_loaded()
    return HealthResponse(
        status="ok" if loaded or not settings.model_eager_load else "degraded",
        service=settings.service_name,
        model_backend=settings.model_backend,
        model_name=settings.model_name,
        model_loaded=loaded,
        registry=registry.description,
        cached_scenarios=registry.cached_entries,
    )


def main() -> None:  # pragma: no cover - entry point for `python -m app.server`
    import uvicorn

    settings = get_settings()
    uvicorn.run(
        "app.server:app",
        host=os.environ.get("HOST", "0.0.0.0"),
        port=int(os.environ.get("PORT", "8000")),
        log_level=settings.log_level.lower(),
    )


if __name__ == "__main__":  # pragma: no cover
    main()
