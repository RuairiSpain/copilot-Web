#!/usr/bin/env bash
# Stop hook. Blocks ending the turn if secret-shaped content is present in the scanned paths.
# The scan runs even when stop_hook_active is true, so a second stop attempt cannot skip it.
cat >/dev/null
bash "$(dirname "${BASH_SOURCE[0]}")/check-no-secrets.sh" >&2 || exit 2
exit 0
