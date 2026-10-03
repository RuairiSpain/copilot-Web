"""The AgentPool resource: the desired state of one agent's pool and what is observed of it."""

from __future__ import annotations

from pydantic import BaseModel

from hosted_agent_kit.config.models import AgentConfig
from hosted_agent_kit.domain.resources import Condition


class AgentPoolStatus(BaseModel):
    # The generation of the spec the controllers have applied. Behind ``generation`` means a
    # configuration change that has not taken effect yet.
    observed_generation: int = 0
    ready_sessions: int = 0
    leased_sessions: int = 0
    provisioning_sessions: int = 0
    deleting_sessions: int = 0
    reserved_slots: int = 0
    queued_requests: int = 0
    conditions: list[Condition] = []


class AgentPoolResource(BaseModel):
    agent_name: str
    generation: int  # changes when the spec changes
    resource_version: int  # changes on every stored update, including status
    spec: AgentConfig
    status: AgentPoolStatus
