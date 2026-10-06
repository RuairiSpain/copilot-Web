#!/usr/bin/env python3
"""ADR-003 check: fail if the upstream azd registry already uses the namespace.

Fetches Azure/azure-dev registry.json and registry.dev.json (main branch) and
fails when any extension namespace equals NAMESPACE or starts with NAMESPACE+'.'.
Exit codes: 0 clear, 1 collision, 2 could not check (never treated as pass).
Standard library only; reads public data, no credentials.
"""
from __future__ import annotations

import argparse
import json
import sys
import urllib.request

BASE = "https://raw.githubusercontent.com/Azure/azure-dev/main/cli/azd/extensions/"
FILES = ("registry.json", "registry.dev.json")


def find_collisions(registry: dict, namespace: str) -> list[str]:
    hits = []
    for ext in registry.get("extensions", []):
        ns = ext.get("namespace", "")
        if ns == namespace or ns.startswith(namespace + "."):
            hits.append(f"{ext.get('id', '?')} ({ns})")
    return hits


def fetch(url: str) -> dict:
    with urllib.request.urlopen(url, timeout=30) as r:  # noqa: S310 - fixed https URL
        return json.load(r)


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--namespace", default="foundry")
    p.add_argument("--registry-file", action="append", default=[], help="local registry JSON (testing/offline)")
    a = p.parse_args(argv)
    try:
        if a.registry_file:
            regs = []
            for f in a.registry_file:
                with open(f, encoding="utf-8") as fh:
                    regs.append(json.load(fh))
        else:
            regs = [fetch(BASE + f) for f in FILES]
    except Exception as e:  # network/parse failure is "unable to run", not pass
        print(f"error: unable to check namespace collision: {e}", file=sys.stderr)
        return 2
    hits = [h for r in regs for h in find_collisions(r, a.namespace)]
    if hits:
        print("namespace collision: " + ", ".join(hits), file=sys.stderr)
        return 1
    print(f"namespace '{a.namespace}' is free in upstream registries")
    return 0


if __name__ == "__main__":
    sys.exit(main())
