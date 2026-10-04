"""``xfoundry`` command line: validate and plan an azure.yaml."""

from __future__ import annotations

import argparse
import json
import sys
from collections.abc import Sequence

from xfoundry.diagnostics import Diagnostic, Severity
from xfoundry.plan import analyse_file
from xfoundry.schema import load_schema


def _print_diagnostics(diagnostics: Sequence[Diagnostic], as_json: bool) -> None:
    if as_json:
        print(json.dumps([d.to_dict() for d in diagnostics], indent=2))
    else:
        for d in diagnostics:
            print(d, file=sys.stderr)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="xfoundry", description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    validate = sub.add_parser("validate", help="validate x-foundry in an azure.yaml")
    validate.add_argument("file")
    validate.add_argument("--json", action="store_true", help="print diagnostics as JSON")
    plan = sub.add_parser("plan", help="print the ordered deployment plan")
    plan.add_argument("file")
    plan.add_argument("--json", action="store_true", help="print the full plan as JSON")
    sub.add_parser("schema", help="print the x-foundry JSON Schema")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    if args.command == "schema":
        print(json.dumps(load_schema(), indent=2))
        return 0
    analysis = analyse_file(args.file)
    if analysis.plan is None:
        _print_diagnostics(analysis.diagnostics, getattr(args, "json", False))
        return 1
    plan = analysis.plan
    if args.command == "validate":
        warnings = [d for d in analysis.diagnostics if d.severity is Severity.WARNING]
        _print_diagnostics(warnings, args.json)
        if not args.json:
            print(f"{args.file}: valid ({len(plan.nodes)} resources, {len(warnings)} warning(s))")
    elif args.json:
        print(plan.to_json())
    else:
        for index, layer in enumerate(plan.layers, start=1):
            print(f"step {index}: {', '.join(layer)}")
    return 0
