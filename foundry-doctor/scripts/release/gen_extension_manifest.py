#!/usr/bin/env python3
"""Generate the azd extension manifest and registry entry for a release.

Reads the release checksums file (sha256sum format) and emits:
  extension.yaml  - azd extension manifest
  registry.json   - registry entry with per-platform artifacts + checksums

Only artifacts named foundry-doctor-azd-extension_<ver>_<os>_<arch>.(tar.gz|zip)
are listed. Standard library only.
"""
from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

EXTENSION_ID = "foundry-doctor"  # TODO publisher prefix undecided; must not be microsoft.foundry
NAMESPACE = "foundry"
REQUIRED_AZD = ">=1.34.2"
SEMVER = re.compile(r"^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$")
ARTIFACT = re.compile(
    r"^foundry-doctor-azd-extension_(?P<ver>[^_]+)_(?P<os>linux|darwin|windows)_(?P<arch>amd64|arm64)\.(?:tar\.gz|zip)$"
)
ID_PATTERN = re.compile(r"^[a-z0-9-.]+$")


def parse_checksums(text: str) -> dict[str, str]:
    out: dict[str, str] = {}
    for line in text.splitlines():
        line = line.strip()
        if not line:
            continue
        m = re.fullmatch(r"([0-9a-fA-F]{64})\s+\*?(.+)", line)
        if not m:
            raise ValueError(f"malformed checksum line: {line!r}")
        out[Path(m.group(2)).name] = m.group(1).lower()
    return out


def build(version: str, checksums: dict[str, str], base_url: str, ext_id: str = EXTENSION_ID):
    if not SEMVER.match(version):
        raise ValueError(f"invalid semver: {version}")
    if not ID_PATTERN.match(ext_id) or ext_id == "microsoft.foundry":
        raise ValueError(f"invalid extension id: {ext_id}")
    artifacts = []
    platforms = []
    for name in sorted(checksums):
        m = ARTIFACT.match(name)
        if not m or m.group("ver") != version:
            continue
        key = f"{m.group('os')}/{m.group('arch')}"
        platforms.append(key)
        artifacts.append(
            {
                "platform": key,
                "url": f"{base_url.rstrip('/')}/{name}",
                "checksum": {"algorithm": "sha256", "value": checksums[name]},
            }
        )
    if not artifacts:
        raise ValueError("no azd extension artifacts found in checksums")
    manifest = {
        "id": ext_id,
        "namespace": NAMESPACE,
        "displayName": "Foundry Doctor (community)",
        "description": "Read-only diagnostics for Microsoft Foundry azd projects. Community/independent tool.",
        "version": version,
        "capabilities": ["custom-commands", "metadata"],
        "requiredAzdVersion": REQUIRED_AZD,
        "entryPoint": "foundry-doctor-azd",
        "usage": "azd foundry doctor [--profile dev|test|prod] [--local]",
        "tags": ["foundry", "diagnostics", "community"],
        "platforms": platforms,
    }
    registry = {
        "extensions": [
            {
                "id": ext_id,
                "namespace": NAMESPACE,
                "displayName": manifest["displayName"],
                "description": manifest["description"],
                "versions": [
                    {
                        "version": version,
                        "capabilities": manifest["capabilities"],
                        "usage": manifest["usage"],
                        "artifacts": artifacts,
                    }
                ],
            }
        ]
    }
    return manifest, registry


def to_yaml(obj: dict) -> str:
    lines = []
    for k, v in obj.items():
        if isinstance(v, list):
            lines.append(f"{k}:")
            lines.extend(f"  - {json.dumps(i)}" for i in v)
        else:
            lines.append(f"{k}: {json.dumps(v)}")
    return "\n".join(lines) + "\n"


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--version", required=True)
    p.add_argument("--checksums", required=True, type=Path)
    p.add_argument("--base-url", required=True)
    p.add_argument("--out-dir", required=True, type=Path)
    p.add_argument("--extension-id", default=EXTENSION_ID)
    a = p.parse_args(argv)
    try:
        manifest, registry = build(
            a.version, parse_checksums(a.checksums.read_text(encoding="utf-8")), a.base_url, a.extension_id
        )
    except ValueError as e:
        print(f"error: {e}", file=sys.stderr)
        return 2
    a.out_dir.mkdir(parents=True, exist_ok=True)
    (a.out_dir / "extension.yaml").write_text(to_yaml(manifest), encoding="utf-8")
    (a.out_dir / "registry.json").write_text(json.dumps(registry, indent=2) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    sys.exit(main())
