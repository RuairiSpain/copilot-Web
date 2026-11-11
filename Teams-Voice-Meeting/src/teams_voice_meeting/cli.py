"""teams-voice setup | calls | watch | end <id> | consent"""

from __future__ import annotations

import argparse
import json
import logging
import os

from .config import Settings


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="teams-voice")
    sub = p.add_subparsers(dest="cmd", required=True)
    sub.add_parser("setup", help="create the voice agent and Teams telephony binding if missing; print JSON")
    c = sub.add_parser("calls", help="list calls")
    c.add_argument("--all", action="store_true", help="include finished calls")
    w = sub.add_parser("watch", help="log call start/end and enforce MAX_CALL_SECONDS")
    w.add_argument("--iterations", type=int, default=None, help="stop after N polls (default: run forever)")
    e = sub.add_parser("end", help="end a call")
    e.add_argument("call_id")
    k = sub.add_parser("consent", help="grant ACS server consent to the resource account")
    k.add_argument("--acs-host", required=True)
    k.add_argument("--teams-tenant-id", required=True)
    k.add_argument("--resource-account-id", required=True)
    args = p.parse_args(argv)
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(message)s")

    if args.cmd == "consent":
        from .config import normalize_resource_account_id
        from .consent import grant_consent
        key = os.environ["ACS_ACCESS_KEY"]  # from the environment so it never appears in a process list
        status = grant_consent(args.acs_host, key, args.teams_tenant_id, normalize_resource_account_id(args.resource_account_id))
        print(json.dumps({"consent_http_status": status}))
        return 0

    from .foundry import FoundryVoice, val
    voice = FoundryVoice(Settings.from_env())
    if args.cmd == "setup":
        agent = voice.ensure_agent()
        b = voice.ensure_binding()
        print(json.dumps({"agent": agent, "binding_id": b.id, "status": b.status, "incoming_call_url": b.incoming_call_url}))
    elif args.cmd == "calls":
        for call in voice.calls(in_progress_only=not args.all):
            print(call.id, val(call.status), val(call.phase), getattr(call, "caller_number", ""))
    elif args.cmd == "watch":
        voice.watch(iterations=args.iterations)
    elif args.cmd == "end":
        print(val(voice.end_call(args.call_id).status))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
