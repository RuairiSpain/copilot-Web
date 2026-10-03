"""Git worktree management: one branch and one working folder per agent role.

A worktree is a second checkout of the same repository. Giving each agent its own means
they never overwrite each other's uncommitted files. Work is shared in two explicit steps:

* ``publish`` commits the role's work and merges its branch into ``main``.
* ``refresh`` merges ``main`` into the role's branch, so it sees what others published.

Merges happen at the repository root, under a lock, and a conflicting merge is aborted so
``main`` is never left half-merged.
"""

from __future__ import annotations

import os
import re
import subprocess
import tempfile
import threading
from dataclasses import dataclass
from pathlib import Path

ROLE_NAME_PATTERN = re.compile(r"^[a-z][a-z0-9-]{0,30}$")
DEFAULT_GITIGNORE = (
    ".worktrees/\n__pycache__/\n*.pyc\n.pytest_cache/\n.mypy_cache/\n.ruff_cache/\n.venv/\n"
)
IDENTITY = ("Dev Team Agent", "dev-team@localhost")


class GitError(RuntimeError):
    """A git command failed."""


@dataclass(frozen=True, slots=True)
class MergeResult:
    """Outcome of a merge."""

    ok: bool
    message: str
    conflicts: tuple[str, ...] = ()


@dataclass(frozen=True, slots=True)
class WorktreeInfo:
    """One checkout of the repository."""

    path: Path
    branch: str | None


class WorktreeManager:
    """Creates and merges per-role worktrees inside one repository."""

    # One lock for the whole process: merges touch the shared repository root.
    MERGE_LOCK = threading.Lock()

    def __init__(self, repo: Path, *, main_branch: str = "main", timeout: float = 60) -> None:
        self.repo = repo.resolve()
        self.main_branch = main_branch
        self.timeout = timeout

    # -- helpers -------------------------------------------------------------------------

    def _env(self) -> dict[str, str]:
        name, email = IDENTITY
        return {
            "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
            "HOME": os.environ.get("HOME", tempfile.gettempdir()),
            "LANG": "C.UTF-8",
            "GIT_TERMINAL_PROMPT": "0",
            "GIT_CONFIG_NOSYSTEM": "1",
            "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_AUTHOR_NAME": name,
            "GIT_AUTHOR_EMAIL": email,
            "GIT_COMMITTER_NAME": name,
            "GIT_COMMITTER_EMAIL": email,
        }

    def _git(
        self, *args: str, cwd: Path | None = None, check: bool = True
    ) -> subprocess.CompletedProcess[str]:
        result = subprocess.run(
            ["git", *args],
            cwd=cwd or self.repo,
            env=self._env(),
            capture_output=True,
            text=True,
            timeout=self.timeout,
            check=False,
        )
        if check and result.returncode != 0:
            detail = (result.stderr or result.stdout).strip()
            raise GitError(f"git {' '.join(args)} failed: {detail}")
        return result

    @staticmethod
    def _validate(name: str) -> None:
        if not ROLE_NAME_PATTERN.match(name):
            raise ValueError(
                f"Invalid role name {name!r}: use lower case letters, digits and hyphens."
            )

    def path_for(self, name: str) -> Path:
        """Where the worktree for ``name`` lives."""
        self._validate(name)
        return self.repo / ".worktrees" / name

    def branch_for(self, name: str) -> str:
        self._validate(name)
        return f"agent/{name}"

    # -- lifecycle -----------------------------------------------------------------------

    def ensure_repo(self) -> None:
        """Create the repository and its first commit if they do not exist. Idempotent."""
        self.repo.mkdir(parents=True, exist_ok=True)
        if (self.repo / ".git").exists():
            return
        self._git("init", "-b", self.main_branch)
        (self.repo / ".gitignore").write_text(DEFAULT_GITIGNORE, encoding="utf-8")
        self._git("add", ".gitignore")
        self._git("commit", "-m", "Initial commit")

    def add(self, name: str) -> Path:
        """Create the worktree and branch for ``name``, or return the existing one."""
        path = self.path_for(name)
        if path.exists():
            return path
        self.ensure_repo()
        branch = self.branch_for(name)
        exists = self._git("show-ref", "--verify", "--quiet", f"refs/heads/{branch}", check=False)
        if exists.returncode == 0:
            self._git("worktree", "add", str(path), branch)
        else:
            self._git("worktree", "add", "-b", branch, str(path), self.main_branch)
        return path

    def remove(self, name: str) -> None:
        """Delete the worktree folder. The branch and its commits are kept."""
        path = self.path_for(name)
        if path.exists():
            self._git("worktree", "remove", "--force", str(path))

    def list(self) -> list[WorktreeInfo]:
        """All checkouts of the repository, the root first."""
        out = self._git("worktree", "list", "--porcelain").stdout
        infos: list[WorktreeInfo] = []
        path: Path | None = None
        branch: str | None = None
        for line in [*out.splitlines(), ""]:
            if line.startswith("worktree "):
                path = Path(line.removeprefix("worktree "))
                branch = None
            elif line.startswith("branch "):
                branch = line.removeprefix("branch refs/heads/")
            elif not line and path is not None:
                infos.append(WorktreeInfo(path.resolve(), branch))
                path = None
        return infos

    # -- sharing work --------------------------------------------------------------------

    def commit(self, name: str, message: str) -> bool:
        """Commit everything pending in the role's worktree. Returns False if nothing changed."""
        path = self.path_for(name)
        self._git("add", "-A", cwd=path)
        pending = self._git("diff", "--cached", "--quiet", cwd=path, check=False)
        if pending.returncode == 0:
            return False
        self._git("commit", "-m", message, cwd=path)
        return True

    def _merge(self, branch: str, message: str, cwd: Path) -> MergeResult:
        result = self._git("merge", "--no-ff", "-m", message, branch, cwd=cwd, check=False)
        if result.returncode == 0:
            return MergeResult(True, (result.stdout or "Merged.").strip())
        conflicts = self._git("diff", "--name-only", "--diff-filter=U", cwd=cwd, check=False)
        files = tuple(f for f in conflicts.stdout.splitlines() if f)
        self._git("merge", "--abort", cwd=cwd, check=False)
        detail = (result.stdout + result.stderr).strip()
        return MergeResult(False, f"Merge aborted. {detail}", files)

    def publish(self, name: str, message: str) -> MergeResult:
        """Commit the role's work and merge its branch into ``main``."""
        self.commit(name, message)
        with self.MERGE_LOCK:
            return self._merge(self.branch_for(name), f"Merge {self.branch_for(name)}", self.repo)

    def refresh(self, name: str) -> MergeResult:
        """Bring the latest ``main`` into the role's branch."""
        self.commit(name, f"Save work in progress for {name}")
        with self.MERGE_LOCK:
            return self._merge(
                self.main_branch, f"Merge {self.main_branch} into {name}", self.path_for(name)
            )

    # -- reading state ---------------------------------------------------------------------

    def status(self, name: str) -> str:
        return self._git("status", "--short", "--branch", cwd=self.path_for(name)).stdout.strip()

    def log(self, count: int = 20) -> str:
        """Recent history on ``main``, one line per commit."""
        result = self._git(
            "log", f"-{count}", "--oneline", "--decorate", self.main_branch, check=False
        )
        return result.stdout.strip()
