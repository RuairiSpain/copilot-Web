#!/usr/bin/env bash
# Starts the gopls MCP server in the foundry-doctor module.
# Uses the gopls from PATH, else the one that install-dev-tools.sh installed in GOPATH/bin. It does not change PATH.
cd "$(dirname "${BASH_SOURCE[0]}")/../.." || exit 1
gopls_bin="$(command -v gopls || true)"
if [ -z "$gopls_bin" ] && command -v go >/dev/null 2>&1 && [ -x "$(go env GOPATH)/bin/gopls" ]; then
  gopls_bin="$(go env GOPATH)/bin/gopls"
fi
[ -n "$gopls_bin" ] || { echo "gopls not installed; run foundry-doctor/scripts/install-dev-tools.sh" >&2; exit 1; }
exec "$gopls_bin" mcp
