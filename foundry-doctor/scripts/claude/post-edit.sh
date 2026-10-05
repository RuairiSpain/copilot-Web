#!/usr/bin/env bash
# PostToolUse hook (Edit|Write|MultiEdit). Formats Go files and scans the edited file for secrets.
f="$(jq -r '.tool_input.file_path // empty' 2>/dev/null)"
{ [ -z "$f" ] || [ ! -f "$f" ]; } && exit 0
case "$f" in */foundry-doctor/*) ;; *) exit 0 ;; esac
case "$f" in *.go) command -v gofmt >/dev/null 2>&1 && gofmt -w "$f" ;; esac
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
rel="${f#*foundry-doctor/}"
case "$rel" in docs/requirements/*|scripts/claude/lib.sh|scripts/claude/secret-allowlist.txt) exit 0 ;; esac
fd_allowlisted "$rel" && exit 0
grep -nIE -e "$FD_SECRET_REGEX" "$f" >&2
case $? in
  0) echo "post-edit.sh: $rel contains secret-shaped content. Remove it, or if it is a fake fixture, allowlist the file in scripts/claude/secret-allowlist.txt." >&2; exit 2 ;;
  1) exit 0 ;;
  *) echo "post-edit.sh: secret scan error on $rel" >&2; exit 2 ;;
esac
