"""FastAPI dependencies: container access and role enforcement."""

from __future__ import annotations

from typing import Annotated

from fastapi import Depends, Request
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer

from hosted_agent_kit.service.container import Container
from hosted_agent_kit.service.security.claims import Principal

# Declares Bearer authentication in the OpenAPI document. It does not enforce anything:
# ``Authenticator`` validates the token and returns problem+json errors itself.
bearer_scheme = HTTPBearer(
    auto_error=False,
    bearerFormat="JWT",
    description="Microsoft Entra ID access token. The user identity is the `oid` claim.",
)
BearerCredentials = Annotated[HTTPAuthorizationCredentials | None, Depends(bearer_scheme)]


async def get_container(request: Request) -> Container:
    container: Container = request.app.state.container
    return container


async def require_invoke(request: Request, _: BearerCredentials = None) -> Principal:
    container = await get_container(request)
    return await container.authenticator.authorise(request, container.settings.invoke_role)


async def require_admin(request: Request, _: BearerCredentials = None) -> Principal:
    container = await get_container(request)
    return await container.authenticator.authorise(request, container.settings.admin_role)


ContainerDep = Annotated[Container, Depends(get_container)]
InvokePrincipal = Annotated[Principal, Depends(require_invoke)]
AdminPrincipal = Annotated[Principal, Depends(require_admin)]
