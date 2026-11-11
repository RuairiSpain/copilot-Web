"""Poll the agent's calendar, join meetings as they start, leave after they end."""

from __future__ import annotations

import json
import logging
import os
import time
from datetime import datetime, timedelta, timezone
from typing import Callable

from . import joiner
from .graph import GraphClient
from .meetings import Meeting, meetings_from_events, parse_join_url
from .settings import CalendarSettings

log = logging.getLogger(__name__)


class StateStore:
    """JSON file so a restart does not rejoin meetings or forget calls to leave."""

    def __init__(self, path: str) -> None:
        self.path = path
        try:
            with open(path) as f:
                self.data: dict[str, dict] = json.load(f)
        except (FileNotFoundError, ValueError):
            self.data = {}

    def save(self) -> None:
        tmp = f"{self.path}.tmp"
        with open(tmp, "w") as f:
            json.dump(self.data, f, indent=1)
        os.replace(tmp, self.path)


class CalendarScheduler:
    def __init__(self, settings: CalendarSettings, graph: GraphClient, store: StateStore,
                 now: Callable[[], datetime] = lambda: datetime.now(timezone.utc)) -> None:
        self.s, self.graph, self.store, self.now = settings, graph, store, now

    def upcoming(self, hours: int = 24) -> list[Meeting]:
        now = self.now()
        return meetings_from_events(self.graph.calendar_view(self.s.agent_mailbox, now, now + timedelta(hours=hours)))

    def tick(self) -> None:
        now = self.now()
        lead = timedelta(seconds=self.s.join_lead_seconds)
        horizon = now + lead + timedelta(seconds=self.s.poll_seconds)
        try:
            meetings = meetings_from_events(self.graph.calendar_view(self.s.agent_mailbox, now, horizon))
        except Exception as exc:  # still leave finished calls below
            log.error("calendar read failed: %s", exc)
            meetings = []
        for m in meetings:
            entry = self.store.data.get(m.key)
            retry = entry and entry["status"] == "failed" and entry["attempts"] < self.s.max_join_attempts
            if (entry is None or retry) and m.start - lead <= now < m.end:
                self._join(m, entry["attempts"] if entry else 0)
        self._leave_finished(now)
        self.store.save()

    def _join(self, m: Meeting, attempts: int) -> None:
        if not self.s.callback_url:
            raise RuntimeError("CALLBACK_URL is required to join: Graph posts call notifications to it")
        try:
            target = parse_join_url(m.join_url, self.s.tenant_id)
            if not self.s.allow_external_meetings and target.tenant_id != self.s.tenant_id:
                self.store.data[m.key] = {"status": "skipped", "attempts": attempts, "subject": m.subject,
                                          "end": m.end.isoformat(), "reason": "meeting is outside our tenant"}
                log.warning("skipped %r: organised in another tenant (set ALLOW_EXTERNAL_MEETINGS=true to join)", m.subject)
                return
            call_id = joiner.join(self.graph, m.join_url, self.s.callback_url, self.s.tenant_id)
            self.store.data[m.key] = {"status": "joined", "call_id": call_id, "subject": m.subject,
                                      "end": m.end.isoformat(), "attempts": attempts + 1}
            log.info("joined %r call=%s", m.subject, call_id)
        except Exception as exc:  # keep the loop alive; retry on a later tick
            self.store.data[m.key] = {"status": "failed", "attempts": attempts + 1, "subject": m.subject,
                                      "end": m.end.isoformat(), "error": str(exc)[:300]}
            log.error("join failed for %r (attempt %d): %s", m.subject, attempts + 1, exc)

    def _leave_finished(self, now: datetime) -> None:
        grace = timedelta(seconds=self.s.leave_after_end_seconds)
        for key, e in self.store.data.items():
            if e["status"] == "joined" and now >= datetime.fromisoformat(e["end"]) + grace:
                try:
                    self.graph.delete_call(e["call_id"])
                except Exception as exc:  # already ended is fine
                    log.warning("leave %s: %s", e["call_id"], exc)
                e["status"] = "left"
                log.info("left %r", e.get("subject"))

    def run(self, once: bool = False, sleep: Callable[[float], None] = time.sleep) -> None:
        while True:
            try:
                self.tick()
            except Exception as exc:
                log.error("tick failed: %s", exc)
            if once:
                return
            sleep(self.s.poll_seconds)
