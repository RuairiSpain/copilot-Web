"""Command line: ``hack validate``, ``hack plan``, ``hack samples`` and ``hack serve``."""

from __future__ import annotations

import argparse
import json
import shutil
import sys
from collections.abc import Sequence
from importlib import resources
from pathlib import Path

from hosted_agent_kit import __version__, planning
from hosted_agent_kit.config.loader import build_config, build_settings_overrides, parse_yaml
from hosted_agent_kit.config.models import ConfigError
from hosted_agent_kit.config.settings import KitSettings


def _samples_root() -> Path:
    return Path(str(resources.files("hosted_agent_kit") / "samples"))


def _validate(paths: Sequence[str]) -> int:
    failed = 0
    for name in paths:
        path = Path(name)
        try:
            raw = parse_yaml(path.read_text(encoding="utf-8"), name)
            config = build_config(raw, name)
            overrides = build_settings_overrides(raw, name)
            if overrides:
                KitSettings(**overrides)
        except OSError as exc:
            failed += 1
            print(f"FAIL {name}: cannot read file: {exc.strerror}", file=sys.stderr)
            continue
        except (ConfigError, ValueError) as exc:
            failed += 1
            print(f"FAIL {name}: {exc}", file=sys.stderr)
            continue
        print(f"ok   {name}: {len(config.agents)} agent(s): {', '.join(config.names)}")
    return 1 if failed else 0


def _samples(action: str, destination: str | None, force: bool) -> int:
    root = _samples_root()
    if not root.is_dir():
        print("this installation has no samples directory", file=sys.stderr)
        return 1
    if action == "list":
        for item in sorted(root.rglob("*")):
            if item.is_file() and "__pycache__" not in item.parts and item.suffix != ".pyc":
                print(item.relative_to(root).as_posix())
        return 0
    if destination is None:
        print("samples copy needs a destination directory", file=sys.stderr)
        return 2
    target = Path(destination)
    if target.exists() and any(target.iterdir()) and not force:
        print(f"{target} is not empty; use --force to copy into it", file=sys.stderr)
        return 1
    shutil.copytree(
        root, target, dirs_exist_ok=True, ignore=shutil.ignore_patterns("__pycache__", "*.pyc")
    )
    print(f"copied samples to {target}")
    return 0


def _plan(files: Sequence[str], as_json: bool) -> int:
    result = planning.plan([Path(name) for name in files])
    if as_json:
        print(json.dumps(planning.to_dict(result), indent=2))
    else:
        print(planning.render(result))
    return 0 if result.ok else 1


def _serve() -> int:
    try:
        from hosted_agent_kit.service.main import run
    except ModuleNotFoundError as exc:  # the service needs its extra
        print(
            f"missing dependency '{exc.name}'. Install the service extra: "
            "pip install 'hosted-agent-kit[service]'",
            file=sys.stderr,
        )
        return 1
    run()
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="hack", description="Hosted Agent Controller Kit (HACK) tools."
    )
    parser.add_argument("--version", action="version", version=f"hosted-agent-kit {__version__}")
    sub = parser.add_subparsers(dest="command", required=True)

    validate = sub.add_parser("validate", help="check scheduler YAML files")
    validate.add_argument("files", nargs="+", help="scheduler.yaml or azure.yaml files")

    samples = sub.add_parser("samples", help="list or copy the bundled samples")
    samples.add_argument("action", choices=["list", "copy"])
    samples.add_argument("destination", nargs="?")
    samples.add_argument("--force", action="store_true", help="copy into a non-empty directory")

    plan = sub.add_parser(
        "plan", help="check several kits' files together: ownership overlaps and quota budgets"
    )
    plan.add_argument("files", nargs="+", help="one scheduler YAML file per kit")
    plan.add_argument("--json", action="store_true", dest="as_json", help="print JSON")

    sub.add_parser("serve", help="run the standalone HTTP service (needs the service extra)")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    if args.command == "validate":
        return _validate(args.files)
    if args.command == "plan":
        return _plan(args.files, args.as_json)
    if args.command == "samples":
        return _samples(args.action, args.destination, args.force)
    return _serve()


if __name__ == "__main__":
    raise SystemExit(main())
