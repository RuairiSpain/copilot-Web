#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <version>" >&2
  exit 2
fi

version="$1"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dist="${root}/dist/rehearsal"
mkdir -p "$dist"
rm -rf "$dist"/*

# ADR-011 decision 6 (Amendment 2): linux and windows on amd64 and arm64. macOS is not supported.
targets=(
  "linux amd64"
  "linux arm64"
  "windows amd64"
  "windows arm64"
)

# Linux has python3, Git Bash on Windows usually has python.
if command -v python3 >/dev/null 2>&1; then py=python3; else py=python; fi

# pack DIR ARCHIVE: .zip for windows, .tar.gz for linux (azd convention). Entries sit at the archive root.
pack() {
  case "$2" in
    *.tar.gz) tar -C "$1" -czf "$2" . ;;
    *.zip)
      "$py" - "$1" "$2" <<'PY'
import os, sys, zipfile
src, dst = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(dst, "w", zipfile.ZIP_DEFLATED) as zf:
    for name in sorted(os.listdir(src)):
        zf.write(os.path.join(src, name), arcname=name)
PY
      ;;
    *) echo "unknown archive type: $2" >&2; return 2 ;;
  esac
}

build_one() {
  local goos="$1" goarch="$2" ext="" archive_ext="tar.gz"
  if [[ "$goos" == windows ]]; then ext=".exe"; archive_ext="zip"; fi
  local stage="${dist}/stage-${goos}-${goarch}"
  rm -rf "$stage"
  mkdir -p "$stage"

  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    go build -trimpath -ldflags "-s -w -X main.version=${version}" \
    -o "${stage}/foundry-doctor${ext}" ./cmd/foundry-doctor

  pack "$stage" "${dist}/foundry-doctor_${version}_${goos}_${goarch}.${archive_ext}"

  if [[ -f "${root}/extension/go.mod" ]]; then
    rm -rf "$stage"
    mkdir -p "$stage"
    (
      cd "${root}/extension"
      GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
        go build -trimpath -ldflags "-s -w -X main.version=${version}" \
        -o "${stage}/foundry-doctor-azd${ext}" .
    )
    pack "$stage" "${dist}/foundry-doctor-azd-extension_${version}_${goos}_${goarch}.${archive_ext}"
  fi

  rm -rf "$stage"
}

cd "$root"
for t in "${targets[@]}"; do
  build_one ${t}
done

(
  cd "$dist"
  sha256sum -- *.tar.gz *.zip | sed -E 's/^([0-9a-f]{64}) [ *]/\1  /' | sort -k2 > checksums.txt
)

"$py" scripts/release/gen_extension_manifest.py \
  --version "$version" \
  --checksums "$dist/checksums.txt" \
  --base-url "https://github.com/RuairiSpain/copilot-Web/releases/download/foundry-doctor/v${version}" \
  --out-dir "$dist"

if command -v syft >/dev/null 2>&1; then
  syft dir:"$root" -o spdx-json > "$dist/foundry-doctor.spdx.json"
else
  echo "syft not installed; skipping local SBOM generation" >&2
fi

echo "dry-run release assets written to $dist"
