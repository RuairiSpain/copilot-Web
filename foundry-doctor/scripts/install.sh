#!/usr/bin/env bash
set -euo pipefail

version="${1:-}"
if [[ -z "$version" ]]; then
  echo "usage: $0 <version> [install-dir]" >&2
  exit 2
fi

install_dir="${2:-$HOME/.local/bin}"
repo="${FOUNDRY_DOCTOR_REPO:-ruairispain/copilot-web}"
base_url="${FOUNDRY_DOCTOR_BASE_URL:-https://github.com/${repo}/releases/download/foundry-doctor-v${version}}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "unsupported architecture: $arch" >&2; exit 2 ;;
esac
case "$os" in
  linux|darwin) ;;
  *) echo "unsupported OS: $os" >&2; exit 2 ;;
esac

archive="foundry-doctor_${version}_${os}_${arch}.tar.gz"
workdir="${PWD}/.foundry-doctor-install"
rm -rf "$workdir"
mkdir -p "$workdir"
trap 'rm -rf "$workdir"' EXIT

curl -fsSL "${base_url}/checksums.txt" -o "${workdir}/checksums.txt"
curl -fsSL "${base_url}/${archive}" -o "${workdir}/${archive}"

(cd "$workdir" && grep " ${archive}\$" checksums.txt | sha256sum -c -)
mkdir -p "$install_dir"
tar -xzf "${workdir}/${archive}" -C "$workdir"
install -m 0755 "${workdir}/foundry-doctor" "${install_dir}/foundry-doctor"
echo "installed foundry-doctor ${version} to ${install_dir}/foundry-doctor"
