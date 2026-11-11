from types import SimpleNamespace as NS

import pytest
from fastapi.testclient import TestClient

from teams_voice_meeting.app import create_app
from teams_voice_meeting.config import Settings, normalize_resource_account_id
from teams_voice_meeting.events import parse_incoming_call, validation_code
from teams_voice_meeting.foundry import FoundryVoice

GUID = "123e4567-e89b-12d3-a456-426614174000"


def settings(**kw):
    base = dict(project_endpoint="https://x", agent_name="agent", connection_name="acs",
                resource_account_object_id=GUID, monitor_poll_seconds=0.01, max_call_seconds=5)
    base.update(kw)
    return Settings(**base)


class FakeTelephony:
    def __init__(self, bindings=(), calls=()):
        self.bindings, self.calls, self.created, self.ended = list(bindings), list(calls), [], []
        self.statuses = {}

    def list_bindings(self, agent, provider=None):
        return self.bindings

    def create_binding(self, agent, req):
        self.created.append(req)
        return NS(id="b1", incoming_call_url="https://foundry/incoming", status="active")

    def list_calls(self, agent, provider=None, status=None):
        return [c for c in self.calls if c.status == status]

    def get_call(self, agent, call_id):
        c = next(c for c in self.calls if c.id == call_id)
        c.status = self.statuses.get(call_id, c.status)
        return c

    def end_call(self, agent, call_id):
        self.ended.append(call_id)
        c = next(c for c in self.calls if c.id == call_id)
        c.status = "success"
        return c


def voice(tel, **kw):
    return FoundryVoice(settings(**kw), client=NS(beta=NS(voice_agents=NS(telephony=tel))))


def test_guid_prefix_removed():
    assert normalize_resource_account_id(f"28:orgid:{GUID.upper()}") == GUID
    with pytest.raises(ValueError):
        normalize_resource_account_id("not-a-guid")


def test_event_parsing():
    assert validation_code({"eventType": "Microsoft.EventGrid.SubscriptionValidationEvent",
                            "data": {"validationCode": "abc"}}) == "abc"
    info = parse_incoming_call({"data": {"correlationId": "c", "serverCallId": "s",
                                         "from": {"rawId": "8:orgid:x"}, "incomingCallContext": "ctx"}})
    assert (info.correlation_id, info.server_call_id, info.caller) == ("c", "s", "8:orgid:x")


def test_ensure_binding_creates_then_reuses():
    tel = FakeTelephony()
    b = voice(tel).ensure_binding()
    assert b.incoming_call_url == "https://foundry/incoming"
    assert tel.created[0].resource_account_object_id == GUID
    tel2 = FakeTelephony(bindings=[NS(id="b9", incoming_call_url="u", status="active",
                                      resource_account_object_id=GUID)])
    assert voice(tel2).ensure_binding().id == "b9" and not tel2.created


def test_wait_for_end_and_time_limit():
    call = NS(id="c1", status="in_progress", started_at=10)
    tel = FakeTelephony(calls=[call])
    tel.statuses["c1"] = "success"
    assert str(voice(tel).wait_for_end("c1", 0.01, 5).status) == "success"
    call2 = NS(id="c2", status="in_progress", started_at=10)
    tel2 = FakeTelephony(calls=[call2])
    voice(tel2).wait_for_end("c2", 0.01, 0.02)  # exceeds limit, is ended
    assert tel2.ended == ["c2"]


def test_app_handshake_and_incoming_call_monitoring():
    call = NS(id="c1", status="in_progress", started_at=2**40)
    tel = FakeTelephony(calls=[call])
    tel.statuses["c1"] = "success"
    with TestClient(create_app(settings(), voice(tel))) as client:
        r = client.post("/api/events", json=[{"eventType": "Microsoft.EventGrid.SubscriptionValidationEvent",
                                              "data": {"validationCode": "v"}}])
        assert r.json() == {"validationResponse": "v"}
        r = client.post("/api/events", json=[{"eventType": "Microsoft.Communication.IncomingCall",
                                              "data": {"correlationId": "corr"}}])
        assert r.status_code == 200
        for _ in range(100):
            if client.get("/api/calls").json()["calls"].get("c1", {}).get("status") == "success":
                break
            import time; time.sleep(0.05)
        assert client.get("/api/calls").json()["calls"]["c1"]["correlation_id"] == "corr"
        assert client.post("/api/events", content="nope").status_code == 400
