"""The tools each agent role receives.

Every tool is bound to one role and one worktree. Failures come back as ``ERROR:`` text so
the model can read the reason and adapt, instead of crashing the run.
"""

from __future__ import annotations

import functools
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path
from typing import Annotated, Any

from agent_framework import FunctionTool, tool

from settings import Settings
from tools import files, shell
from tools.worktrees import GitError, WorktreeManager

EXPECTED_ERRORS = (shell.ShellError, files.FileAccessError, GitError, ValueError, OSError)


@dataclass(frozen=True)
class TeamContext:
    """Everything the tools need to know about the shared project."""

    settings: Settings
    manager: WorktreeManager

    def worktree(self, role: str) -> Path:
        """The role's working folder, created on first use."""
        return self.manager.add(role)


def _guarded(func: Callable[..., str]) -> Callable[..., str]:
    """Turn expected failures into ``ERROR:`` text. ``wraps`` keeps the real signature."""

    @functools.wraps(func)
    def wrapper(*args: Any, **kwargs: Any) -> str:
        try:
            return func(*args, **kwargs)
        except EXPECTED_ERRORS as exc:
            return f"ERROR: {exc}"

    return wrapper


def build_role_tools(role: str, ctx: TeamContext) -> list[FunctionTool]:
    """Tools for ``role``: files, commands, git and, for code roles, a test runner."""
    if role not in files.OWNERSHIP:
        raise ValueError(f"Unknown role {role!r}.")
    settings = ctx.settings

    def run(command: str) -> shell.CommandResult:
        return shell.run_command(
            command,
            cwd=ctx.worktree(role),
            root=ctx.manager.repo,
            timeout=settings.command_timeout_seconds,
            max_output_chars=settings.max_output_chars,
        )

    @tool(
        name="run_command",
        description=(
            "Run one allow-listed command in your own working folder. Allowed: git (local "
            "subcommands only), python -m pytest, python <script>.py, ruff, mypy, ls, cat, "
            "head, tail, wc, grep. There is no shell: no pipes, redirects or chaining."
        ),
    )
    @_guarded
    def run_command(command: Annotated[str, "The command line, for example: git status"]) -> str:
        return run(command).render()

    @tool(
        name="read_file", description="Read a text file, given a path relative to the project root."
    )
    @_guarded
    def read_file(path: Annotated[str, "Relative path, for example docs/brief.md"]) -> str:
        return files.read_file(ctx.worktree(role), path)

    allowed = ", ".join(files.OWNERSHIP[role])

    @tool(
        name="write_file",
        description=f"Create or replace a file you own. You may write only: {allowed}.",
    )
    @_guarded
    def write_file(
        path: Annotated[str, "Relative path of the file to write"],
        content: Annotated[str, "The complete new content of the file"],
    ) -> str:
        return files.write_file(ctx.worktree(role), role, path, content)

    @tool(name="list_files", description="List project files under a folder, skipping caches.")
    @_guarded
    def list_files(directory: Annotated[str, "Folder to list"] = ".") -> str:
        names = files.list_files(ctx.worktree(role), directory)
        return "\n".join(names) if names else "(no files)"

    @tool(
        name="git_status",
        description="Show the branch and uncommitted changes in your working folder.",
    )
    @_guarded
    def git_status() -> str:
        ctx.worktree(role)
        return ctx.manager.status(role)

    @tool(name="git_commit", description="Commit all your pending changes with a clear message.")
    @_guarded
    def git_commit(message: Annotated[str, "Commit message, imperative mood"]) -> str:
        ctx.worktree(role)
        if not message.strip():
            raise ValueError("The commit message is empty.")
        return "Committed." if ctx.manager.commit(role, message) else "Nothing to commit."

    @tool(
        name="publish_work",
        description=(
            "Commit your work and merge your branch into main so other agents can see it. "
            "Do this when a deliverable is finished."
        ),
    )
    @_guarded
    def publish_work(message: Annotated[str, "Commit message for pending changes"]) -> str:
        ctx.worktree(role)
        result = ctx.manager.publish(role, message or f"Publish {role} work")
        if result.ok:
            return f"Published to main. {result.message}"
        return (
            f"MERGE CONFLICT in {', '.join(result.conflicts) or 'unknown files'}. {result.message}"
        )

    @tool(
        name="refresh_from_main",
        description="Merge the latest main into your branch to see work other agents published.",
    )
    @_guarded
    def refresh_from_main() -> str:
        ctx.worktree(role)
        result = ctx.manager.refresh(role)
        if result.ok:
            return f"Up to date with main. {result.message}"
        return (
            f"MERGE CONFLICT in {', '.join(result.conflicts) or 'unknown files'}. {result.message}"
        )

    tools: list[FunctionTool] = [
        run_command,
        read_file,
        write_file,
        list_files,
        git_status,
        git_commit,
        publish_work,
        refresh_from_main,
    ]

    if role in {"writer", "tester"}:

        @tool(
            name="run_tests",
            description="Run the project's unit tests with pytest and return the report.",
        )
        @_guarded
        def run_tests(
            args: Annotated[
                str, "Extra pytest arguments, for example: tests/test_x.py -k name"
            ] = "",
        ) -> str:
            return run(f"python -m pytest -q {args}".strip()).render()

        tools.append(run_tests)

    return tools
