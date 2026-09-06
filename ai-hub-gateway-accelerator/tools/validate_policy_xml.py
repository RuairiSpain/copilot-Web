#!/usr/bin/env python3
"""
validate_policy_xml.py — a real, APIM-dialect-aware structural validator
for this repo's policy fragments and product/API policy XML files.

## Why this exists

guides/enterprise-hardening-checklist.md §8 flagged a real gap: nothing
in this repo's build/CI process validates policy XML well-formedness
before deployment, for any fragment, old or new. A naive strict-XML
parser can't fill that gap on its own, because this codebase's policy
files legitimately embed C# expressions inside XML attribute values —
e.g. `condition="@(context.Operation.Id == "decide-quota-request")"` or
`GetValueOrDefault<string>("assetKind","llm")` — which contain characters
(`<`, `>`, nested `"`) that are illegal in strict XML but are APIM's own
accepted, working authoring convention (confirmed: this exact pattern
already exists in this repo's pre-existing, presumably-deployed
frag-central-cache-manager.xml and default-multi-product-policy.xml, not
just in anything this fork added).

## What this script actually does

1. Finds every `@(...)` and `@{...}` C# expression block in each policy
   file (brace/paren-balanced, so a nested `(...)`/`{...}` inside the
   expression doesn't truncate the match early).
2. Replaces each one with an opaque placeholder token of the SAME
   character — this is the key trick: it keeps line/column numbers
   stable for error reporting, and removes exactly the characters that
   are legitimately "not XML" from APIM's perspective, without touching
   anything that actually needs to be valid XML (tags, attribute
   structure, nesting).
3. Runs a real strict-XML parse (Python's xml.dom.minidom) on what's
   left. A genuine structural bug — an unclosed tag, a stray `<`, a
   missing quote around a plain attribute value, a mismatched element —
   still gets caught, because those live outside the masked regions.
4. Sanity-checks the root element is `<policies>` or `<fragment>` (the
   only two root shapes used anywhere in this repo's policy XML).

## What this does NOT do

- Does not validate the C# expressions' own syntax — a typo inside
  `@(...)` still won't be caught. That needs a real APIM instance (or a
  C# parser this script doesn't have).
- Does not validate against APIM's actual policy XSD/schema (element
  names, valid attribute combinations) — only general XML structure.
- Does not deploy or call anything — read-only, local, no Azure access
  needed. Exit code 0 = every file structurally sound; 1 = at least one
  genuine problem found.

## Usage

    python3 tools/validate_policy_xml.py [root-dir]

Defaults to the repo root (this script's parent directory) if no
argument given. Intended to run in CI as a pre-deployment gate — wire it
into whatever pipeline runs `az deployment` today, per
guides/enterprise-hardening-checklist.md §8's recommendation.
"""

from __future__ import annotations

import sys
import xml.dom.minidom as minidom
import xml.parsers.expat
from pathlib import Path

DEFAULT_ROOT = Path(__file__).resolve().parent.parent

# Only these root elements exist anywhere in this repo's policy XML today
# (product/API-level policies use <policies>, reusable snippets use
# <fragment>). A file with neither is either not a policy file this tool
# should look at, or is missing its root wrapper entirely.
VALID_ROOTS = {"policies", "fragment"}


def mask_csharp_expressions(xml_text: str) -> str:
    """
    Replaces every @(...) and @{...} block with same-length placeholder
    text (using 'x' for structural characters, preserving newlines so
    line numbers in later parser errors still line up with the original
    file). Balanced-delimiter aware, so `@(foo("a", "b"))` doesn't get
    truncated at the first `)`.
    """
    out = []
    i = 0
    n = len(xml_text)
    while i < n:
        if xml_text[i] == "@" and i + 1 < n and xml_text[i + 1] in "({":
            open_ch = xml_text[i + 1]
            close_ch = ")" if open_ch == "(" else "}"
            depth = 0
            j = i + 1
            while j < n:
                if xml_text[j] == open_ch:
                    depth += 1
                elif xml_text[j] == close_ch:
                    depth -= 1
                    if depth == 0:
                        j += 1
                        break
                j += 1
            # xml_text[i:j] is the whole "@(...)"/"@{...}" block,
            # including the outer delimiters. Mask it, keeping newlines
            # so downstream line numbers stay accurate.
            block = xml_text[i:j]
            masked = "".join(c if c == "\n" else "x" for c in block)
            out.append(masked)
            i = j
        else:
            out.append(xml_text[i])
            i += 1
    return "".join(out)


def validate_file(path: Path) -> list[str]:
    """Returns a list of problem descriptions; empty list = file is fine."""
    problems: list[str] = []
    try:
        raw = path.read_text(encoding="utf-8")
    except UnicodeDecodeError as exc:
        return [f"could not read as UTF-8: {exc}"]

    masked = mask_csharp_expressions(raw)

    try:
        dom = minidom.parseString(masked.encode("utf-8"))
    except xml.parsers.expat.ExpatError as exc:
        # exc.lineno is 1-indexed and stays accurate because masking
        # preserves newlines.
        return [f"not well-formed XML at line {exc.lineno}, column {exc.offset}: {exc}"]

    root_name = dom.documentElement.tagName
    if root_name not in VALID_ROOTS:
        problems.append(
            f"unexpected root element <{root_name}> — expected one of {sorted(VALID_ROOTS)} "
            f"(every policy/fragment file in this repo uses one of these)"
        )

    return problems


def find_policy_files(root: Path) -> list[Path]:
    candidates: list[Path] = []
    for pattern in ("**/policies/*.xml", "**/*-policy.xml", "**/frag-*.xml"):
        candidates.extend(root.glob(pattern))
    # De-dupe (a file can match more than one glob) and skip anything
    # under node_modules/dist, same exclusions the rest of this repo's
    # tooling already uses.
    seen: set[Path] = set()
    result: list[Path] = []
    for p in sorted(candidates):
        if "node_modules" in p.parts or "dist" in p.parts:
            continue
        if p in seen:
            continue
        seen.add(p)
        result.append(p)
    return result


def main(argv: list[str]) -> int:
    root = Path(argv[1]).resolve() if len(argv) > 1 else DEFAULT_ROOT
    files = find_policy_files(root)

    if not files:
        print(f"No policy XML files found under {root} — check the path.")
        return 1

    failures: dict[Path, list[str]] = {}
    for f in files:
        problems = validate_file(f)
        if problems:
            failures[f] = problems

    print(f"Checked {len(files)} policy XML file(s) under {root}")
    if not failures:
        print("All structurally sound (masked-C#-expression strict XML parse + root-element check).")
        return 0

    print(f"\n{len(failures)} file(s) with real problems:\n")
    for f, problems in sorted(failures.items()):
        rel = f.relative_to(root) if root in f.parents or f == root else f
        for p in problems:
            print(f"  {rel}: {p}")
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))
