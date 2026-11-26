#!/usr/bin/env bash
# Regenerate the GitHub Copilot copies of the egm-* agents and skills from the
# Claude Code originals in .claude/. Edit the .claude files, then run this.
#   scripts/sync-egm-copilot.sh          write the copies
#   scripts/sync-egm-copilot.sh --check  exit 1 if the copies are stale
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
check=0
[ "${1:-}" = "--check" ] && check=1
stale=0

# Claude tool list -> Copilot tool aliases. Empty result means "all tools".
map_tools() {
  local out=() t
  IFS=',' read -ra parts <<<"$1"
  for t in "${parts[@]}"; do
    t="$(echo "$t" | xargs)"
    case "$t" in
      Read|Grep|Glob) out+=("read" "search") ;;
      Bash) out+=("execute") ;;
      Edit|Write) out+=("edit") ;;
    esac
  done
  printf '%s\n' "${out[@]}" | awk '!seen[$0]++' | paste -sd, - | sed 's/,/", "/g'
}

emit() { # $1 = dest, content on stdin
  local tmp
  tmp="$(mktemp)"
  cat >"$tmp"
  if [ "$check" = 1 ]; then
    cmp -s "$tmp" "$1" || { echo "stale: $1"; stale=1; }
    rm -f "$tmp"
  else
    mkdir -p "$(dirname "$1")"
    mv "$tmp" "$1"
    echo "wrote $1"
  fi
}

for src in .claude/agents/egm-*.md; do
  tools="$(sed -n 's/^tools: *//p' "$src" | head -1)"
  mapped="$(map_tools "$tools")"
  # Agents that need to edit files get no explicit list (all tools).
  case "$tools" in *Edit*|*Write*) mapped="" ;; esac
  awk -v tools="$mapped" '
    /^---$/ { n++ }
    /^tools:/ && n == 1 { if (tools != "") print "tools: [\"" tools "\"]"; next }
    { print }
  ' "$src" | emit ".github/agents/$(basename "$src")"
done

for src in .claude/skills/egm-*/SKILL.md; do
  emit ".github/skills/$(basename "$(dirname "$src")")/SKILL.md" <"$src"
done

[ "$stale" = 0 ] || { echo "run scripts/sync-egm-copilot.sh to refresh"; exit 1; }
