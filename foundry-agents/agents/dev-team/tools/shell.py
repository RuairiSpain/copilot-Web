"""A small, allow-listed command runner for the agents.

Agents get a command line because git, pytest and friends are the natural tools for the
job. They do not get a shell. Every command is parsed, checked against an allow-list,
confined to the workspace, and run with a time limit and a scrubbed environment.

What is blocked, and why:

* No shell is involved, so pipes, redirects and ``$(...)`` have no meaning.
* Only the executables in ``ALLOWED_EXECUTABLES`` run. ``python -c`` and ``python -`` do
  not, so the model cannot run arbitrary inline code.
* git is limited to local subcommands. There is no push, pull, fetch, remote, clone or
  config, and global options such as ``-C`` or ``--git-dir`` are refused.
* Absolute paths outside the workspace and any ``..`` path part are refused.
* The environment passed to the child holds no secrets. The agent's own credentials and
  tokens are never visible to a command.
"""

from __future__ import annotations

import os
import shlex
import shutil
import signal
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path

ALLOWED_EXECUTABLES = frozenset(
    {
        "git",
        "python",
        "python3",
        "pytest",
        "ruff",
        "mypy",
        "ls",
        "cat",
        "head",
        "tail",
        "wc",
        "grep",
    }
)
ALLOWED_GIT_SUBCOMMANDS = frozenset(
    {"status", "diff", "log", "add", "commit", "show", "rev-parse", "ls-files", "restore"}
)
ALLOWED_PYTHON_MODULES = frozenset({"pytest", "ruff", "mypy", "compileall"})
# Flags that make a command write somewhere other than the working tree.
FORBIDDEN_FLAG_PREFIXES = ("--output", "--git-dir", "--work-tree", "--exec-path")
DEFAULT_IDENTITY = ("Dev Team Agent", "dev-team@localhost")


class ShellError(ValueError):
    """A command was refused by policy, or could not be started."""


@dataclass(frozen=True, slots=True)
class CommandResult:
    """What a command did."""

    exit_code: int | None
    output: str
    timed_out: bool = False
    truncated: bool = False

    @property
    def ok(self) -> bool:
        return self.exit_code == 0 and not self.timed_out

    def render(self) -> str:
        """Text for the model."""
        if self.timed_out:
            head = "TIMED OUT: the command was killed. Run something smaller."
        else:
            head = f"exit code: {self.exit_code}"
        note = "\n[output truncated]" if self.truncated else ""
        return f"{head}\n{self.output}{note}".rstrip()


def _inside(path: Path, root: Path) -> bool:
    return path == root or root in path.parents


def _check_argument(arg: str, root: Path) -> None:
    if "\x00" in arg or "\n" in arg or "\r" in arg:
        raise ShellError("Arguments may not contain control characters.")
    if arg.startswith("-"):
        if arg.startswith(FORBIDDEN_FLAG_PREFIXES):
            raise ShellError(f"The option {arg.split('=')[0]!r} is not allowed.")
        return
    candidate = Path(arg)
    if candidate.is_absolute():
        if not _inside(candidate.resolve(), root):
            raise ShellError(f"The path {arg!r} is outside the workspace.")
    elif ".." in candidate.parts:
        raise ShellError(f"The path {arg!r} climbs out of the working folder.")


def _check_rules(argv: list[str]) -> None:
    """Per-executable rules. Run before the path checks so the message names the real cause."""
    name = argv[0]
    if name == "git":
        if len(argv) < 2 or argv[1].startswith("-"):
            raise ShellError("Give git a subcommand first. Global git options are not allowed.")
        if argv[1] not in ALLOWED_GIT_SUBCOMMANDS:
            allowed = ", ".join(sorted(ALLOWED_GIT_SUBCOMMANDS))
            raise ShellError(f"git {argv[1]!r} is not allowed. Allowed: {allowed}.")
    elif name in {"python", "python3"}:
        if len(argv) < 2:
            raise ShellError("Interactive python is not allowed. Use `python -m <module>`.")
        if argv[1] == "-m":
            module = argv[2] if len(argv) > 2 else ""
            if module not in ALLOWED_PYTHON_MODULES:
                allowed = ", ".join(sorted(ALLOWED_PYTHON_MODULES))
                raise ShellError(f"python -m {module!r} is not allowed. Allowed: {allowed}.")
        elif argv[1].startswith("-") or not argv[1].endswith(".py"):
            raise ShellError("Only `python -m <module>` or `python <script>.py` is allowed.")


