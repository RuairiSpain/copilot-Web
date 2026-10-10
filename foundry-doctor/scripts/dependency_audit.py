#!/usr/bin/env python3
"""Fail-closed licence audit of every module in the selected Go graph."""
import argparse
import json
import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
INVENTORY = ROOT / "docs" / "licence-inventory.md"
NOTICES = ROOT / "THIRD_PARTY_NOTICES.md"
WORKFLOW = ROOT.parent / ".github" / "workflows" / "foundry-doctor-ci.yml"
INSTALL_TOOLS = ROOT / "scripts" / "install-dev-tools.sh"
TOOL_PINS = {
    "govulncheck": ("golang.org/x/vuln/cmd/govulncheck", "v1.7.0"),
    "staticcheck": ("honnef.co/go/tools/cmd/staticcheck", "v0.6.1"),
}
AZURE_DEV_COMMIT = "afe4b2b4d262bab4c11f4937e7a7942557ab0ecd"
MARKER = re.compile(
    r"^<!-- dependency-audit: module=(\S+) version=(\S+) licence=(.+) -->$", re.M
)
APPROVED = {"MIT", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "ISC"}
PROHIBITED = re.compile(
    r"\b(?:AGPL|LGPL|GPL)(?:[- v]?\d+(?:\.\d+)?(?:-only|-or-later|\+)?)?\b|"
    r"\bGNU\s+(?:Affero\s+|Lesser\s+)?General Public License\b",
    re.I,
)
SPDX_LINE = re.compile(r"SPDX-License-Identifier\s*:(.*)$", re.I)
SPDX_TOKEN = re.compile(r"\s*(\(|\)|AND\b|OR\b|WITH\b|[A-Za-z0-9.+-]+)", re.I)


def go_json(*args):
    run = subprocess.run(["go", *args], cwd=ROOT, text=True, capture_output=True)
    if run.returncode:
        raise RuntimeError(f"{' '.join(run.args)} failed:\n{run.stderr}")
    decoder, pos, values = json.JSONDecoder(), 0, []
    while pos < len(run.stdout):
        while pos < len(run.stdout) and run.stdout[pos].isspace():
            pos += 1
        if pos < len(run.stdout):
            value, pos = decoder.raw_decode(run.stdout, pos)
            values.append(value)
    return values


def module_graph():
    modules = []
    for metadata in go_json("list", "-m", "-json", "all"):
        if not isinstance(metadata, dict):
            raise RuntimeError(f"malformed module metadata: {metadata!r}")
        if not metadata.get("Main"):
            modules.append(metadata)
    return modules


def parse_spdx(expression):
    """Parse the SPDX expression subset needed by policy and return canonical text."""
    tokens, pos = [], 0
    while pos < len(expression):
        match = SPDX_TOKEN.match(expression, pos)
        if not match:
            raise RuntimeError(f"malformed SPDX licence expression: {expression!r}")
        tokens.append(match.group(1))
        pos = match.end()
    if not tokens:
        raise RuntimeError("empty SPDX licence expression")
    cursor = 0

    def primary():
        nonlocal cursor
        if cursor >= len(tokens):
            raise RuntimeError(f"malformed SPDX licence expression: {expression!r}")
        if tokens[cursor] == "(":
            cursor += 1
            value = disjunction()
            if cursor >= len(tokens) or tokens[cursor] != ")":
                raise RuntimeError(f"malformed SPDX licence expression: {expression!r}")
            cursor += 1
            return f"({value})"
        identifier = tokens[cursor]
        if identifier.upper() in {"AND", "OR", "WITH"} or identifier == ")":
            raise RuntimeError(f"malformed SPDX licence expression: {expression!r}")
        cursor += 1
        if PROHIBITED.search(identifier):
            raise RuntimeError(f"prohibited licence in SPDX expression: {identifier}")
        if identifier not in APPROVED:
            raise RuntimeError(f"unknown or unapproved SPDX licence: {identifier}")
        if cursor < len(tokens) and tokens[cursor].upper() == "WITH":
            cursor += 1
            if cursor >= len(tokens):
                raise RuntimeError(f"malformed SPDX licence exception: {expression!r}")
            exception = tokens[cursor]
            cursor += 1
            # Exceptions change the licence grant. None are currently approved.
            raise RuntimeError(f"unapproved SPDX licence exception: {exception}")
        return identifier

    def conjunction():
        nonlocal cursor
        value = primary()
        while cursor < len(tokens) and tokens[cursor].upper() == "AND":
            cursor += 1
            value += " AND " + primary()
        return value

    def disjunction():
        nonlocal cursor
        value = conjunction()
        while cursor < len(tokens) and tokens[cursor].upper() == "OR":
            cursor += 1
            value += " OR " + conjunction()
        return value

    result = disjunction()
    if cursor != len(tokens):
        raise RuntimeError(f"malformed SPDX licence expression: {expression!r}")
    return result


