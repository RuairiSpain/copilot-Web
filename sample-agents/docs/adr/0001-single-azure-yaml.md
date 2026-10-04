# 0001. One azure.yaml, no per-agent definition files

Status: accepted, 2026-10-03

## Context

Earlier Foundry tooling used `agent.manifest.yaml` and `agent.yaml` next to `azure.yaml`.
Microsoft deprecated both. All hosted-agent configuration now lives in `azure.yaml`.

## Decision

Declare the project, model, connections, toolbox and all four agents in one `azure.yaml`.
Validate it in CI against the official extension schemas.

## Consequences

* One file shows the whole deployment and its dependency graph.
* The file is long. Comments above every service are mandatory and tested.
* Large definitions can move to `$ref` files later without changing semantics.
