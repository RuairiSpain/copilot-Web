from __future__ import annotations

import sys
from pathlib import Path

import pytest

from tools.shell import (
    ALLOWED_EXECUTABLES,
    ShellError,
    clean_environment,
    parse_command,
    run_command,
)


@pytest.fixture
def root(tmp_path: Path) -> Path:
    (tmp_path / "work").mkdir()
    return tmp_path


def parse(command: str, root: Path) -> list[str]:
    return parse_command(command, cwd=root / "work", root=root)


class TestAllowed:
    @pytest.mark.parametrize(
        "command",
        [
            "git status",
            "git diff --stat",
            "git log --oneline -5",
            "git add src/app.py",
            'git commit -m "Add the model"',
            "python -m pytest -q",
            "python -m pytest tests/test_a.py::test_one -k 'a or b'",
            "python -m compileall -q src",
            "python script.py",
            "ls",
            "ls -la src",
            "cat README.md",
            "grep -rn TODO src",
        ],
    )
    def test_commands_on_the_allow_list(self, command: str, root: Path) -> None:
        assert parse(command, root)

    def test_python_resolves_to_the_running_interpreter(self, root: Path) -> None:
        assert parse("python -m pytest", root)[0] == sys.executable

    def test_every_allowed_executable_is_documented_in_the_module(self) -> None:
        assert {"git", "python", "pytest"} <= ALLOWED_EXECUTABLES


class TestBlocked:
    @pytest.mark.parametrize(
        ("command", "reason"),
        [
            ("rm -rf .", "not an allowed command"),
            ("curl http://example.com", "not an allowed command"),
            ("bash -c ls", "not an allowed command"),
            ("sudo ls", "not an allowed command"),
            ("git push origin main", "not allowed"),
            ("git clone http://x/y", "not allowed"),
            ("git remote add o http://x", "not allowed"),
            ("git config user.name x", "not allowed"),
            ("git -C /etc status", "Global git options"),
            ("git --git-dir=/x status", "Global git options"),
            ("git", "subcommand"),
            ("git diff --output=leak.txt", "not allowed"),
            ("python -c 'import os'", "Only"),
            ("python -", "Only"),
            ("python", "Interactive"),
            ("python -m pip install requests", "not allowed"),
            ("python -m http.server", "not allowed"),
            ("python notes.txt", "Only"),
            ("cat /etc/passwd", "outside the workspace"),
            ("cat ../secret.txt", "climbs out"),
            ("ls ../../", "climbs out"),
            ("cat 'unterminated", "Could not parse"),
            ("", "empty"),
            ("   ", "empty"),
            ("cat 'a\nb'", "control characters"),
        ],
    )
    def test_refused_by_policy(self, command: str, reason: str, root: Path) -> None:
        with pytest.raises(ShellError, match=reason):
            parse(command, root)

    def test_pipes_and_chaining_have_no_effect_because_there_is_no_shell(self, root: Path) -> None:
        argv = parse("ls ; rm -rf x", root)
        assert argv[1:] == [";", "rm", "-rf", "x"]  # passed to ls as plain arguments
        # ... and "rm" is never executed because argv[0] is ls.

    def test_absolute_paths_inside_the_workspace_are_fine(self, root: Path) -> None:
        assert parse(f"cat {root / 'work' / 'a.txt'}", root)

    def test_the_working_folder_must_be_inside_the_root(
        self, root: Path, tmp_path_factory: pytest.TempPathFactory
    ) -> None:
        outside = tmp_path_factory.mktemp("outside")
        with pytest.raises(ShellError, match="outside the workspace"):
            parse_command("ls", cwd=outside, root=root)


class TestRunning:
    def test_captures_output_and_exit_code(self, root: Path) -> None:
        (root / "work" / "hello.py").write_text("print('hi')")
        result = run_command("python hello.py", cwd=root / "work", root=root)
        assert result.ok and result.exit_code == 0
        assert result.output.strip() == "hi"
        assert result.render().startswith("exit code: 0")

    def test_failures_are_results_not_exceptions(self, root: Path) -> None:
        (root / "work" / "fail.py").write_text("import sys; print('bad'); sys.exit(3)")
        result = run_command("python fail.py", cwd=root / "work", root=root)
        assert result.exit_code == 3 and not result.ok
        assert "exit code: 3" in result.render()

    def test_time_limit_kills_the_command(self, root: Path) -> None:
        (root / "work" / "slow.py").write_text("import time; time.sleep(30)")
        result = run_command("python slow.py", cwd=root / "work", root=root, timeout=1)
        assert result.timed_out and result.exit_code is None
        assert "TIMED OUT" in result.render()

    def test_long_output_is_truncated_in_the_middle(self, root: Path) -> None:
        (root / "work" / "loud.py").write_text("print('A' * 5000 + 'Z' * 5000)")
        result = run_command("python loud.py", cwd=root / "work", root=root, max_output_chars=200)
        assert result.truncated
        assert "characters omitted" in result.output
        assert result.output.startswith("A") and result.output.rstrip().endswith("Z")
        assert "[output truncated]" in result.render()

    def test_children_never_see_secrets(self, root: Path, monkeypatch: pytest.MonkeyPatch) -> None:
        monkeypatch.setenv("AZURE_CLIENT_SECRET", "super-secret")
        monkeypatch.setenv("GITHUB_TOKEN", "ghp_abc")
        (root / "work" / "env.py").write_text("import os; print(sorted(os.environ))")
        result = run_command("python env.py", cwd=root / "work", root=root)
        assert "AZURE_CLIENT_SECRET" not in result.output
        assert "GITHUB_TOKEN" not in result.output
        assert "PATH" in result.output

    def test_stdin_is_closed_so_prompts_cannot_hang(self, root: Path) -> None:
        (root / "work" / "ask.py").write_text("input('name? ')")
        result = run_command("python ask.py", cwd=root / "work", root=root, timeout=10)
        assert not result.timed_out and result.exit_code == 1
        assert "EOFError" in result.output

    def test_policy_violations_raise_before_anything_runs(self, root: Path) -> None:
        with pytest.raises(ShellError):
            run_command("git push", cwd=root / "work", root=root)

    def test_the_cleaned_environment_carries_the_git_identity_only(self) -> None:
        env = clean_environment(("Ada", "ada@example.com"))
        assert env["GIT_AUTHOR_NAME"] == "Ada" and env["GIT_COMMITTER_EMAIL"] == "ada@example.com"
        assert env["GIT_TERMINAL_PROMPT"] == "0"
        assert not any("SECRET" in key or "TOKEN" in key for key in env)
