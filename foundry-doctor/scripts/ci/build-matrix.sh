#!/usr/bin/env bash
# Cross-platform build of the standalone binary (ADR-011 decision 6: linux, darwin, windows x amd64, arm64).
# CGO_ENABLED=0, -trimpath, stripped (-s -w), version injected with -ldflags -X, SHA-256 checksums file.
#
# Usage: build-matrix.sh [--out DIR] [--version VERSION] [--targets "os/arch ..."]
# Environment overrides:
#   BUILD_PKG      main package to build (default ./cmd/foundry-doctor)
#   BINARY_NAME    output name prefix (default foundry-doctor)
#   VERSION_VAR    fully qualified variable set by -X (default <module>/internal/buildinfo.Version).
#                  Go ignores -X for a variable that does not exist, so the packet that owns the version
#                  variable must keep this default in sync (see docs/development/ci.md).
# Output: DIR/<name>_<version>_<os>_<arch>[.exe] and DIR/SHA256SUMS.
# Exit: 0 built; 1 build failure; 2 bad arguments; 5 SKIPPED (go missing, or BUILD_PKG directory absent).
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT" || exit 1

OUT="$ROOT/dist"; VERSION=""; TARGETS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT="${2:?--out needs a directory}"; shift 2 ;;
    --version) VERSION="${2:?--version needs a value}"; shift 2 ;;
    --targets) TARGETS="${2:?--targets needs a list}"; shift 2 ;;
    -h|--help) sed -n 2,14p "${BASH_SOURCE[0]}"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

BUILD_PKG="${BUILD_PKG:-./cmd/foundry-doctor}"
BINARY_NAME="${BINARY_NAME:-foundry-doctor}"
if ! command -v go >/dev/null 2>&1; then echo "SKIPPED: go toolchain not installed"; exit 5; fi
if [ ! -f go.mod ]; then echo "SKIPPED: no go.mod"; exit 5; fi
if [ ! -d "$BUILD_PKG" ]; then echo "SKIPPED: $BUILD_PKG does not exist yet, nothing to build"; exit 5; fi

MODULE="$(go list -m 2>/dev/null)" || { echo "cannot read module path" >&2; exit 1; }
VERSION_VAR="${VERSION_VAR:-$MODULE/internal/buildinfo.Version}"
if [ -z "$VERSION" ]; then VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"; fi
VERSION="${VERSION#foundry-doctor/}"
case "$VERSION" in ''|*[!A-Za-z0-9._+-]*) echo "invalid version '$VERSION' (allowed: A-Z a-z 0-9 . _ + -)" >&2; exit 2 ;; esac

mkdir -p "$OUT" || exit 1
OUT="$(cd "$OUT" && pwd)"
rm -f "$OUT/SHA256SUMS"
export CGO_ENABLED=0 GOWORK=off

built=()
for t in $TARGETS; do
  goos="${t%/*}"; goarch="${t#*/}"
  ext=""; [ "$goos" = windows ] && ext=".exe"
  file="${BINARY_NAME}_${VERSION}_${goos}_${goarch}${ext}"
  echo "build $goos/$goarch -> $file"
  if ! GOOS="$goos" GOARCH="$goarch" go build -trimpath -buildvcs=false \
        -ldflags "-s -w -X ${VERSION_VAR}=${VERSION}" -o "$OUT/$file" "$BUILD_PKG"; then
    echo "FAIL: build for $goos/$goarch failed" >&2; exit 1
  fi
  built+=("$file")
done

sha256_of() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }
( cd "$OUT" && sha256_of "${built[@]}" > SHA256SUMS ) || { echo "FAIL: could not write checksums" >&2; exit 1; }
echo "wrote $OUT/SHA256SUMS:"; cat "$OUT/SHA256SUMS"
echo "RESULT: PASS (${#built[@]} binaries, version $VERSION)"
