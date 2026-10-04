#!/usr/bin/env bash
# Validates the rule catalogue and checks the generated doc is current.
# Exits 5 (SKIPPED) until cmd/rulecatalog exists; verify-phase.sh reports 5 as skipped, never as pass.
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$FD_ROOT" || exit 4
if [ ! -d cmd/rulecatalog ]; then
  echo "check-rule-catalog: SKIPPED (cmd/rulecatalog does not exist yet; created in Phase 0)"
  exit 5
fi
go run ./cmd/rulecatalog validate || exit 1
go run ./cmd/rulecatalog generate-docs || exit 1
git diff --exit-code -- docs/rule-catalog.md || { echo "check-rule-catalog: docs/rule-catalog.md is stale; commit the regenerated file" >&2; exit 1; }
echo "check-rule-catalog: ok"
