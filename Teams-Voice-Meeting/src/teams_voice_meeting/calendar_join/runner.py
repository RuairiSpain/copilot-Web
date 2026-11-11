from __future__ import annotations

from .callback import CallRegistry, serve_in_background
from .graph import GraphClient
from .scheduler import CalendarScheduler, StateStore
from .settings import CalendarSettings


def run_calendar(once: bool = False) -> None:
    s = CalendarSettings.from_env()
    if not once:
        serve_in_background(s.listen, CallRegistry())
    CalendarScheduler(s, GraphClient(), StateStore(s.state_file)).run(once=once)
