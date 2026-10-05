#!/usr/bin/env bash
# Smoke test: run a built foundry-doctor binary against every directory in samples/ (except samples/github).
# Usage: smoke-samples.sh --binary PATH [--out DIR]
# For each sample the binary runs inside the sample directory: `doctor --format sarif --out <DIR>/<sample>.sarif`.
# Accepted exit codes are 0 and 1 (PRD section 7: findings are a valid outcome). A sample can pin one exact code in
# `<sample>/smoke.expected-exit`. Exit 2, 3 and 4 always fail. The SARIF file must exist and be JSON with version 2.1.0.
# Exit: 0 all samples passed; 1 a sample failed; 5 SKIPPED (no binary, or no sample directories). 5 is never a pass.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN=""; OUT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --binary) BIN="${2:?--binary needs a path}"; shift 2 ;;
    --out) OUT="${2:?--out needs a directory}"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
if [ -z "$BIN" ] || [ ! -x "$BIN" ]; then echo "SKIPPED: no executable binary (--binary '${BIN}'); cmd/foundry-doctor is not built yet"; exit 5; fi
BIN="$(cd "$(dirname "$BIN")" && pwd)/$(basename "$BIN")"
command -v jq >/dev/null 2>&1 || { echo "SKIPPED: jq not installed, cannot validate SARIF"; exit 5; }

samples=()
if [ -d "$ROOT/samples" ]; then
  for d in "$ROOT"/samples/*/; do
    d="${d%/}"; [ -d "$d" ] || continue
    [ "$(basename "$d")" = github ] && continue   # copy-paste workflow samples, not doctor projects
    samples+=("$d")
  done
fi
if [ ${#samples[@]} -eq 0 ]; then echo "SKIPPED: no sample projects under samples/ yet"; exit 5; fi

[ -n "$OUT" ] || OUT="$(mktemp -d)"
mkdir -p "$OUT" && OUT="$(cd "$OUT" && pwd)"
failed=0
for s in "${samples[@]}"; do
  name="$(basename "$s")"; sarif="$OUT/$name.sarif"
  ( cd "$s" && "$BIN" doctor --format sarif --out "$sarif" >"$OUT/$name.log" 2>&1 ); rc=$?
  want=""; [ -f "$s/smoke.expected-exit" ] && want="$(tr -d '[:space:]' < "$s/smoke.expected-exit")"
  ok=0
  if [ -n "$want" ]; then [ "$rc" = "$want" ] && ok=1; else { [ $rc -eq 0 ] || [ $rc -eq 1 ]; } && ok=1; fi
  if [ $ok -ne 1 ]; then echo "FAIL  $name: exit $rc (expected ${want:-0 or 1})"; tail -20 "$OUT/$name.log"; failed=$((failed+1)); continue; fi
  if ! jq -e '.version == "2.1.0" and (.runs | type == "array")' "$sarif" >/dev/null 2>&1; then
    echo "FAIL  $name: $sarif is missing or not SARIF 2.1.0"; failed=$((failed+1)); continue
  fi
  echo "PASS  $name: exit $rc, $(jq '[.runs[].results | length] | add // 0' "$sarif") results"
done
if [ $failed -gt 0 ]; then echo "RESULT: FAIL ($failed of ${#samples[@]} samples)"; exit 1; fi
echo "RESULT: PASS (${#samples[@]} samples)"
