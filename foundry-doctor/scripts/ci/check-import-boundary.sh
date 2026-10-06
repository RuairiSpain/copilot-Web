#!/usr/bin/env bash
# ADR-005 import-boundary check for the core module.
#   1. go.mod must not require github.com/azure/azure-dev (any case).
#   2. With GOWORK=off the core module must build on its own.
#   3. `go list -deps -test ./...` (GOWORK=off) must not contain any github.com/azure/azure-dev package.
# Exit: 0 pass; 1 violation or build failure; 5 SKIPPED (go toolchain or go.mod missing).
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT" || exit 1

if [ ! -f go.mod ]; then echo "SKIPPED: no go.mod in $ROOT"; exit 5; fi
if ! command -v go >/dev/null 2>&1; then echo "SKIPPED: go toolchain not installed"; exit 5; fi

export GOWORK=off
fail=0

if grep -Eiq 'github\.com/azure/azure-dev' go.mod; then
  echo "FAIL: go.mod requires github.com/azure/azure-dev (ADR-005 decision 3 and 6):"
  grep -Ein 'github\.com/azure/azure-dev' go.mod
  fail=1
else
  echo "ok: go.mod has no github.com/azure/azure-dev requirement"
fi

if go build ./... ; then
  echo "ok: GOWORK=off go build ./..."
else
  echo "FAIL: GOWORK=off go build ./... failed"
  fail=1
fi

deps="$(go list -deps -test ./... 2>&1)"; rc=$?
if [ $rc -ne 0 ]; then
  echo "FAIL: go list -deps -test ./... failed:"; echo "$deps" | tail -20
  fail=1
elif echo "$deps" | grep -Eiq '^github\.com/azure/azure-dev(/|$)'; then
  echo "FAIL: the core module depends on github.com/azure/azure-dev packages:"
  echo "$deps" | grep -Ei '^github\.com/azure/azure-dev(/|$)' | sort -u
  fail=1
else
  echo "ok: go list -deps -test ./... has no github.com/azure/azure-dev package ($(echo "$deps" | wc -l | tr -d ' ') packages checked)"
fi

if [ $fail -ne 0 ]; then echo "RESULT: FAIL"; exit 1; fi
echo "RESULT: PASS"
