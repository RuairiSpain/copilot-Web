#!/usr/bin/env bash
# PostToolUse hook (Edit|Write|MultiEdit). Formats Go files and scans the edited file for secrets.
# Fails closed: if jq is missing or the input is unreadable, exit 2 (the edit is reported as unverified).
if ! command -v jq >/dev/null 2>&1; then echo "post-edit.sh: jq is required (fail closed)" >&2; exit 2; fi
input="$(cat)"
f="$(printf '%s' "$input" | jq -r '.tool_input.file_path // empty' 2>/dev/null)" || { echo "post-edit.sh: unreadable hook input (fail closed)" >&2; exit 2; }
{ [ -z "$f" ] || [ ! -f "$f" ]; } && exit 0
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
f="$(cd "$(dirname "$f")" && pwd)/$(basename "$f")"
rel="${f#"$FD_REPO_ROOT"/}"
in_scope=0
for p in "${FD_SCAN_PATHS[@]}"; do case "$rel" in "$p"|"$p"/*) in_scope=1 ;; esac; done
[ $in_scope -eq 0 ] && exit 0
case "$f" in *.go) command -v gofmt >/dev/null 2>&1 && gofmt -w "$f" ;; esac
case "$rel" in foundry-doctor/scripts/claude/lib.sh|foundry-doctor/scripts/claude/secret-allowlist.txt) exit 0 ;; esac
fd_allowlisted "${rel#foundry-doctor/}" && exit 0
grep -nIiE -e "$FD_SECRET_REGEX" "$f" >&2
case $? in
  0) echo "post-edit.sh: $rel contains secret-shaped content. Remove it, or if it is a fake fixture, allowlist the file in scripts/claude/secret-allowlist.txt." >&2; exit 2 ;;
  1) exit 0 ;;
  *) echo "post-edit.sh: secret scan error on $rel (fail closed)" >&2; exit 2 ;;
esac
