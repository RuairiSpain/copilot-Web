# ADR-005: azd extension host lives in a separate Go module

Status: Proposed
Date: 2026-10-04

## Context

PRD section 9 requires that core logic stays library-independent so the azd extension and the standalone CLI share it.
ADR-003 defines an `AzdContext` abstraction and says core packages must not import `azdext`.

`docs/licence-inventory.md` found, from the Go module proxy on 2026-10-04:

- The azd module that contains `azdext` (v1.35.0) requires Go 1.26.4 and brings about 150 direct requirements.
- The latest `azidentity`, `azcore` and `armresourcegraph` need Go 1.25.0 and `golang.org/x/sync` v0.23.0 needs Go 1.26.0.
- Go-1.24-compatible pins exist: `azidentity` v1.13.1, `azcore` v1.20.0, `x/sync` v0.19.0, `armresourcegraph` v0.9.0.
- This repository's `foundry-doctor/go.mod` declares `go 1.24`; the toolchain in the development sandbox is go1.24.7.

One module that imports `azdext` would force the whole core, and the standalone binary, onto Go 1.26.4 and the azd dependency tree.

## Decision

1. `foundry-doctor/` is the **core module**: rule engine, catalogue, reporters, standalone CLI. It never imports `azdext`.
   Its `go` directive is chosen deliberately in Phase 1 (see open item below), not inherited from a dependency.
2. The azd extension host is a **second module** (`foundry-doctor/extension/`, own `go.mod`) that imports the core module and
   `azdext`, implements `AzdContext`, and registers the Cobra commands under the namespace chosen in ADR-003.
3. A CI check fails if any package in the core module imports `github.com/azure/azure-dev` (use `go list -deps`).
4. The two modules are tied together with a `go.work` file for development and with a tagged core version for releases.

## Consequences

- Standalone users get a binary without the azd dependency tree.
- Two modules to version and release; the release workflow builds both.
- Extension builds need Go 1.26+ in CI, while the core can use an older toolchain.

## Open items

- Choose the core `go` directive in Phase 1 after checking that the Azure SDK modules the core needs resolve at that version
  (the 1.24-compatible pins above are the fallback).
- The binary-size cost of importing the azd module is unmeasured.
- Whether a standalone binary can ever use `azdext` is answered in ADR-003: it cannot run without azd, which sets
  `AZD_SERVER` and `AZD_ACCESS_TOKEN` only when it launches an extension.
