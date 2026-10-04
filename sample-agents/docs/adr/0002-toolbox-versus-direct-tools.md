# 0002. Toolbox for shared tools, direct tools for single-agent tools

Status: accepted, 2026-10-03

## Context

Two hosted agents need the same AI Search and code interpreter tools. One prompt agent needs one
MCP server and one PDF.

## Decision

* Shared tools go in a toolbox (`search-and-code`). Agents consume its MCP endpoint through
  `FoundryToolbox`. Credentials, versions and policy live in one place.
* A tool with exactly one consumer stays on that agent (`kb-prompt-agent`). Move it into a
  toolbox when a second consumer appears.

## Consequences

* Changing a shared tool needs no agent code change. Use `azd deploy --all` after edits.
* The toolbox is a deploy-time dependency of both hosted agents.
