"""A live check of the Foundry behaviour the kit depends on. Run it against a sandbox project.

It creates a session, calls the agent, stops the session, calls again (a resume), forces a sync,
deletes the session and, if asked, fills a low test quota to see the quota errors. It touches only
sessions it created, and deletes them at the end.

Environment: FOUNDRY_PROJECT_ENDPOINT, and credentials that DefaultAzureCredential finds.
    python scripts/live_canary.py --agent canary-agent
    python scripts/live_canary.py --agent canary-agent --quota-probe 40   # needs a low quota
    python scripts/live_canary.py --fake                                  # no Azure: self-test

Exit status is 0 when every step passes. A failure means the platform no longer behaves as the kit
assumes: treat it as a release blocker.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import sys
import time
from collections.abc import Awaitable, Callable
from dataclasses import asdict, dataclass

from hosted_agent_kit import AgentResult, Hack, KitSettings
from hosted_agent_kit.errors import HackError, RegionalCapacityError, SessionQuotaError
from hosted_agent_kit.testing import DemoFoundry


@dataclass
class Step:
    name: str
    ok: bool
    detail: str = ""
    seconds: float = 0.0


async def timed(name: str, action: Callable[[], Awaitable[str]]) -> Step:
    started = time.perf_counter()
    try:
        detail = await action()
        return Step(name, True, detail, time.perf_counter() - started)
    except Exception as exc:
        return Step(name, False, f"{type(exc).__name__}: {exc}", time.perf_counter() - started)


async def run(kit: Hack, agent: str, quota_probe: int = 0) -> list[Step]:
    steps: list[Step] = []
    created: list[str] = []
    user = "canary"

    async def first_call() -> str:
        result = await kit.ask(agent, "canary: reply with any text", user_id=user)
        assert result.ok, f"status {result.status_code}"
        return f"status {result.status_code}"

    async def sessions_visible() -> str:
        sessions = await kit.reporting.sessions(agent)
        assert sessions, "the kit has no session after a successful call"
        created.extend(s.session_id for s in sessions)
        return f"{len(sessions)} session(s)"

    async def stop_and_resume() -> str:
        session_id = created[0]
        await kit.runtime.adapter.stop_session(agent, session_id)
        result = await kit.ask(agent, "canary: after a stop", user_id=user)
        assert result.ok, f"status {result.status_code}"
        return "the session resumed after stop_session"

    async def sync_now() -> str:
        report = await kit.admin.sync(agent)
        assert report.outcome in ("success", "partial"), report.outcome
        return f"outcome {report.outcome}, discovered {report.discovered}"

    async def delete_all() -> str:
        for session_id in list(created):
            await kit.admin.delete_session(agent, session_id)
        remaining = await kit.reporting.sessions(agent)
        assert not [s for s in remaining if s.session_id in created], "a session was not removed"
        created.clear()
        return "deleted"

    async def probe() -> str:
        """Hold ``quota_probe`` streams open at once, so each needs its own session, until
        Foundry answers with a session quota error."""
        refusals: list[HackError] = []
        held: list[AgentResult] = []

        async def open_one(i: int) -> None:
            try:
                held.append(
                    await kit.responses(
                        agent, user_id=f"probe-{i}", input={"input": "canary: hold"}, stream=True
                    )
                )
            except (RegionalCapacityError, SessionQuotaError) as exc:
                refusals.append(exc)

        try:
            await asyncio.wait_for(
                asyncio.gather(*(open_one(i) for i in range(quota_probe))), timeout=600
            )
        finally:
            for result in held:
                await result.aclose()
        assert refusals, f"no quota error with {quota_probe} sessions open: the quota is too high"
        return f"{len(held)} open, then {type(refusals[0]).__name__}: {refusals[0].code}"

    for name, action in (
        ("first call", first_call),
        ("session is tracked", sessions_visible),
        ("stop then resume", stop_and_resume),
        ("sync", sync_now),
    ):
        steps.append(await timed(name, action))
        if not steps[-1].ok:
            break
    if quota_probe and all(s.ok for s in steps):
        steps.append(await timed(f"quota error within {quota_probe} sessions", probe))
    # Clean up what the canary made, whatever happened.
    for session in await kit.reporting.sessions(agent):
        if session.session_id not in created:
            created.append(session.session_id)
    steps.append(await timed("delete", delete_all))
    return steps


def fake_kit(agent: str) -> Hack:
    document = {"agentPool": {"agents": {agent: {"mode": "stateless", "max_sessions": 50}}}}
    return Hack.from_dict(document, adapter=DemoFoundry(), settings=KitSettings())


def real_kit(agent: str) -> Hack:
    document = {"agentPool": {"agents": {agent: {"mode": "stateless", "max_sessions": 200}}}}
    return Hack.from_dict(document, settings=KitSettings(create_ready_timeout_seconds=300))


async def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("--agent", default="canary-agent", help="a stateless Responses agent")
    parser.add_argument("--quota-probe", type=int, default=0, metavar="N")
    parser.add_argument("--fake", action="store_true", help="use the in-memory fake")
    parser.add_argument("--json", action="store_true", dest="as_json")
    args = parser.parse_args(argv)
    kit = fake_kit(args.agent) if args.fake else real_kit(args.agent)
    async with kit:
        steps = await run(kit, args.agent, args.quota_probe)
    if args.as_json:
        print(json.dumps([asdict(s) for s in steps], indent=2))
    else:
        for step in steps:
            mark = "PASS" if step.ok else "FAIL"
            print(f"{mark}  {step.name:<34} {step.seconds:6.2f}s  {step.detail}")
    return 0 if all(s.ok for s in steps) else 1


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
