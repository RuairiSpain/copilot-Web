"""Mutation check for the scheduler, queue, affinity, registry and reconciler.

Each mutant changes one operator, constant, condition, return value or statement in a copy of the
source. The tests that cover that module run against the copy. A mutant is *killed* when a test
fails and *survived* when every test still passes. Survivors point at behaviour the tests do not
pin down, or at equivalent mutants that cannot change behaviour.

Usage:
    uv run python scripts/mutation_check.py [--workers 4] [--only scheduler] [--report out.json]
"""

from __future__ import annotations

import argparse
import ast
import copy
import json
import queue
import shutil
import subprocess
import sys
import tempfile
import threading
from collections import defaultdict
from concurrent.futures import ThreadPoolExecutor
from dataclasses import asdict, dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PKG = "src/hosted_agent_kit"
UNIT = "tests/unit"
TARGETS: dict[str, tuple[str, list[str]]] = {
    "scheduler": (
        f"{PKG}/services/scheduler.py",
        [f"{UNIT}/test_scheduler.py", f"{UNIT}/test_pool_core.py"],
    ),
    "queue": (
        f"{PKG}/adapters/memory_queue.py",
        [f"{UNIT}/test_stores.py", f"{UNIT}/test_pool_core.py", f"{UNIT}/test_pool_edges.py"],
    ),
    "affinity": (
        f"{PKG}/adapters/memory_affinity.py",
        [
            f"{UNIT}/test_stores.py",
            f"{UNIT}/test_pool_core.py",
            f"{UNIT}/test_pool_edges.py",
            f"{UNIT}/test_pool_recovery.py",
        ],
    ),
    "registry": (
        f"{PKG}/adapters/memory_registry.py",
        [
            f"{UNIT}/test_stores.py",
            f"{UNIT}/test_pool_core.py",
            f"{UNIT}/test_pool_edges.py",
            f"{UNIT}/test_reconciler.py",
        ],
    ),
    "circuit": (
        f"{PKG}/services/circuit_breaker.py",
        [f"{UNIT}/test_circuit_breaker.py"],
    ),
    "reconciler": (
        f"{PKG}/services/reconciler.py",
        [
            f"{UNIT}/test_reconciler.py",
            f"{UNIT}/test_pool_edges.py",
            f"{UNIT}/test_restore.py",
        ],
    ),
}
COMPARE = {
    ast.Eq: ast.NotEq, ast.NotEq: ast.Eq, ast.Lt: ast.LtE, ast.LtE: ast.Lt,
    ast.Gt: ast.GtE, ast.GtE: ast.Gt, ast.Is: ast.IsNot, ast.IsNot: ast.Is,
    ast.In: ast.NotIn, ast.NotIn: ast.In,
}  # fmt: skip
LOG_NAMES = {"log_event", "logger"}


@dataclass
class Mutant:
    target: str
    index: int
    line: int
    description: str
    result: str = "pending"  # killed | survived | timeout


def _is_logging(node: ast.AST) -> bool:
    call = node.value if isinstance(node, ast.Expr) else node
    if isinstance(call, ast.Await):
        call = call.value
    if not isinstance(call, ast.Call):
        return False
    func = call.func
    while isinstance(func, ast.Attribute):
        func = func.value
    return isinstance(func, ast.Name) and func.id in LOG_NAMES


