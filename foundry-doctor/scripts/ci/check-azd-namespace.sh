#!/usr/bin/env bash
# ADR-003 condition 1: fail if a first-party azd extension or command already uses `foundry`.
# Sources (all read over HTTPS from raw.githubusercontent.com):
#   - Azure/azure-dev cli/azd/extensions/registry.json and registry.dev.json, at the commit ADR-003 was verified against
#     (AZD_PINNED_REF) and at AZD_LIVE_REF (default main) to detect drift;
#   - MicrosoftDocs/azure-dev-docs articles/azure-developer-cli/reference.md (core command reference), live ref.
# A hit is: registry extension `namespace` equal to "foundry" or starting with "foundry."; or a "## azd foundry" heading
# in the command reference. It also fails if this repo's extension manifest (if present) uses id microsoft.foundry.
# The registry id `microsoft.foundry` itself is expected (a pack without commands, ADR-003) and is only reported.
# Exit: 0 pass; 1 collision found; 5 SKIPPED (network unavailable, jq missing, or a source could not be parsed).
# CI must treat 5 as a warning, never as a pass.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

AZD_PINNED_REF="${AZD_PINNED_REF:-afe4b2b4d262bab4c11f4937e7a7942557ab0ecd}"
AZD_LIVE_REF="${AZD_LIVE_REF:-main}"
DOCS_REF="${DOCS_REF:-main}"
RAW="${RAW_BASE:-https://raw.githubusercontent.com}"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

skip() { echo "SKIPPED: $*"; echo "RESULT: SKIPPED (the namespace check did not run; this is not a pass)"; exit 5; }
command -v curl >/dev/null 2>&1 || skip "curl not installed"
command -v jq >/dev/null 2>&1 || skip "jq not installed"

fetch() { # fetch <url> <dest>
  curl -fsSL --retry 2 --connect-timeout 10 --max-time 60 -o "$2" "$1" 2>"$TMP/err" \
    || skip "could not fetch $1 ($(tr '\n' ' ' < "$TMP/err" | cut -c1-160))"
}

fail=0
check_registry() { # check_registry <label> <file>
  local label="$1" file="$2" hits
  jq -e '.extensions | type == "array"' "$file" >/dev/null 2>&1 || skip "$label: unexpected registry format"
  hits="$(jq -r '.extensions[] | select((.namespace // "") | (. == "foundry" or startswith("foundry."))) | "\(.id) namespace=\(.namespace)"' "$file")"
  if [ -n "$hits" ]; then echo "FAIL: $label has a first-party foundry namespace:"; echo "$hits"; fail=1
  else echo "ok: $label has no foundry namespace ($(jq '.extensions | length' "$file") extensions)"; fi
  jq -r '.extensions[] | select(.id == "microsoft.foundry") | "info: \($label) lists id microsoft.foundry (expected, namespace=\(.namespace // "none")); our extension id must differ"' --arg label "$label" "$file" 2>/dev/null || true
}

for ref in "$AZD_PINNED_REF" "$AZD_LIVE_REF"; do
  for f in registry.json registry.dev.json; do
    dest="$TMP/$(echo "$ref-$f" | tr '/' '_')"
    fetch "$RAW/Azure/azure-dev/$ref/cli/azd/extensions/$f" "$dest"
    check_registry "azure-dev@${ref:0:12} $f" "$dest"
  done
done

fetch "$RAW/MicrosoftDocs/azure-dev-docs/$DOCS_REF/articles/azure-developer-cli/reference.md" "$TMP/reference.md"
grep -q '^## azd ' "$TMP/reference.md" || skip "azd command reference has no '## azd' headings (format changed?)"
if grep -Eiq '^#+ azd foundry( |$)' "$TMP/reference.md"; then
  echo "FAIL: the azd command reference documents a 'foundry' command:"; grep -Ein '^#+ azd foundry( |$)' "$TMP/reference.md"; fail=1
else
  echo "ok: azd command reference ($(grep -c '^## azd ' "$TMP/reference.md") commands) has no 'azd foundry'"
fi

# Our own extension manifest must not claim the taken id (ADR-003). Absent until the extension module exists.
manifests="$(find "$ROOT" -name extension.yaml -not -path '*/node_modules/*' -not -path '*/test/*' 2>/dev/null)"
if [ -n "$manifests" ]; then
  while IFS= read -r m; do
    if grep -Eq '^[[:space:]]*id:[[:space:]]*"?microsoft\.foundry"?[[:space:]]*$' "$m"; then
      echo "FAIL: $m uses extension id microsoft.foundry (ADR-003)"; fail=1
    else echo "ok: $m does not use id microsoft.foundry"; fi
  done <<< "$manifests"
else
  echo "note: no extension.yaml in this repository yet; the own-id check did not run"
fi

if [ $fail -ne 0 ]; then echo "RESULT: FAIL"; exit 1; fi
echo "RESULT: PASS"
