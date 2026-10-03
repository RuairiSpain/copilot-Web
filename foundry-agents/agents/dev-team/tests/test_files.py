from __future__ import annotations

from pathlib import Path

import pytest

from tools.files import (
    MAX_WRITE_BYTES,
    OWNERSHIP,
    ROLES,
    FileAccessError,
    list_files,
    owner_of,
    read_file,
    resolve_inside,
    write_file,
)


@pytest.fixture
def root(tmp_path: Path) -> Path:
    base = tmp_path / "project"
    base.mkdir()
    return base


class TestResolveInside:
    @pytest.mark.parametrize(
        "path", ["", "/etc/passwd", "../x", "a/../../x", ".git/config", "src/.git/x"]
    )
    def test_unsafe_paths_are_refused(self, root: Path, path: str) -> None:
        with pytest.raises(FileAccessError):
            resolve_inside(root, path)

    def test_a_symlink_that_escapes_is_refused(self, root: Path, tmp_path: Path) -> None:
        outside = tmp_path / "outside"
        outside.mkdir()
        (root / "link").symlink_to(outside)
        with pytest.raises(FileAccessError, match="leaves the project"):
            resolve_inside(root, "link/stolen.txt")

    def test_normal_paths_resolve_under_the_root(self, root: Path) -> None:
        assert resolve_inside(root, "src/app.py") == (root / "src" / "app.py").resolve()


class TestOwnership:
    def test_every_role_has_a_distinct_set_of_paths(self) -> None:
        for role, prefixes in OWNERSHIP.items():
            for prefix in prefixes:
                assert owner_of(prefix) == role, f"{prefix} is claimed by more than one role"

    def test_the_four_roles(self) -> None:
        assert ROLES == ("lead", "planner", "writer", "tester")

    @pytest.mark.parametrize(
        ("role", "path"),
        [
            ("lead", "docs/brief.md"),
            ("planner", "docs/tasks.md"),
            ("planner", "docs/guides/start.md"),
            ("writer", "src/pkg/models.py"),
            ("writer", "pyproject.toml"),
            ("tester", "tests/test_models.py"),
            ("tester", "docs/testing.md"),
        ],
    )
    def test_owners_can_write(self, root: Path, role: str, path: str) -> None:
        assert write_file(root, role, path, "x").startswith("Wrote")
        assert (root / path).read_text() == "x"

    @pytest.mark.parametrize(
        ("role", "path", "owner"),
        [
            ("writer", "tests/test_x.py", "tester"),
            ("tester", "src/pkg/x.py", "writer"),
            ("planner", "src/pkg/x.py", "writer"),
            ("writer", "docs/tasks.md", "planner"),
            ("planner", "docs/brief.md", "lead"),
            ("lead", "src/x.py", "writer"),
        ],
    )
    def test_other_roles_files_are_refused_with_a_hint(
        self, root: Path, role: str, path: str, owner: str
    ) -> None:
        with pytest.raises(FileAccessError, match=f"belongs to the {owner} role"):
            write_file(root, role, path, "x")
        assert not (root / path).exists()

    def test_nobody_owns_stray_files(self, root: Path) -> None:
        with pytest.raises(FileAccessError, match="may not write"):
            write_file(root, "writer", "Makefile", "x")

    def test_unknown_roles_are_refused(self, root: Path) -> None:
        with pytest.raises(FileAccessError, match="Unknown role"):
            write_file(root, "intern", "src/x.py", "x")

    def test_oversized_files_are_refused(self, root: Path) -> None:
        with pytest.raises(FileAccessError, match="larger than"):
            write_file(root, "writer", "src/big.py", "x" * (MAX_WRITE_BYTES + 1))


class TestReadAndList:
    def test_read_back(self, root: Path) -> None:
        write_file(root, "writer", "src/a.py", "print(1)")
        assert read_file(root, "src/a.py") == "print(1)"

    def test_long_files_are_truncated(self, root: Path) -> None:
        write_file(root, "writer", "src/a.py", "x" * 100)
        assert "truncated: 90 more" in read_file(root, "src/a.py", max_chars=10)

    def test_reading_a_folder_or_missing_file_fails(self, root: Path) -> None:
        (root / "src").mkdir()
        for path in ("src", "nope.txt"):
            with pytest.raises(FileAccessError, match="not a file"):
                read_file(root, path)

    def test_listing_skips_caches_git_and_worktrees(self, root: Path) -> None:
        write_file(root, "writer", "src/a.py", "x")
        for hidden in (
            ".git/HEAD",
            "src/__pycache__/a.pyc",
            ".worktrees/w/src/b.py",
            ".pytest_cache/x",
        ):
            path = root / hidden
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("x")
        assert list_files(root) == ["src/a.py"]

    def test_listing_a_subfolder_and_errors(self, root: Path) -> None:
        write_file(root, "tester", "tests/test_a.py", "x")
        write_file(root, "writer", "src/a.py", "x")
        assert list_files(root, "tests") == ["tests/test_a.py"]
        with pytest.raises(FileAccessError, match="not a folder"):
            list_files(root, "missing")
