#!/usr/bin/env bash
# Vendors the azd azure.yaml schemas into schemas/vendor/azd/ (CFG-001, ADR-011).
#
#   scripts/ci/vendor-azd-schemas.sh /path/to/azure-dev-clone
#
# Needs a clone of github.com/Azure/azure-dev that contains both commits below. It reads files with `git show`
# (no checkout), rewrites the absolute raw.githubusercontent.com $id and $ref values to relative paths inside
# schemas/vendor/azd/, and rewrites SHA256SUMS. After a refresh, update the recorded hashes in
# schemas/vendor/azd/vendor_test.go and the commits in schemas/vendor/azd/README.md in the same change.
# The doctor never needs the network at run time; this script is the only place that does git reads.
set -euo pipefail

ROOT_COMMIT=170ebb858071da353cc9bf8657a377bff268ba36 # tag azure-dev-cli_1.35.0
EXT_COMMIT=a64ca8fe04d0bc1b0375c32fed1b76573e2a91ef  # tags azd-ext-azure-ai-*; see README.md

clone="${1:-}"
if [[ -z "$clone" || ! -d "$clone" ]]; then
  echo "usage: $0 /absolute/path/to/azure-dev-clone" >&2
  exit 2
fi
here="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
out="$here/schemas/vendor/azd"
base='https://raw.githubusercontent.com/Azure/azure-dev/main'

rewrite() { # $1 = path prefix to strip from the URL, stdin -> stdout
  sed -E \
    -e "/\"\\\$(ref|id)\": *\"https:/ s#${base}/schemas/v1\\.0/#./#" \
    -e "/\"\\\$(ref|id)\": *\"https:/ s#${base}/cli/azd/extensions/(azure\\.ai\\.[a-z]+)/schemas/#\\1/#"
}

mkdir -p "$out"
git -C "$clone" show "$ROOT_COMMIT:schemas/v1.0/azure.yaml.json" | rewrite | sed -E 's#"\./azure\.yaml\.json"#"azure.yaml.json"#' >"$out/azure.yaml.json"

ext_files=(
  azure.ai.agents/schemas/azure.ai.agent.json
  azure.ai.agents/schemas/microsoft.foundry.json
  azure.ai.agents/schemas/Agent.json
  azure.ai.agents/schemas/Connection.json
  azure.ai.agents/schemas/Deployment.json
  azure.ai.agents/schemas/FileRef.json
  azure.ai.agents/schemas/Routine.json
  azure.ai.agents/schemas/Skill.json
  azure.ai.agents/schemas/Toolbox.json
  azure.ai.connections/schemas/azure.ai.connection.json
  azure.ai.evaluations/schemas/azure.ai.eval.json
  azure.ai.projects/schemas/azure.ai.project.json
  azure.ai.routines/schemas/azure.ai.routine.json
  azure.ai.skills/schemas/azure.ai.skill.json
  azure.ai.toolboxes/schemas/azure.ai.toolbox.json
)
for f in "${ext_files[@]}"; do
  ext="${f%%/*}"
  name="${f##*/}"
  mkdir -p "$out/$ext"
  git -C "$clone" show "$EXT_COMMIT:cli/azd/extensions/$f" | rewrite >"$out/$ext/$name"
done

if grep -rn 'raw\.githubusercontent\.com' "$out" --include='*.json' | grep -E '"\$(ref|id)"'; then
  echo "unrewritten \$ref/\$id remain" >&2
  exit 1
fi

(cd "$out" && find . -name '*.json' -type f | LC_ALL=C sort | sed 's#^\./##' | xargs sha256sum >SHA256SUMS)
echo "vendored into $out"
