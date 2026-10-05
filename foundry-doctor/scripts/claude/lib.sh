#!/usr/bin/env bash
# Shared helpers for Foundry Doctor harness scripts.
FD_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export FD_ROOT

# Pathspec excludes for the secret scan: one repo-relative glob per line in the allowlist.
fd_secret_excludes() {
  local f="$FD_ROOT/scripts/claude/secret-allowlist.txt"
  [ -f "$f" ] || return 0
  grep -vE '^\s*(#|$)' "$f" | sed 's/^/:(exclude,glob)/'
}

# Regexes for secret-shaped content (extended regex, case-sensitive).
FD_SECRET_REGEX='-----BEGIN [A-Z ]*PRIVATE KEY-----|AccountKey=[A-Za-z0-9+/=]{20,}|SharedAccessSignature=sv=|[?&]sig=[A-Za-z0-9%+/=]{30,}|Bearer eyJ[A-Za-z0-9._-]{20,}|eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}|client_secret["'"'"' :=]+[A-Za-z0-9~._-]{20,}|DefaultEndpointsProtocol=https;AccountName=[^;]+;AccountKey='

# fd_allowlisted <path relative to foundry-doctor/>: success if it matches a secret-allowlist glob.
fd_allowlisted() {
  local rel="$1" g f="$FD_ROOT/scripts/claude/secret-allowlist.txt"
  [ -f "$f" ] || return 1
  while IFS= read -r g; do
    case "$g" in ''|'#'*) continue ;; esac
    # shellcheck disable=SC2254
    case "$rel" in $g) return 0 ;; esac
  done < "$f"
  return 1
}
