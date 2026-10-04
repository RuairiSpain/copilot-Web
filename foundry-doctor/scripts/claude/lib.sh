#!/usr/bin/env bash
# Shared helpers for Foundry Doctor harness scripts.
FD_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export FD_ROOT

# Repository root and the paths the secret scan covers (the whole harness and the project, not other projects).
FD_REPO_ROOT="$(git -C "$FD_ROOT" rev-parse --show-toplevel 2>/dev/null || (cd "$FD_ROOT/.." && pwd))"
export FD_REPO_ROOT
FD_SCAN_PATHS=(foundry-doctor .claude .mcp.json .github/workflows/foundry-doctor-ci.yml)

# Pathspec excludes for the secret scan, built from the allowlist (globs relative to foundry-doctor/).
# Blank lines and comments are ignored; entries are trimmed; a bare "**" or "*" would disable the scan and is rejected.
fd_secret_excludes() {
  local f="$FD_ROOT/scripts/claude/secret-allowlist.txt" g
  [ -f "$f" ] || return 0
  while IFS= read -r g; do
    g="${g%$'\r'}"; g="${g#"${g%%[![:space:]]*}"}"; g="${g%"${g##*[![:space:]]}"}"
    case "$g" in ''|'#'*) continue ;; '*'|'**'|'**/*') echo "secret-allowlist.txt: refusing catch-all entry '$g'" >&2; return 1 ;; esac
    echo ":(exclude,glob)foundry-doctor/$g"
  done < "$f"
}

# Secret-shaped content (extended regex; callers pass -i). Deliberately broad: the allowlist handles fake fixtures.
FD_SECRET_REGEX='-----BEGIN [A-Z ]*PRIVATE KEY-----'
FD_SECRET_REGEX+='|(AccountKey|SharedAccessKey|SharedAccessSignature|password|passwd|pwd|secret|api[-_]?key|access[-_]?token|client[-_]?secret)["'"'"' ]*[:=]["'"'"' ]*[A-Za-z0-9+/=~._-]{12,}'
FD_SECRET_REGEX+='|[?&]sig=[A-Za-z0-9%+/=]{30,}'
FD_SECRET_REGEX+='|Bearer [A-Za-z0-9._~+/-]{20,}=*'
FD_SECRET_REGEX+='|eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}'
FD_SECRET_REGEX+='|gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{40,}|AKIA[0-9A-Z]{16}|xox[baprs]-[A-Za-z0-9-]{10,}|sk-[A-Za-z0-9]{20,}'
FD_SECRET_REGEX+='|DefaultEndpointsProtocol=https;AccountName=[^;]+;AccountKey='
export FD_SECRET_REGEX

# fd_allowlisted <path relative to foundry-doctor/>: success if it matches a secret-allowlist glob.
fd_allowlisted() {
  local rel="$1" g f="$FD_ROOT/scripts/claude/secret-allowlist.txt"
  [ -f "$f" ] || return 1
  while IFS= read -r g; do
    g="${g%$'\r'}"; g="${g#"${g%%[![:space:]]*}"}"; g="${g%"${g##*[![:space:]]}"}"
    case "$g" in ''|'#'*|'*'|'**'|'**/*') continue ;; esac
    # shellcheck disable=SC2254
    case "$rel" in $g) return 0 ;; esac
  done < "$f"
  return 1
}
