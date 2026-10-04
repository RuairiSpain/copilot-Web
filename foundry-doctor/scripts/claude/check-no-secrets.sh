#!/usr/bin/env bash
# Fails (exit 2) if secret-shaped content appears in foundry-doctor/ tracked or untracked files.
# Exit 4 if the scan itself errors: the scanner must never fail open.
# Intentional fake secrets in fixtures must be listed in secret-allowlist.txt.
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$FD_ROOT" || exit 4
mapfile -t excludes < <(fd_secret_excludes)
git grep --untracked -nIE -e "$FD_SECRET_REGEX" -- . \
  ':(exclude,glob)docs/requirements/**' ':(exclude,glob)scripts/claude/lib.sh' \
  ':(exclude,glob)scripts/claude/secret-allowlist.txt' "${excludes[@]}"
rc=$?
case $rc in
  0) echo "check-no-secrets: potential secret found (above). Remove it, or if it is a fake fixture value, list the file in scripts/claude/secret-allowlist.txt." >&2; exit 2 ;;
  1) echo "check-no-secrets: ok" ;;
  *) echo "check-no-secrets: scan error (git grep exit $rc)" >&2; exit 4 ;;
esac
