#!/usr/bin/env bash
# Local smoke test: builds a tiny fake docs repo, runs scan -> summarise -> build_site against it
# in a throwaway copy of this project, and prints the result. Does not touch your real data/.
set -euo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
cp -r "$HERE"/{config,tracker,site,requirements.txt} "$WORK"/
mkdir -p "$WORK/state" "$WORK/data" && echo '{}' > "$WORK/state/state.json"
bash "$HERE/tests/make_test_repo.sh" "$WORK/fake-repo" >/dev/null
cd "$WORK"
python -m tracker.scan --repo-path azure-ai-docs="$WORK/fake-repo"
GITHUB_TOKEN= python -m tracker.summarise
python -m tracker.build_site
python -m tracker.scan --repo-path azure-ai-docs="$WORK/fake-repo"   # second run: expect "no new commits"
echo "OK - output in $WORK (open site/ with: python -m http.server -d $WORK/site 8000)"
