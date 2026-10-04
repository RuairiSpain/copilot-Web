#!/usr/bin/env bash
# Stop hook. Blocks ending the turn if foundry-doctor/ contains secret-shaped content.
in="$(cat)"
[ "$(echo "$in" | jq -r '.stop_hook_active // false' 2>/dev/null)" = "true" ] && exit 0
bash "$(dirname "${BASH_SOURCE[0]}")/check-no-secrets.sh" >&2 || exit 2
exit 0
