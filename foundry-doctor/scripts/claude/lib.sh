#!/usr/bin/env bash
# Shared helpers for Foundry Doctor harness scripts.
FD_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export FD_ROOT

FD_REPO_ROOT="$(git -C "$FD_ROOT" rev-parse --show-toplevel 2>/dev/null || (cd "$FD_ROOT/.." && pwd))"
export FD_REPO_ROOT
FD_SCAN_PATHS=(foundry-doctor .claude .mcp.json \
  .github/workflows/foundry-doctor-ci.yml \
  .github/workflows/foundry-doctor-dependency-review.yml)

# Emit git pathspec excludes only for fake-secret fixtures. Source, scripts,
# workflows and whole test trees can never be hidden by this allowlist.
fd_secret_excludes() {
  local f="$FD_ROOT/scripts/claude/secret-allowlist.txt" g
  [ -f "$f" ] || return 0
  while IFS= read -r g; do
    g="${g%$'\r'}"; g="${g#"${g%%[![:space:]]*}"}"; g="${g%"${g##*[![:space:]]}"}"
    case "$g" in
      ''|'#'*) continue ;;
      /*|*'..'*|*\\*) echo "secret-allowlist.txt: unsafe entry '$g'" >&2; return 1 ;;
      test/fixtures/*|*/testdata/*) ;;
      *) echo "secret-allowlist.txt: only test/fixtures/** or */testdata/** may be allowlisted: '$g'" >&2; return 1 ;;
    esac
    case "$g" in
      *'*'*|*'?'*|*'['*) echo "secret-allowlist.txt: globs are not permitted; name one fixture file: '$g'" >&2; return 1 ;;
    esac
    [ -f "$FD_ROOT/$g" ] || { echo "secret-allowlist.txt: fixture does not exist: '$g'" >&2; return 1; }
    echo ":(exclude,glob)foundry-doctor/$g"
  done < "$f"
}

# High-signal credential formats and credential assignments. Callers use grep -iE.
# Assignment values begin with the credential alphabet, so documented values
# such as <redacted>, <secret>, and other angle-bracket placeholders do not match.
FD_SECRET_REGEX='-----BEGIN [A-Z ]*PRIVATE KEY-----'
FD_SECRET_REGEX+='|(AccountKey|SharedAccessKey|SharedAccessSignature|password|passwd|pwd|secret|api[-_]?key|access[-_]?token|client[-_]?secret)["'"'"' ]*[:=]["'"'"' ]*[A-Za-z0-9+/=~._-]{12,}'
FD_SECRET_REGEX+='|[?&]sig=[A-Za-z0-9%+/=]{30,}'
FD_SECRET_REGEX+='|eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}'
FD_SECRET_REGEX+='|gh[pousr]_[A-Za-z0-9]{36,255}|github_pat_[A-Za-z0-9_]{40,255}|AKIA[0-9A-Z]{16}|ASIA[0-9A-Z]{16}|xox[baprs]-[A-Za-z0-9-]{10,}|sk-(live|proj)-[A-Za-z0-9_-]{20,}'
FD_SECRET_REGEX+='|DefaultEndpointsProtocol=https;AccountName=[^;]+;AccountKey='
export FD_SECRET_REGEX

fd_allowlisted() {
  local rel="$1" g f="$FD_ROOT/scripts/claude/secret-allowlist.txt"
  [ -f "$f" ] || return 1
  while IFS= read -r g; do
    g="${g%$'\r'}"; g="${g#"${g%%[![:space:]]*}"}"; g="${g%"${g##*[![:space:]]}"}"
    case "$g" in
      ''|'#'*) continue ;;
      test/fixtures/*|*/testdata/*) ;;
      *) continue ;;
    esac
    case "$g" in *'*'*|*'?'*|*'['*) continue ;; esac
    [ -f "$FD_ROOT/$g" ] || continue
    # shellcheck disable=SC2254
    case "$rel" in $g) return 0 ;; esac
  done < "$f"
  return 1
}
