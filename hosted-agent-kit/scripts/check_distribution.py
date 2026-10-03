"""Install the built wheel and sdist into clean virtual environments and exercise them.

Usage: python scripts/check_distribution.py dist

For each distribution this checks: the wheel contains the package, the typing marker, the
samples and their YAML; the core install imports and runs a call against the in-memory fake
without FastAPI installed; the ``fastapi`` extra runs a sample app; the ``hack`` command works.
Nothing is uploaded.
"""

from __future__ import annotations

import subprocess
import sys
import tempfile
import zipfile
from pathlib import Path

REQUIRED_IN_WHEEL = (
    "hosted_agent_kit/py.typed",
    "hosted_agent_kit/samples/README.md",
    "hosted_agent_kit/samples/config/kitchen-sink.yaml",
    "hosted_agent_kit/samples/agents/support-bot/azure.yaml",
    "hosted_agent_kit/samples/agents/support-bot/Dockerfile",
    "hosted_agent_kit/samples/response/basic_ask.py",
)
FORBIDDEN_IN_WHEEL = ("tests/", ".pyc", "__pycache__", ".env", "uv.lock")

CORE_SMOKE = """
import asyncio, importlib.util, sys
assert importlib.util.find_spec("fastapi") is None, "the core install must not need FastAPI"
from hosted_agent_kit import Hack
from hosted_agent_kit.testing import DemoFoundry

async def main():
    kit = Hack.from_yaml_text(
        "agentPool:\\n  agents:\\n    bot:\\n      mode: stateless\\n", adapter=DemoFoundry()
    )
    async with kit:
        result = await kit.ask("bot", "hi", user_id="u")
        assert result.json()["output_text"] == "Echo from bot: hi", result.json()
        assert (await kit.reporting.agents())[0].agent_name == "bot"

asyncio.run(main())
print("core smoke ok")
"""

FASTAPI_SMOKE = """
import os
os.environ.pop("FOUNDRY_PROJECT_ENDPOINT", None)
from fastapi.testclient import TestClient
from hosted_agent_kit.samples.response.basic_ask import app

with TestClient(app) as client:
    reply = client.post("/ask", json={"message": "hi"}, headers={"X-User-Id": "u"})
    assert reply.status_code == 200, reply.text
    assert reply.json()["answer"]["output_text"] == "Echo from support-bot: hi"
print("fastapi smoke ok")
"""


def run(*command: str | Path, cwd: Path | None = None) -> str:
    done = subprocess.run(
        [str(part) for part in command], capture_output=True, text=True, cwd=cwd, check=False
    )
    if done.returncode != 0:
        sys.exit(f"command failed: {' '.join(map(str, command))}\n{done.stdout}\n{done.stderr}")
    return done.stdout


def venv(path: Path) -> Path:
    run(sys.executable, "-m", "venv", path)
    return path / ("Scripts" if sys.platform == "win32" else "bin")


def check_wheel_contents(wheel: Path) -> None:
    with zipfile.ZipFile(wheel) as archive:
        names = archive.namelist()
    missing = [r for r in REQUIRED_IN_WHEEL if r not in names]
    forbidden = [n for n in names for f in FORBIDDEN_IN_WHEEL if f in n]
    if missing or forbidden:
        sys.exit(f"wheel contents wrong. missing={missing} forbidden={forbidden}")
    print(f"wheel contents ok ({len(names)} files)")


def check_install(artifact: Path, label: str) -> None:
    with tempfile.TemporaryDirectory() as tmp:
        work = Path(tmp)
        # core install: no web framework
        bin_dir = venv(work / "core")
        run(bin_dir / "python", "-m", "pip", "install", "--quiet", artifact)
        print(label, run(bin_dir / "python", "-c", CORE_SMOKE, cwd=work).strip())
        out = run(bin_dir / "hack", "--version").strip()
        assert out.startswith("hosted-agent-kit "), out
        listing = run(bin_dir / "hack", "samples", "list")
        assert "response/basic_ask.py" in listing
        assert "config/kitchen-sink.yaml" in listing
        yaml_files = [line for line in listing.splitlines() if line.startswith("config/")]
        run(
            bin_dir / "hack",
            "validate",
            *[
                str(
                    Path(
                        run(
                            bin_dir / "python",
                            "-c",
                            "import hosted_agent_kit.samples as s, pathlib; "
                            "print(pathlib.Path(s.__file__).parent)",
                        ).strip()
                    )
                    / name
                )
                for name in yaml_files
                if name.endswith(".yaml")
            ],
        )
        # fastapi extra: a sample app runs
        extra_dir = venv(work / "web")
        run(extra_dir / "python", "-m", "pip", "install", "--quiet", f"{artifact}[fastapi]")
        print(label, run(extra_dir / "python", "-c", FASTAPI_SMOKE, cwd=work).strip())


def main() -> None:
    dist = Path(sys.argv[1] if len(sys.argv) > 1 else "dist")
    wheels = sorted(dist.glob("*.whl"))
    sdists = sorted(dist.glob("*.tar.gz"))
    if len(wheels) != 1 or len(sdists) != 1:
        sys.exit(f"expected one wheel and one sdist in {dist}, found {wheels} {sdists}")
    check_wheel_contents(wheels[0])
    check_install(wheels[0].resolve(), "wheel:")
    check_install(sdists[0].resolve(), "sdist:")
    print("distribution ok")


if __name__ == "__main__":
    main()