def licence_files(directory):
    root = pathlib.Path(directory)
    return sorted(
        (p for p in root.iterdir() if p.is_file() and
         (p.name.lower().startswith(("license", "licence", "copying")) or
          p.name.lower().startswith("notice"))),
        key=lambda p: p.name.lower(),
    )


def licence_for(directory):
    root = pathlib.Path(directory)
    files = licence_files(root)
    if not files:
        raise RuntimeError(f"{root}: no root licence file")
    expressions = []
    for path in files:
        raw = path.read_text(encoding="utf-8", errors="replace")
        if PROHIBITED.search(raw):
            raise RuntimeError(f"{path}: prohibited GPL-family licence text")
        spdx_lines = [re.sub(r"\s*(?:\*/|-->)\s*$", "", m.group(1)).strip()
                      for line in raw.splitlines()
                      for m in [SPDX_LINE.search(line)] if m]
        if "spdx-license-identifier" in raw.lower() and not spdx_lines:
            raise RuntimeError(f"{path}: malformed SPDX licence metadata")
        if spdx_lines:
            if len(set(spdx_lines)) != 1:
                raise RuntimeError(
                    f"{path}: ambiguous multiple SPDX licence declarations")
            expressions.extend(parse_spdx(value) for value in spdx_lines)
            continue
        # NOTICE is supplemental evidence and need not itself contain a grant.
        if path.name.lower().startswith("notice"):
            continue
        text = raw.lower()
        found = []
        if "permission is hereby granted, free of charge" in text:
            found.append("MIT")
        if "apache license" in text and "version 2.0" in text:
            found.append("Apache-2.0")
        bsd = "redistribution and use in source and binary forms" in text
        if bsd and ("advertising materials" in text or
                    "all advertising materials" in text):
            raise RuntimeError(f"{path}: unapproved or ambiguous BSD licence")
        if bsd:
            found.append("BSD-3-Clause" if "neither the name" in text
                         else "BSD-2-Clause")
        if ("permission to use, copy, modify, and/or distribute" in text and
                'the software is provided "as is"' in text):
            found.append("ISC")
        if not found:
            raise RuntimeError(f"{path}: licence is absent, unknown, or not approved")
        expressions.extend(found)
    expressions = list(dict.fromkeys(expressions))
    if not expressions:
        raise RuntimeError(f"{root}: licence is absent, unknown, or not approved")
    return " AND ".join(expressions)


def module_directory(path, version):
    downloaded = go_json("mod", "download", "-json", f"{path}@{version}")
    if (len(downloaded) != 1 or not isinstance(downloaded[0], dict) or
            downloaded[0].get("Error") or
            downloaded[0].get("Path") != path or
            downloaded[0].get("Version") != version or
            not downloaded[0].get("Sum") or
            not isinstance(downloaded[0].get("Dir"), str)):
        raise RuntimeError(
            f"{path}@{version}: download returned malformed or unverified metadata")
    return downloaded[0]["Dir"]