class Mutator(ast.NodeTransformer):
    """Counts mutation sites and, when ``apply`` is set, applies exactly that one."""

    def __init__(self, apply: int | None = None) -> None:
        self.apply = apply
        self.count = 0
        self.sites: list[tuple[int, str]] = []

    def _hit(self, node: ast.AST, description: str) -> bool:
        index = self.count
        self.count += 1
        self.sites.append((getattr(node, "lineno", 0), description))
        return self.apply == index

    def visit_Compare(self, node: ast.Compare) -> ast.AST:
        self.generic_visit(node)
        for i, op in enumerate(node.ops):
            replacement = COMPARE.get(type(op))
            if replacement and self._hit(node, f"{type(op).__name__} -> {replacement.__name__}"):
                node.ops[i] = replacement()
        return node

    def visit_BoolOp(self, node: ast.BoolOp) -> ast.AST:
        self.generic_visit(node)
        swapped = ast.Or if isinstance(node.op, ast.And) else ast.And
        if self._hit(node, f"{type(node.op).__name__} -> {swapped.__name__}"):
            node.op = swapped()
        return node

    def visit_UnaryOp(self, node: ast.UnaryOp) -> ast.AST:
        self.generic_visit(node)
        if isinstance(node.op, ast.Not) and self._hit(node, "remove not"):
            return node.operand
        return node

    def visit_BinOp(self, node: ast.BinOp) -> ast.AST:
        self.generic_visit(node)
        if isinstance(node.op, ast.Add | ast.Sub):
            swapped = ast.Sub if isinstance(node.op, ast.Add) else ast.Add
            if self._hit(node, f"{type(node.op).__name__} -> {swapped.__name__}"):
                node.op = swapped()
        return node

    def visit_Constant(self, node: ast.Constant) -> ast.AST:
        value = node.value
        if isinstance(value, bool):
            if self._hit(node, f"{value} -> {not value}"):
                return ast.copy_location(ast.Constant(not value), node)
        elif isinstance(value, int | float) and self._hit(node, f"{value} -> {value + 1}"):
            return ast.copy_location(ast.Constant(value + 1), node)
        return node

    def visit_If(self, node: ast.If) -> ast.AST:
        self.generic_visit(node)
        if self._hit(node, "negate if condition"):
            node.test = ast.copy_location(ast.UnaryOp(ast.Not(), node.test), node.test)
        return node

    def visit_While(self, node: ast.While) -> ast.AST:
        self.generic_visit(node)
        if self._hit(node, "negate while condition"):
            node.test = ast.copy_location(ast.UnaryOp(ast.Not(), node.test), node.test)
        return node

    def visit_Return(self, node: ast.Return) -> ast.AST:
        self.generic_visit(node)
        if (
            node.value is not None
            and not (isinstance(node.value, ast.Constant) and node.value.value is None)
            and self._hit(node, "return None")
        ):
            return ast.copy_location(ast.Return(ast.Constant(None)), node)
        return node

    def visit_Expr(self, node: ast.Expr) -> ast.AST:
        self.generic_visit(node)
        value = node.value.value if isinstance(node.value, ast.Await) else node.value
        if (
            isinstance(value, ast.Call)
            and not _is_logging(node)
            and self._hit(node, "delete statement")
        ):
            return ast.copy_location(ast.Pass(), node)
        return node


def enumerate_sites(source: str) -> list[tuple[int, str]]:
    mutator = Mutator()
    mutator.visit(ast.parse(source))
    return mutator.sites


def mutate(source: str, index: int) -> str:
    tree = ast.parse(source)
    mutator = Mutator(apply=index)
    tree = mutator.visit(copy.deepcopy(tree))
    ast.fix_missing_locations(tree)
    return ast.unparse(tree) + "\n"


def run_tests(workdir: Path, tests: list[str], timeout: int) -> str:
    command = [
        sys.executable,
        "-m",
        "pytest",
        *tests,
        "-x",
        "-q",
        "--no-cov",
        "-p",
        "no:cacheprovider",
        "-o",
        "addopts=",
    ]
    env = {
        "PYTHONPATH": str(workdir / "src"),
        "PATH": "/usr/bin:/bin",
        "PYTHONDONTWRITEBYTECODE": "1",
    }
    try:
        done = subprocess.run(
            command, cwd=workdir, env=env, capture_output=True, timeout=timeout, check=False
        )
    except subprocess.TimeoutExpired:
        return "timeout"
    return "survived" if done.returncode == 0 else "killed"


