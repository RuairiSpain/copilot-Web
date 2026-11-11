"""teams-voice

Invite mode (phone number, Foundry + ACS):  setup | calls | watch | end <id> | consent
Calendar mode (Graph, agent's Outlook):     calendar run | list | join-url <url>
Either mode:                                run   (starts the mode chosen by JOIN_MODE=invite|calendar)

The two modes share no code. Each branch below imports only its own package.
"""

from __future__ import annotations

import argparse
import json
import logging
import os


def _invite(args) -> int:
    from .config import Settings
    from .foundry import FoundryVoice, val

    voice = FoundryVoice(Settings.from_env())
    if args.cmd == "setup":
        agent = voice.ensure_agent(update=args.update_agent)
        b = voice.ensure_binding()
        print(json.dumps({"agent": agent, "binding_id": b.id, "status": b.status, "incoming_call_url": b.incoming_call_url}))
    elif args.cmd == "calls":
        for call in voice.calls(in_progress_only=not args.all):
            print(call.id, val(call.status), val(call.phase), getattr(call, "caller_number", ""))
    elif args.cmd in ("watch", "run"):
        voice.watch(iterations=getattr(args, "iterations", None))
    elif args.cmd == "end":
        print(val(voice.end_call(args.call_id).status))
    return 0


def _consent(args) -> int:
    from .config import normalize_resource_account_id
    from .consent import grant_consent

    key = os.environ["ACS_ACCESS_KEY"]  # from the environment so it never appears in a process list
    status = grant_consent(args.acs_host, key, args.teams_tenant_id, normalize_resource_account_id(args.resource_account_id))
    print(json.dumps({"consent_http_status": status}))
    return 0


def _calendar(args) -> int:
    from .calendar_join.graph import GraphClient
    from .calendar_join.joiner import join
    from .calendar_join.runner import run_calendar
    from .calendar_join.scheduler import CalendarScheduler, StateStore
    from .calendar_join.settings import CalendarSettings

    if args.cal_cmd == "run":
        run_calendar(once=args.once)
    elif args.cal_cmd == "list":
        s = CalendarSettings.from_env()
        for m in CalendarScheduler(s, GraphClient(), StateStore(s.state_file)).upcoming(args.hours):
            print(m.start.isoformat(), m.end.isoformat(), m.subject)
    elif args.cal_cmd == "join-url":
        s = CalendarSettings.from_env()
        if not s.callback_url:
            raise SystemExit("CALLBACK_URL is required")
        print(join(GraphClient(), args.url, s.callback_url, s.tenant_id))
    return 0


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="teams-voice")
    sub = p.add_subparsers(dest="cmd", required=True)
    st = sub.add_parser("setup", help="[invite] create the voice agent and Teams telephony binding if missing; print JSON")
    st.add_argument("--update-agent", action="store_true", help="publish a new agent version (use after changing instructions or wake words)")
    c = sub.add_parser("calls", help="[invite] list calls")
    c.add_argument("--all", action="store_true", help="include finished calls")
    w = sub.add_parser("watch", help="[invite] log call start/end and enforce MAX_CALL_SECONDS")
    w.add_argument("--iterations", type=int, default=None, help="stop after N polls (default: run forever)")
    e = sub.add_parser("end", help="[invite] end a call")
    e.add_argument("call_id")
    k = sub.add_parser("consent", help="[invite] grant ACS server consent to the resource account")
    k.add_argument("--acs-host", required=True)
    k.add_argument("--teams-tenant-id", required=True)
    k.add_argument("--resource-account-id", required=True)
    sub.add_parser("run", help="run the mode selected by JOIN_MODE (invite or calendar)")
    cal = sub.add_parser("calendar", help="[calendar] join meetings found on the agent's calendar")
    cs = cal.add_subparsers(dest="cal_cmd", required=True)
    r = cs.add_parser("run", help="poll the calendar, join and leave meetings, receive call notifications")
    r.add_argument("--once", action="store_true", help="one poll, no notification server")
    ls = cs.add_parser("list", help="show Teams meetings found on the calendar")
    ls.add_argument("--hours", type=int, default=24)
    ju = cs.add_parser("join-url", help="join one meeting now (for testing)")
    ju.add_argument("url")
    args = p.parse_args(argv)
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(message)s")

    if args.cmd == "consent":
        return _consent(args)
    if args.cmd == "calendar":
        return _calendar(args)
    if args.cmd == "run":
        mode = os.environ.get("JOIN_MODE", "invite").strip().lower()
        if mode == "calendar":
            args.cmd, args.cal_cmd, args.once = "calendar", "run", False
            return _calendar(args)
        if mode != "invite":
            raise SystemExit(f"JOIN_MODE must be 'invite' or 'calendar', not {mode!r}")
    return _invite(args)


if __name__ == "__main__":
    raise SystemExit(main())
