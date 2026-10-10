#!/usr/bin/env bash
# Validates the rule catalogue and checks that the generated docs are current.
# Exits 5 (SKIPPED) until cmd/rulecatalog exists; verify-phase.sh reports 5 as skipped, never as pass.
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$FD_ROOT" || exit 4
if [ ! -d cmd/rulecatalog ]; then
  echo "check-rule-catalog: SKIPPED (cmd/rulecatalog does not exist yet)"
  exit 5
fi
go run ./cmd/rulecatalog validate --phase0 || exit 1
go run ./cmd/rulecatalog generate-docs --check || exit 1
go run ./cmd/rulecatalog generate-overlap --check || exit 1
echo "check-rule-catalog: ok"
