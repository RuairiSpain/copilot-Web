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

targets=(
  "linux amd64"
  "linux arm64"
  "darwin amd64"
  "darwin arm64"
  "windows amd64"
  "windows arm64"
)

build_one() {
  local goos="$1" goarch="$2" ext=""
  [[ "$goos" == windows ]] && ext=".exe"
  local stage="${dist}/stage-${goos}-${goarch}"
  rm -rf "$stage"
  mkdir -p "$stage"

  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    go build -trimpath -ldflags "-s -w -X main.version=${version}" \
    -o "${stage}/foundry-doctor${ext}" ./cmd/foundry-doctor

  if [[ "$goos" == windows ]]; then
    python - "$stage" "${dist}/foundry-doctor_${version}_${goos}_${goarch}.zip" <<'PY'
import os, sys, zipfile
src, dst = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(dst, "w", zipfile.ZIP_DEFLATED) as zf:
    for name in sorted(os.listdir(src)):
        zf.write(os.path.join(src, name), arcname=name)
PY
  else
    tar -C "$stage" -czf "${dist}/foundry-doctor_${version}_${goos}_${goarch}.tar.gz" .
  fi

  if [[ -f "${root}/extension/go.mod" ]]; then
    rm -rf "$stage"
    mkdir -p "$stage"
    (
      cd "${root}/extension"
      GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
        go build -trimpath -ldflags "-s -w -X main.version=${version}" \
        -o "${stage}/foundry-doctor-azd${ext}" .
    )
    if [[ "$goos" == windows ]]; then
      python - "$stage" "${dist}/foundry-doctor-azd-extension_${version}_${goos}_${goarch}.zip" <<'PY'
import os, sys, zipfile
src, dst = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(dst, "w", zipfile.ZIP_DEFLATED) as zf:
    for name in sorted(os.listdir(src)):
        zf.write(os.path.join(src, name), arcname=name)
PY
    else
      tar -C "$stage" -czf "${dist}/foundry-doctor-azd-extension_${version}_${goos}_${goarch}.tar.gz" .
    fi
  fi

  rm -rf "$stage"
}

cd "$root"
for t in "${targets[@]}"; do
  build_one ${t}
done

(
  cd "$dist"
  sha256sum -- *.tar.gz *.zip | sort -k2 > checksums.txt
)

python scripts/release/gen_extension_manifest.py \
  --version "$version" \
  --checksums "$dist/checksums.txt" \
  --base-url "https://github.com/ruairispain/copilot-web/releases/download/foundry-doctor-v${version}" \
  --out-dir "$dist"

if command -v syft >/dev/null 2>&1; then
  syft dir:"$root" -o spdx-json > "$dist/foundry-doctor.spdx.json"
else
  echo "syft not installed; skipping local SBOM generation" >&2
fi

echo "dry-run release assets written to $dist"
