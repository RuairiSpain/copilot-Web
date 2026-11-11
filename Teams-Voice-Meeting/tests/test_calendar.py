import json
from datetime import datetime, timedelta, timezone
from urllib.parse import quote

import pytest

from teams_voice_meeting.calendar_join.callback import CallRegistry
from teams_voice_meeting.calendar_join.joiner import build_join_request
from teams_voice_meeting.calendar_join.meetings import (
    meetings_from_events, parse_graph_time, parse_join_url)
from teams_voice_meeting.calendar_join.scheduler import CalendarScheduler, StateStore
from teams_voice_meeting.calendar_join.settings import CalendarSettings

T0 = datetime(2026, 11, 12, 10, 0, tzinfo=timezone.utc)
CTX = {"Tid": "tenant-1", "Oid": "organizer-1", "MessageId": "0"}
LONG = ("https://teams.microsoft.com/l/meetup-join/" + quote("19:meeting_abc@thread.v2", safe="") + "/0?context="
        + quote(json.dumps(CTX), safe=""))
SHORT = "https://teams.microsoft.com/meet/123456789?p=ab+c%26d"


def event(id="e1", start=T0, minutes=30, **kw):
    e = {"id": id, "subject": "Standup", "isOnlineMeeting": True, "onlineMeetingProvider": "teamsForBusiness",
         "onlineMeeting": {"joinUrl": LONG}, "responseStatus": {"response": "accepted"},
         "start": {"dateTime": start.strftime("%Y-%m-%dT%H:%M:%S.0000000")},
         "end": {"dateTime": (start + timedelta(minutes=minutes)).strftime("%Y-%m-%dT%H:%M:%S.0000000")}}
    e.update(kw)
    return e


def test_graph_time_with_seven_fraction_digits():
    assert parse_graph_time("2026-11-12T10:00:00.0000000") == T0


def test_event_filtering():
    events = [event("ok"), event("cancelled", isCancelled=True), event("phys", isOnlineMeeting=False),
              event("declined", responseStatus={"response": "declined"}),
              event("skype", onlineMeetingProvider="skypeForBusiness"),
              event("nourl", onlineMeeting=None)]
    assert [m.key.split("|")[0] for m in meetings_from_events(events)] == ["ok"]


def test_long_url_parses_to_chat_and_organizer_info():
    t = parse_join_url(LONG)
    assert t.tenant_id == "tenant-1"
    assert t.chat_info["threadId"] == "19:meeting_abc@thread.v2" and t.chat_info["messageId"] == "0"
    assert t.meeting_info["@odata.type"] == "#microsoft.graph.organizerMeetingInfo"
    assert t.meeting_info["organizer"]["user"]["id"] == "organizer-1"


def test_short_url_keeps_passcode_raw_and_needs_tenant():
    t = parse_join_url(SHORT, "tenant-9")
    assert t.meeting_info == {"@odata.type": "#microsoft.graph.joinMeetingIdMeetingInfo",
                              "joinMeetingId": "123456789", "passcode": "ab+c%26d"}
    assert t.chat_info is None and t.tenant_id == "tenant-9"
    with pytest.raises(ValueError):
        parse_join_url(SHORT)
    with pytest.raises(ValueError):
        parse_join_url("https://example.com/not-teams")


def test_join_request_shape():
    body = build_join_request(parse_join_url(LONG), "https://bot.example/api/calling")
    assert body["callbackUri"] == "https://bot.example/api/calling"
    assert body["mediaConfig"]["@odata.type"] == "#microsoft.graph.serviceHostedMediaConfig"
    assert body["requestedModalities"] == ["audio"] and body["tenantId"] == "tenant-1" and "chatInfo" in body


class FakeGraph:
    def __init__(self, events):
        self.events, self.joined, self.deleted, self.fail = events, [], [], 0

    def calendar_view(self, mailbox, start, end):
        return [e for e in self.events]

    def join_call(self, body):
        if self.fail:
            self.fail -= 1
            raise RuntimeError("boom")
        self.joined.append(body)
        return {"id": f"call-{len(self.joined)}"}

    def delete_call(self, call_id):
        self.deleted.append(call_id)


def scheduler(tmp_path, graph, clock, **kw):
    s = CalendarSettings(agent_mailbox="agent@x.com", callback_url="https://bot/api/calling",
                         tenant_id=kw.pop("tenant_id", "tenant-1"), state_file=str(tmp_path / "state.json"), **kw)
    return CalendarScheduler(s, graph, StateStore(s.state_file), now=lambda: clock[0])


def test_joins_once_at_lead_time_and_leaves_after_end(tmp_path):
    g, clock = FakeGraph([event()]), [T0 - timedelta(minutes=5)]
    sch = scheduler(tmp_path, g, clock)
    sch.tick()
    assert g.joined == []                       # too early
    clock[0] = T0 - timedelta(seconds=30)       # inside the 60 s lead
    sch.tick(); sch.tick()
    assert len(g.joined) == 1                   # not rejoined on the next tick
    clock[0] = T0 + timedelta(minutes=30, seconds=10)
    sch.tick()
    assert g.deleted == []
    clock[0] = T0 + timedelta(minutes=36)
    sch.tick()
    assert g.deleted == ["call-1"]
    assert StateStore(str(tmp_path / "state.json")).data[next(iter(sch.store.data))]["status"] == "left"


