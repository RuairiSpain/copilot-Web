#!/usr/bin/env bash
# Fails (exit 2) if secret-shaped content appears in the foundry-doctor project, the Claude harness (.claude, .mcp.json)
# or this project's workflow, in tracked or untracked files. Exit 4 if the scan itself errors: it never fails open.
# Intentional fake secrets in fixtures must be listed in secret-allowlist.txt (globs relative to foundry-doctor/).
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$FD_REPO_ROOT" || exit 4
excludes=()
if ! mapfile -t excludes < <(fd_secret_excludes); then echo "check-no-secrets: bad allowlist" >&2; exit 4; fi
# mapfile cannot see the exit status of the process substitution, so validate the allowlist directly as well.
fd_secret_excludes >/dev/null || { echo "check-no-secrets: bad allowlist" >&2; exit 4; }
git grep --untracked -nIiE -e "$FD_SECRET_REGEX" -- "${FD_SCAN_PATHS[@]}" \
  ':(exclude,glob)foundry-doctor/scripts/claude/lib.sh' ':(exclude,glob)foundry-doctor/scripts/claude/secret-allowlist.txt' \
  ${excludes[@]+"${excludes[@]}"}
rc=$?
case $rc in
  0) echo "check-no-secrets: potential secret found (above). Remove it, or if it is a fake fixture value, list the file in scripts/claude/secret-allowlist.txt." >&2; exit 2 ;;
  1) echo "check-no-secrets: ok" ;;
  *) echo "check-no-secrets: scan error (git grep exit $rc)" >&2; exit 4 ;;
esac
