"""Publish the schema files under ``schemas/`` from the packaged source of truth.

Run ``python scripts/build_schemas.py`` after editing
``src/xfoundry/schema/x-foundry.schema.json``; ``--check`` fails if ``schemas/`` is stale.
"""

from __future__ import annotations

import argparse
import json
import shutil
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SOURCE = ROOT / "src" / "xfoundry" / "schema" / "x-foundry.schema.json"
OUT = ROOT / "schemas"
PUBLISHED_EXAMPLES = (
    "standalone-minimal",
    "standalone-private",
    "hub-spoke",
    "foundry-iq",
    "hosted-agent-runtime",
    "apim-ai-gateway",
)


def render() -> dict[str, str]:
    """Return ``{relative path: file content}`` for every published file."""
    subtree = json.loads(SOURCE.read_text(encoding="utf-8"))
    defs = dict(subtree["$defs"])
    wrapper = {
        "$schema": subtree["$schema"],
        "$id": subtree["$id"].replace("x-foundry.schema.json", "azure-yaml-x-foundry.schema.json"),
        "title": "azure.yaml with the x-foundry extension",
        "description": (
            "Validates the x-foundry key of an azure.yaml while allowing every native azd "
            "property alongside it. Combine with the native azure.yaml schema in editors "
            "(allOf). x-foundry is implemented by a custom extension and is not a native "
            "azure.yaml capability."
        ),
        "type": "object",
        "properties": {"x-foundry": {"$ref": "#/$defs/xFoundry"}},
        "required": ["x-foundry"],
        "additionalProperties": True,
        "$defs": defs,
    }
    version = subtree["$id"].split("/x-foundry/")[1].split("/")[0].rsplit(".", 1)[0]
    files = {
        "x-foundry.schema.json": json.dumps(subtree, indent=2, ensure_ascii=False) + "\n",
        f"versions/{version}/x-foundry.schema.json": json.dumps(
            subtree, indent=2, ensure_ascii=False
        )
        + "\n",
        "azure-yaml-x-foundry.schema.json": json.dumps(wrapper, indent=2, ensure_ascii=False)
        + "\n",
    }
    for name in PUBLISHED_EXAMPLES:
        files[f"examples/{name}.yaml"] = (ROOT / "examples" / f"{name}.yaml").read_text(
            encoding="utf-8"
        )
    return files


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="fail if schemas/ is out of date")
    args = parser.parse_args()
    files = render()
    if args.check:
        stale = [
            rel
            for rel, content in files.items()
            if not (OUT / rel).is_file() or (OUT / rel).read_text(encoding="utf-8") != content
        ]
        if stale:
            print("stale published schema files:", *stale, sep="\n  ", file=sys.stderr)
            return 1
        return 0
    if OUT.exists():
        shutil.rmtree(OUT)
    for rel, content in files.items():
        target = OUT / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content, encoding="utf-8")
    print(f"wrote {len(files)} files to {OUT}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