def audit():
    rows = []
    for module in module_graph():
        if not isinstance(module, dict) or module.get("Error"):
            raise RuntimeError(f"malformed module metadata: {module!r}")
        path, version = module.get("Path"), module.get("Version")
        if not isinstance(path, str) or not path:
            raise RuntimeError(f"malformed module path metadata: {module!r}")
        if not isinstance(version, str) or not version:
            raise RuntimeError(f"{path}: selected module has no immutable version")
        rows.append((path, version, licence_for(module_directory(path, version))))
    return sorted(rows)


def inventory_rows():
    rows = [tuple(m.groups()) for m in MARKER.finditer(INVENTORY.read_text(encoding="utf-8"))]
    if len(rows) != len(set(rows)):
        raise RuntimeError("licence inventory contains duplicate dependency-audit markers")
    return sorted(rows)


def check_tool_pins():
    workflow = WORKFLOW.read_text(encoding="utf-8")
    installer = INSTALL_TOOLS.read_text(encoding="utf-8")
    inventory = INVENTORY.read_text(encoding="utf-8")
    if workflow.count(f"ref: {AZURE_DEV_COMMIT}") != 1:
        raise RuntimeError("workflow does not pin Azure/azure-dev to the approved commit")
    if inventory.count(f"`{AZURE_DEV_COMMIT}`") != 1:
        raise RuntimeError("licence inventory does not record the Azure/azure-dev commit")
    for name, (module, version) in TOOL_PINS.items():
        env_name = name.upper()
        expected_env = f"{env_name}_VERSION: {version}"
        expected_workflow_install = f'"{module}@${{{env_name}_VERSION}}"'
        expected_install = f"go install {module}@{version}"
        expected_inventory = f"`{module}@{version}`"
        if workflow.count(expected_env) != 1:
            raise RuntimeError(f"workflow does not pin {name} to {version}")
        if workflow.count(expected_workflow_install) != 1:
            raise RuntimeError(f"workflow does not install the pinned {name}")
        install_lines = re.findall(
            rf"^\s*go install {re.escape(module)}@(\S+)\s*$", installer, re.M)
        if install_lines != [version] or installer.count(expected_install) != 1:
            raise RuntimeError(f"install-dev-tools.sh does not pin {name} to {version}")
        if inventory.count(expected_inventory) != 1:
            raise RuntimeError(f"licence inventory does not pin {name} to {version}")


def render_notices(rows):
    parts = [
        "# Third-Party Notices\n",
        "This file is generated from the checksum-verified selected Go module "
        "graph by `scripts/dependency_audit.py`. Do not edit it independently.\n",
    ]
    for path, version, licence in rows:
        files = licence_files(module_directory(path, version))
        if not files:
            raise RuntimeError(f"{path}@{version}: no notice material")
        parts.append(f"\n## {path} {version}\n")
        parts.append(f"\nLicence classification: `{licence}`\n")
        for source in files:
            text = source.read_text(encoding="utf-8", errors="replace").strip()
            parts.append(f"\n### {source.name}\n\n```text\n{text}\n```\n")
    return "".join(parts)


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("--check-inventory", action="store_true", required=True)
    parser.parse_args(argv)
    try:
        actual, documented = audit(), inventory_rows()
        if actual != documented:
            print("licence inventory does not match the selected Go module graph", file=sys.stderr)
            print(f"documented: {documented}", file=sys.stderr)
            print(f"actual:     {actual}", file=sys.stderr)
            return 1
        check_tool_pins()
        expected_notices = render_notices(actual)
        if NOTICES.read_text(encoding="utf-8") != expected_notices:
            print("THIRD_PARTY_NOTICES.md is missing or stale", file=sys.stderr)
            return 1
        for row in actual:
            print("\t".join(row))
        return 0
    except (OSError, KeyError, ValueError, RuntimeError) as error:
        print(f"dependency-audit: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
