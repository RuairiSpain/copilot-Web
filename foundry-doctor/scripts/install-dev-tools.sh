#!/usr/bin/env bash
# Installs the Go tooling used by the acceptance gates. Needs network access to proxy.golang.org.
set -euo pipefail
go install golang.org/x/tools/gopls@latest
go install honnef.co/go/tools/cmd/staticcheck@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
echo "Installed to $(go env GOPATH)/bin. Add it to PATH."
command -v bicep >/dev/null || echo "Note: Bicep CLI not found; needed for Bicep-backed projects (Phase 0 spikes)."
