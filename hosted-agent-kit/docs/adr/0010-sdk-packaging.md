# ADR 0010: Ship the scheduler as an importable SDK

## Status
Accepted.

## Context
The scheduler began as a standalone HTTP service. Teams that already write a Python FastAPI
application must run, secure and call a second service to use it. Many of them want to write
their own endpoints and call the scheduler in process.

## Decision
- The package `hosted-agent-kit` is a library. The unit of use is `Hack`, a facade over the runtime
  that the service also uses (`runtime.py`: `Runtime`, `build_runtime`).
- Calls, reporting and administration are plain async methods that return typed values. Web
  concerns (lifecycle, errors, streaming, routers) are in `integrations/fastapi.py`, which needs
  the `fastapi` extra. The core install has no web framework.
- The standalone service stays in `hosted_agent_kit.service` and is an optional extra. It composes
  a `Runtime` and adds authentication, request limits and HTTP routes. Its reporting and admin
  routes call the same `Reporting` and `Administration` classes as the SDK.
- Settings split in two: `KitSettings` (runtime) and `Settings(KitSettings)` (service only). The
  non-secret runtime settings can live in a `hack:` section of the scheduler YAML.
- The SDK does not configure logging or install signal handlers. The host application owns both.
- `testing.py` is public: fakes are part of the contract so users can test their endpoints.

## Consequences
- One implementation of scheduling for both forms. A fix lands once.
- The SDK inherits the single-process limit (ADR 0003). The documentation says so.
- The public surface is `hosted_agent_kit`, `.errors`, `.plugins`, `.testing`,
  `.integrations.fastapi`, `.views`. Other modules are internal and may change.
- Version 0.x signals that the surface can still change.