def parse_command(command: str, *, cwd: Path, root: Path) -> list[str]:
    """Check ``command`` against the policy and return the argv to execute.

    Raises:
        ShellError: if the command is not allowed.
    """
    root = root.resolve()
    if not _inside(cwd.resolve(), root):
        raise ShellError("The working folder is outside the workspace.")
    try:
        argv = shlex.split(command)
    except ValueError as exc:
        raise ShellError(f"Could not parse the command: {exc}") from exc
    if not argv:
        raise ShellError("The command is empty.")

    name = argv[0]
    if name not in ALLOWED_EXECUTABLES:
        allowed = ", ".join(sorted(ALLOWED_EXECUTABLES))
        raise ShellError(f"{name!r} is not an allowed command. Allowed: {allowed}.")

    _check_rules(argv)
    for arg in argv[1:]:
        _check_argument(arg, root)

    if name in {"python", "python3"}:
        argv[0] = sys.executable
        return argv

    resolved = shutil.which(name, path=_search_path())
    if resolved is None:
        raise ShellError(f"{name!r} is not installed in this environment.")
    argv[0] = resolved
    return argv


def _search_path() -> str:
    # The interpreter's own folder first, so `pytest` and friends from its venv are found.
    return os.pathsep.join(
        [str(Path(sys.executable).parent), os.environ.get("PATH", "/usr/bin:/bin")]
    )


def clean_environment(identity: tuple[str, str] = DEFAULT_IDENTITY) -> dict[str, str]:
    """The only environment a child process sees. No tokens, no cloud credentials."""
    name, email = identity
    return {
        "PATH": _search_path(),
        "HOME": os.environ.get("HOME", tempfile.gettempdir()),
        "LANG": "C.UTF-8",
        "PYTHONDONTWRITEBYTECODE": "1",
        "PYTHONUNBUFFERED": "1",
        "GIT_TERMINAL_PROMPT": "0",
        "GIT_CONFIG_NOSYSTEM": "1",
        "GIT_CONFIG_GLOBAL": os.devnull,
        "GIT_AUTHOR_NAME": name,
        "GIT_AUTHOR_EMAIL": email,
        "GIT_COMMITTER_NAME": name,
        "GIT_COMMITTER_EMAIL": email,
    }


def _truncate(text: str, limit: int) -> tuple[str, bool]:
    if len(text) <= limit:
        return text, False
    half = limit // 2
    return f"{text[:half]}\n... [{len(text) - limit} characters omitted] ...\n{text[-half:]}", True


def run_command(
    command: str,
    *,
    cwd: Path,
    root: Path,
    timeout: float = 180,
    max_output_chars: int = 20_000,
    identity: tuple[str, str] = DEFAULT_IDENTITY,
) -> CommandResult:
    """Run an allowed command in ``cwd`` and return its combined output.

    Raises:
        ShellError: if the command is refused. A command that runs and fails is not an
            error here. It comes back as a result with a non-zero exit code.
    """
    argv = parse_command(command, cwd=cwd, root=root)
    process = subprocess.Popen(
        argv,
        cwd=cwd,
        env=clean_environment(identity),
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        errors="replace",
        start_new_session=True,  # lets us kill the whole process group on timeout
    )
    timed_out = False
    try:
        output, _ = process.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        timed_out = True
        os.killpg(process.pid, signal.SIGKILL)
        output, _ = process.communicate()
    text, truncated = _truncate(output or "", max_output_chars)
    return CommandResult(None if timed_out else process.returncode, text, timed_out, truncated)
