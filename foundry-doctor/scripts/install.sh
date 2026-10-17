#!/usr/bin/env bash
# Installs the Linux binary published by foundry-doctor-release.yml and checks it against SHA256SUMS from the same
# release. Layout: foundry-doctor_<version>_linux_<arch> (bare binary, no extension), SHA256SUMS, tag foundry-doctor/<version>.
# <version> is the tag suffix, for example v0.1.0 (the release tag is foundry-doctor/v0.1.0).
# Linux only (amd64, arm64). Windows uses install.ps1. macOS is not supported (ADR-011 Amendment 2).
set -euo pipefail

version="${1:-}"
if [[ -z "$version" ]]; then
  echo "usage: $0 <version> [install-dir]" >&2
  exit 2
fi

install_dir="${2:-$HOME/.local/bin}"
repo="${FOUNDRY_DOCTOR_REPO:-RuairiSpain/copilot-Web}"
base_url="${FOUNDRY_DOCTOR_BASE_URL:-https://github.com/${repo}/releases/download/foundry-doctor/${version}}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "unsupported architecture: $arch" >&2; exit 2 ;;
esac
case "$os" in
  linux) ;;
  *) echo "unsupported OS: $os (install.sh supports Linux only; on Windows use install.ps1)" >&2; exit 2 ;;
esac

asset="foundry-doctor_${version}_${os}_${arch}"
workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

curl -fsSL "${base_url}/SHA256SUMS" -o "${workdir}/SHA256SUMS"
curl -fsSL "${base_url}/${asset}" -o "${workdir}/${asset}"

# Lines look like "<hash>  <name>" or "<hash> *<name>". Match the exact asset name, never a substring.
( cd "$workdir" \
  && awk -v a="$asset" '{ f = $2; sub(/^\*/, "", f) } f == a { print $1 "  " a }' SHA256SUMS > expected.sum \
  && [ -s expected.sum ] \
  && sha256sum -c expected.sum ) || { echo "checksum verification failed for ${asset}" >&2; exit 1; }

mkdir -p "$install_dir"
install -m 0755 "${workdir}/${asset}" "${install_dir}/foundry-doctor"
echo "installed foundry-doctor ${version} to ${install_dir}/foundry-doctor"
