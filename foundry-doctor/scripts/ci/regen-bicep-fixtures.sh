#!/usr/bin/env bash
# Regenerates the committed compiled-ARM fixtures and captured compiler diagnostics under
# test/fixtures/bicep from the .bicep sources, using the Bicep CLI named by BICEP_PATH
# (an absolute path). The compiler runs with a minimal environment: no AZURE_*, no tokens.
#
#   BICEP_PATH=/abs/path/to/bicep scripts/ci/regen-bicep-fixtures.sh
#
# Review the diff before committing. TestFixtureARMFresh (go test -tags bicep) fails when the
# committed ARM no longer matches the compiler output after normalisation.
set -euo pipefail

if [[ -z "${BICEP_PATH:-}" || "${BICEP_PATH}" != /* || ! -x "${BICEP_PATH}" ]]; then
  echo "BICEP_PATH must be the absolute path of an executable bicep binary" >&2
  exit 2
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fx="${root}/test/fixtures/bicep"
home="$(mktemp -d)"
trap 'rm -rf "${home}"' EXIT

bicep() {
  env -i HOME="${home}" PATH=/usr/bin:/bin DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1 \
    DOTNET_CLI_TELEMETRY_OPTOUT=1 "${BICEP_PATH}" "$@"
}

echo "bicep: $(bicep --version)"

# build <dir> <file> <out>: compiled ARM to <out>.
build() {
  (cd "${fx}/$1" && bicep build "$2" --stdout --no-restore >"${fx}/$3" 2>/dev/null)
  echo "wrote $3"
}

build spike main.bicep spike/main.arm.json
build foundry foundry.bicep foundry/foundry.arm.json
build foundry-v2 foundry.bicep foundry-v2/foundry.arm.json
build synthetic main.bicep synthetic/main.arm.json

# diag <dir> <file> <out>: stderr of a build, with the absolute fixture directory replaced by {{DIR}}.
# Exit code 1 (compile error) is expected for some files and tolerated.
diag() {
  (cd "${fx}/$1" && bicep build "$2" --stdout --no-restore 2>&1 >/dev/null || true) \
    | sed "s#${fx}/$1#{{DIR}}#g" >"${fx}/$3"
  echo "wrote $3"
}

mkdir -p "${fx}/diagnostics"
diag spike error.bicep diagnostics/error.stderr
diag spike lint-warning.bicep diagnostics/lint-warning.stderr
diag spike secure-output.bicep diagnostics/secure-output.stderr

pdiag() {
  (cd "${fx}/params" && bicep build-params "$1" --stdout --no-restore 2>&1 >/dev/null || true) \
    | sed "s#${fx}/params#{{DIR}}#g" >"${fx}/$2"
  echo "wrote $2"
}
pdiag readenv.bicepparam diagnostics/readenv.stderr
pdiag readenv-default.bicepparam diagnostics/readenv-default.stderr