def test_restart_does_not_rejoin(tmp_path):
    g, clock = FakeGraph([event()]), [T0]
    scheduler(tmp_path, g, clock).tick()
    scheduler(tmp_path, g, clock).tick()        # new scheduler, same state file
    assert len(g.joined) == 1


def test_failed_join_retries_up_to_the_limit(tmp_path):
    g, clock = FakeGraph([event()]), [T0]
    g.fail = 10
    sch = scheduler(tmp_path, g, clock, max_join_attempts=3)
    for _ in range(6):
        sch.tick()
    assert next(iter(sch.store.data.values()))["attempts"] == 3 and g.joined == []


def test_notification_registry():
    r = CallRegistry()
    r.update({"value": [{"resourceUrl": "/communications/calls/abc", "resourceData": {"state": "established"}}]})
    assert r.states == {"abc": "established"}


def test_graph_client_paging_headers_and_errors():
    import io
    import urllib.error
    from types import SimpleNamespace as NS
    from teams_voice_meeting.calendar_join.graph import GraphClient, GraphError

    seen = []
    pages = {"1": {"value": [{"id": "a"}], "@odata.nextLink": "https://graph.microsoft.com/next"},
             "2": {"value": [{"id": "b"}]}}

    class Resp:
        def __init__(self, data): self.data = json.dumps(data).encode()
        def read(self): return self.data
        def __enter__(self): return self
        def __exit__(self, *a): pass

    def opener(req, timeout):
        seen.append(req)
        return Resp(pages["2" if req.full_url.endswith("/next") else "1"])

    g = GraphClient(credential=NS(get_token=lambda scope: NS(token="tok")), opener=opener)
    assert [e["id"] for e in g.calendar_view("agent@x.com", T0, T0 + timedelta(hours=1))] == ["a", "b"]
    assert "users/agent%40x.com/calendarView?startDateTime=2026-11-12T10:00:00Z" in seen[0].full_url
    assert seen[0].get_header("Prefer") == 'outlook.timezone="UTC"' and seen[0].get_header("Authorization") == "Bearer tok"

    def failing(req, timeout):
        raise urllib.error.HTTPError(req.full_url, 403, "no", {}, io.BytesIO(b"denied"))
    with pytest.raises(GraphError, match="403"):
        GraphClient(credential=NS(get_token=lambda s: NS(token="t")), opener=failing).join_call({})


def test_modes_do_not_import_each_other():
    import pathlib, re
    pkg = pathlib.Path(__file__).parent.parent / "src" / "teams_voice_meeting"
    for f in (pkg / "calendar_join").glob("*.py"):
        assert not re.search(r"^\s*(from|import)\s.*(foundry|consent|azure\.ai|\.\.config)", f.read_text(), re.M), f
    for name in ("foundry.py", "consent.py", "config.py"):
        assert not re.search(r"^\s*(from|import)\s.*calendar_join", (pkg / name).read_text(), re.M), name


def test_run_rejects_unknown_join_mode(monkeypatch):
    from teams_voice_meeting.cli import main
    monkeypatch.setenv("JOIN_MODE", "both")
    with pytest.raises(SystemExit, match="JOIN_MODE"):
        main(["run"])


def test_calendar_outage_still_leaves_finished_calls(tmp_path):
    g, clock = FakeGraph([event()]), [T0]
    sch = scheduler(tmp_path, g, clock)
    sch.tick()                                   # joined
    def down(*a): raise RuntimeError("Graph 503")
    g.calendar_view = down
    clock[0] = T0 + timedelta(hours=2)
    sch.tick()                                   # must not raise, must leave
    assert g.deleted == ["call-1"]


def test_meeting_from_another_tenant_is_skipped_unless_allowed(tmp_path):
    g, clock = FakeGraph([event()]), [T0]
    scheduler(tmp_path, g, clock, tenant_id="our-tenant").tick()
    assert g.joined == []
    other = tmp_path / "allowed"
    other.mkdir()
    scheduler(other, g, clock, tenant_id="our-tenant", allow_external_meetings=True).tick()
    assert len(g.joined) == 1


def test_callback_bad_content_length_is_400():
    import http.client, threading
    from teams_voice_meeting.calendar_join.callback import make_server
    srv = make_server("127.0.0.1", 0, CallRegistry())
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    try:
        c = http.client.HTTPConnection("127.0.0.1", srv.server_address[1], timeout=3)
        c.putrequest("POST", "/api/calling"); c.putheader("Content-Length", "abc"); c.endheaders()
        assert c.getresponse().status == 400
    finally:
        srv.shutdown()


def test_env_example_can_be_sourced_by_a_shell():
    import pathlib, shlex
    text = (pathlib.Path(__file__).parent.parent / ".env.example").read_text()
    for line in text.splitlines():
        if line.strip() and not line.startswith("#"):
            assert len(shlex.split(line)) == 1, f"value needs quotes: {line}"
