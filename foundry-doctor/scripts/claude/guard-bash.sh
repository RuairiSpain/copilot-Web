#!/usr/bin/env bash
# PreToolUse hook (Bash). Blocks commands that mutate Azure or rewrite shared git history,
# including inside compound commands that permission prefix rules would miss.
# Reads the hook JSON on stdin. Exit 2 blocks the call and shows stderr to Claude.
cmd="$(jq -r '.tool_input.command // empty' 2>/dev/null)"
[ -z "$cmd" ] && exit 0
block() { echo "Blocked by guard-bash.sh: $1. Foundry Doctor is read-only; ask the user if this is really intended." >&2; exit 2; }
echo "$cmd" | grep -Eq '(^|[;&|[:space:]])azd[[:space:]]+(down|up|provision|deploy)([[:space:]]|$)' && block "azd down/up/provision/deploy mutates Azure"
echo "$cmd" | grep -Eq '(^|[;&|[:space:]])az[[:space:]]+.*[[:space:]](delete|purge|create|update|deployment[[:space:]]+[a-z]+[[:space:]]+create)([[:space:]]|$)' && block "az write/delete command"
echo "$cmd" | grep -Eq 'git[[:space:]]+push[^;&|]*(--force|--force-with-lease|[[:space:]]-f([[:space:]]|$))' && block "force push"
echo "$cmd" | grep -Eq 'git[[:space:]]+(reset[[:space:]]+--hard|clean[[:space:]]+-[a-z]*f)' && block "destructive git command"
echo "$cmd" | grep -Eq '(cat|less|head|tail|cp|curl[^|]*-d[[:space:]]*@)[^;&|]*(\.env($|[[:space:]])|\.pem|\.key|id_rsa)' && block "reading or sending credential files"
exit 0
