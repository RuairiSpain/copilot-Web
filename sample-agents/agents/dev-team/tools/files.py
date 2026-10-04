"""File access for agents, with path confinement and per-role ownership.

Agents write files only through :func:`write_file`. The shell allow-list has no way to
write, so ownership cannot be bypassed. Because each role owns different paths, two
agents never edit the same file and git merges stay clean.
"""

from __future__ import annotations

from pathlib import Path, PurePosixPath

# Which paths each role may write. A trailing slash means "everything below".
OWNERSHIP: dict[str, tuple[str, ...]] = {
    "lead": ("docs/brief.md", "README.md"),
    "planner": ("docs/requirements.md", "docs/tasks.md", "docs/architecture.md", "docs/guides/"),
    "writer": ("src/", "pyproject.toml"),
    "tester": ("tests/", "docs/testing.md"),
}
ROLES = tuple(OWNERSHIP)
SKIPPED_PARTS = frozenset({".git", "__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache"})
MAX_WRITE_BYTES = 200_000
MAX_LISTED_FILES = 500


class FileAccessError(ValueError):
    """The path or the role is not allowed to do this."""


def owner_of(relative: str) -> str | None:
    """The role that owns ``relative``, or ``None`` if nobody does."""
    for role, prefixes in OWNERSHIP.items():
        if _matches(relative, prefixes):
            return role
    return None


def _matches(relative: str, prefixes: tuple[str, ...]) -> bool:
    return any(
        relative.startswith(prefix) if prefix.endswith("/") else relative == prefix
        for prefix in prefixes
    )


def resolve_inside(root: Path, relative: str) -> Path:
    """Resolve ``relative`` under ``root``, refusing anything that escapes it.

    Raises:
        FileAccessError: for empty, absolute, parent-climbing, ``.git`` or symlink-escaping paths.
    """
    if not relative or "\x00" in relative:
        raise FileAccessError("The path is empty or invalid.")
    posix = PurePosixPath(relative)
    if posix.is_absolute():
        raise FileAccessError("Use a path relative to the project root.")
    if ".." in posix.parts:
        raise FileAccessError("Paths may not contain '..'.")
    if ".git" in posix.parts:
        raise FileAccessError("The .git folder is off limits.")
    root = root.resolve()
    target = (root / posix).resolve()
    if target != root and root not in target.parents:
        raise FileAccessError("The path leaves the project folder.")
    return target


def write_file(root: Path, role: str, relative: str, content: str) -> str:
    """Write ``content`` to ``relative`` if ``role`` owns it. Returns a confirmation."""
    if role not in OWNERSHIP:
        raise FileAccessError(f"Unknown role {role!r}.")
    target = resolve_inside(root, relative)
    clean = PurePosixPath(relative).as_posix()
    if not _matches(clean, OWNERSHIP[role]):
        owner = owner_of(clean)
        hint = f" It belongs to the {owner} role." if owner else ""
        allowed = ", ".join(OWNERSHIP[role])
        raise FileAccessError(
            f"The {role} role may not write {clean!r}.{hint} It may write: {allowed}."
        )
    if len(content.encode("utf-8")) > MAX_WRITE_BYTES:
        raise FileAccessError(f"The file is larger than {MAX_WRITE_BYTES} bytes.")
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(content, encoding="utf-8")
    return f"Wrote {clean} ({len(content)} characters)."


def read_file(root: Path, relative: str, max_chars: int = 20_000) -> str:
    """Read a text file under ``root``."""
    target = resolve_inside(root, relative)
    if not target.is_file():
        raise FileAccessError(f"{relative!r} is not a file.")
    text = target.read_text(encoding="utf-8", errors="replace")
    if len(text) > max_chars:
        return text[:max_chars] + f"\n[truncated: {len(text) - max_chars} more characters]"
    return text


def list_files(root: Path, directory: str = ".") -> list[str]:
    """Project-relative paths of files under ``directory``, skipping tool caches."""
    base = root.resolve() if directory in {".", ""} else resolve_inside(root, directory)
    if not base.is_dir():
        raise FileAccessError(f"{directory!r} is not a folder.")
    found: list[str] = []
    for path in sorted(base.rglob("*")):
        relative = path.relative_to(root.resolve())
        if SKIPPED_PARTS & set(relative.parts) or ".worktrees" in relative.parts:
            continue
        if path.is_file():
            found.append(relative.as_posix())
        if len(found) >= MAX_LISTED_FILES:
            break
    return found