def make_workdir(base: Path, name: str) -> Path:
    workdir = base / name
    workdir.mkdir(parents=True)
    shutil.copytree(ROOT / "src", workdir / "src")
    shutil.copytree(ROOT / "tests", workdir / "tests", ignore=shutil.ignore_patterns("__pycache__"))
    for name_ in ("pyproject.toml", "agent-pool.example.yaml", "azure.yaml"):
        shutil.copy(ROOT / name_, workdir / name_)
    return workdir


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workers", type=int, default=4)
    parser.add_argument("--only", action="append", choices=sorted(TARGETS))
    parser.add_argument("--report", type=Path, default=ROOT / "mutation-report.json")
    parser.add_argument(
        "--timeout", type=int, default=20, help="seconds per mutant; a hang counts as killed"
    )
    parser.add_argument("--resume", action="store_true", help="skip mutants already in the report")
    args = parser.parse_args()
    names = args.only or list(TARGETS)

    with tempfile.TemporaryDirectory(prefix="mutants-") as tmp:
        base = Path(tmp)
        pool: queue.Queue[Path] = queue.Queue()
        for i in range(args.workers):
            pool.put(make_workdir(base, f"w{i}"))
        baseline = pool.get()
        for name in names:
            if run_tests(baseline, TARGETS[name][1], args.timeout) != "survived":
                print(f"baseline tests fail for {name}; fix them before mutating", file=sys.stderr)
                return 2
        pool.put(baseline)

        jobs: list[Mutant] = []
        sources: dict[str, str] = {}
        for name in names:
            path = ROOT / TARGETS[name][0]
            sources[name] = path.read_text(encoding="utf-8")
            jobs += [
                Mutant(name, i, line, text)
                for i, (line, text) in enumerate(enumerate_sites(sources[name]))
            ]
        print(
            f"{len(jobs)} mutants across {len(names)} modules, {args.workers} workers", flush=True
        )

        done_before: dict[tuple[str, int], str] = {}
        if args.resume and args.report.exists():
            for item in json.loads(args.report.read_text(encoding="utf-8")):
                if item["result"] != "pending":
                    done_before[(item["target"], item["index"])] = item["result"]
        lock = threading.Lock()
        finished = 0

        def checkpoint() -> None:
            args.report.write_text(
                json.dumps([asdict(m) for m in jobs], indent=2), encoding="utf-8"
            )

        def work(mutant: Mutant) -> Mutant:
            nonlocal finished
            previous = done_before.get((mutant.target, mutant.index))
            if previous is not None:
                mutant.result = previous
                return mutant
            workdir = pool.get()
            try:
                relative, tests = TARGETS[mutant.target]
                target = workdir / relative
                target.write_text(mutate(sources[mutant.target], mutant.index), encoding="utf-8")
                mutant.result = run_tests(workdir, tests, args.timeout)
            finally:
                (workdir / TARGETS[mutant.target][0]).write_text(
                    sources[mutant.target], encoding="utf-8"
                )
                pool.put(workdir)
            with lock:
                finished += 1
                checkpoint()
                label = f"{mutant.target}:{mutant.line} {mutant.description}"
                print(f"[{finished}] {label} -> {mutant.result}", flush=True)
            return mutant

        with ThreadPoolExecutor(max_workers=args.workers) as executor:
            results = list(executor.map(work, jobs))

    by_target: dict[str, list[Mutant]] = defaultdict(list)
    for mutant in results:
        by_target[mutant.target].append(mutant)
    print(f"\n{'module':<12}{'mutants':>8}{'killed':>8}{'timeout':>9}{'survived':>10}{'score':>8}")
    for name in names:
        group = by_target[name]
        killed = sum(m.result == "killed" for m in group)
        timed_out = sum(m.result == "timeout" for m in group)
        survived = sum(m.result == "survived" for m in group)
        score = 100 * (killed + timed_out) / len(group) if group else 100.0
        print(f"{name:<12}{len(group):>8}{killed:>8}{timed_out:>9}{survived:>10}{score:>7.1f}%")
    survivors = [m for m in results if m.result == "survived"]
    if survivors:
        print("\nSurvivors:")
        for m in survivors:
            print(f"  {m.target}:{m.line}  {m.description}")
    args.report.write_text(json.dumps([asdict(m) for m in results], indent=2), encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
