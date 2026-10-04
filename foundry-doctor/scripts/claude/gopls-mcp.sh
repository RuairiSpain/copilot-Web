#!/usr/bin/env bash
# Starts the gopls MCP server in the foundry-doctor module.
cd "$(dirname "${BASH_SOURCE[0]}")/../.." || exit 1
export PATH="$PATH:$(go env GOPATH 2>/dev/null)/bin"
command -v gopls >/dev/null 2>&1 || { echo "gopls not installed; run foundry-doctor/scripts/install-dev-tools.sh" >&2; exit 1; }
exec gopls mcp
