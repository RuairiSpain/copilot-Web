#!/usr/bin/env bash
# Installs the Go tooling used by the acceptance gates, at pinned versions (tested 2026-10-04).
# gopls is pinned to v0.21.1: v0.22.0 and later declare go 1.26.0, which the Go 1.25 toolchain in CI cannot build.
# Needs network access to proxy.golang.org. Bump the versions deliberately, not with @latest.
set -euo pipefail
go install golang.org/x/tools/gopls@v0.21.1
go install honnef.co/go/tools/cmd/staticcheck@v0.6.1
go install golang.org/x/vuln/cmd/govulncheck@v1.7.0
echo "Installed to $(go env GOPATH)/bin. Add it to PATH."
command -v bicep >/dev/null || echo "Note: Bicep CLI not found; needed for Bicep-backed projects (Phase 0 spikes)."
