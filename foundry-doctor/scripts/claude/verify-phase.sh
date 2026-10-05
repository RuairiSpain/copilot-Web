#!/usr/bin/env bash
# Runs every acceptance gate and prints a table. A gate that cannot run is SKIPPED, never PASS.
# Exit: 0 all runnable gates pass; 1 a gate failed; 3 something was skipped and --strict was given.
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
STRICT=0; PHASE0=0
for arg in "$@"; do
  case "$arg" in
    --strict) STRICT=1 ;;
    --phase0) PHASE0=1 ;;
    *) echo "usage: verify-phase.sh [--strict] [--phase0]" >&2; exit 2 ;;
  esac
done
cd "$FD_ROOT" || exit 4
export PATH="$PATH:$(go env GOPATH 2>/dev/null)/bin"

results=(); failed=0; skipped=0
record() { results+=("$(printf '%-9s %-34s %s' "$1" "$2" "$3")"); }
gate() { # gate <name> <needs-go-module 0|1> <command...>
  local name="$1" needs="$2"; shift 2
  if [ "$needs" = 1 ] && [ ! -f go.mod ]; then record SKIPPED "$name" "no go.mod yet"; skipped=$((skipped+1)); return; fi
  if [ "$name" = "govulncheck ./..." ]; then command -v govulncheck >/dev/null 2>&1 || { record SKIPPED "$name" "govulncheck not installed (scripts/install-dev-tools.sh)"; skipped=$((skipped+1)); return; }
  elif ! command -v "$1" >/dev/null 2>&1; then record SKIPPED "$name" "$1 not installed (scripts/install-dev-tools.sh)"; skipped=$((skipped+1)); return; fi
  local out; out="$("$@" 2>&1)"; local rc=$?
  if [ $rc -eq 0 ]; then record PASS "$name" ""
  elif [ $rc -eq 5 ]; then record SKIPPED "$name" "$(echo "$out" | tail -1)"; skipped=$((skipped+1))
  else record FAIL "$name" "exit $rc"; failed=$((failed+1)); echo "--- $name ---"; echo "$out" | tail -40; fi
}

gofmt_check() { local o; o="$(gofmt -l . 2>&1)"; [ -z "$o" ] || { echo "unformatted:"; echo "$o"; return 1; }; }
gate "gofmt -l"                1 gofmt_check
gate "git diff --check HEAD"    0 git diff --check HEAD
gate "go vet ./..."            1 go vet ./...
gate "go test ./..."           1 go test ./...
race_platform="$(go env GOOS GOARCH 2>/dev/null | paste -sd / -)"
case "$race_platform" in
  linux/amd64|linux/ppc64le|linux/arm64|linux/s390x|darwin/amd64|darwin/arm64|freebsd/amd64|windows/amd64)
    gate "go test -race ./..." 1 go test -race ./...
    ;;
  *)
    record SKIPPED "go test -race ./..." "race detector unsupported on ${race_platform:-unknown platform}"
    skipped=$((skipped+1))
    ;;
esac
gate "staticcheck ./..."       1 staticcheck ./...
govulncheck_gate() { # exit 5 (skipped) when the vulnerability database is unreachable; real findings still fail
  local o rc; o="$(govulncheck ./... 2>&1)"; rc=$?
  if [ $rc -ne 0 ] && echo "$o" | grep -q 'fetching vulnerabilities'; then echo "vulnerability database unreachable (vuln.go.dev)"; return 5; fi
  echo "$o"; return $rc
}
gate "govulncheck ./..."       1 govulncheck_gate
if [ -d cmd/foundry-doctor ]; then gate "go build ./cmd/foundry-doctor" 1 go build -o /dev/null ./cmd/foundry-doctor
elif [ "$PHASE0" = 1 ]; then record EXEMPT "go build ./cmd/foundry-doctor" "phase 0: product command not created yet"
else record SKIPPED "go build ./cmd/foundry-doctor" "cmd/foundry-doctor not created yet"; skipped=$((skipped+1)); fi
gate "check-rule-catalog.sh"   0 bash scripts/claude/check-rule-catalog.sh
gate "check-no-secrets.sh"     0 bash scripts/claude/check-no-secrets.sh
harness_tests() { python3 -m unittest discover -s scripts/claude 2>&1; }
gate "harness unit tests"      0 harness_tests

echo; echo "RESULT    GATE                               DETAIL"; printf '%s\n' "${results[@]}"; echo
echo "failed=$failed skipped=$skipped"
[ $failed -gt 0 ] && exit 1
[ $STRICT = 1 ] && [ $skipped -gt 0 ] && exit 3
exit 0
