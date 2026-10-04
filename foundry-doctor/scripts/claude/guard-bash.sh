#!/usr/bin/env bash
# PreToolUse hook (Bash). Thin wrapper: the logic is in guard_bash.py. Fails closed if python3 is missing.
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if ! command -v python3 >/dev/null 2>&1; then
  echo "Blocked by guard-bash.sh: python3 is required to inspect commands and is not installed (fail closed)." >&2
  exit 2
fi
exec python3 "$here/guard_bash.py"
