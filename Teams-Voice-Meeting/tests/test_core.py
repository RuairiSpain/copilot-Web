import base64
import hashlib
import hmac
from datetime import datetime, timezone
from types import SimpleNamespace as NS

import pytest
from azure.ai.projects import models as m
from azure.core.exceptions import ResourceNotFoundError

from teams_voice_meeting.config import Settings, normalize_resource_account_id
from teams_voice_meeting.consent import sign_request
from teams_voice_meeting.foundry import FoundryVoice, val

GUID = "123e4567-e89b-12d3-a456-426614174000"


def settings(**kw):
    base = dict(project_endpoint="https://x", agent_name="agent", connection_name="acs",
                resource_account_object_id=GUID, max_call_seconds=0)
    base.update(kw)
    return Settings(**base)


def real_call(cid, status="in_progress", reason=None):
    """A call record deserialized by the SDK, so status is a real enum and started_at a datetime."""
    return m.TelephonyCallRecord({"id": cid, "provider": "teams_phone_extensibility", "status": status,
                                  "phase": "answered", "started_at": 1760000000, "end_reason": reason})


class FakeTelephony:
    def __init__(self, bindings=(), calls=()):
        self.bindings, self.calls, self.created, self.ended = list(bindings), {c.id: c for c in calls}, [], []
        self.list_kwargs = []

    def list_bindings(self, agent, provider=None):
        return self.bindings

    def create_binding(self, agent, req):
        self.created.append(req)
        return NS(id="b1", incoming_call_url="https://foundry/incoming", status=m.TelephonyBindingStatus.ACTIVE)

    def list_calls(self, agent, provider=None, status=None):
        self.list_kwargs.append(status)
        return [c for c in self.calls.values() if status is None or val(c.status) == status]

    def get_call(self, agent, call_id):
        return self.calls[call_id]

    def end_call(self, agent, call_id):
        self.ended.append(call_id)
        self.calls[call_id] = real_call(call_id, "success", "agent_ended")
        return self.calls[call_id]


class FakeAgents:
    def __init__(self, exists):
        self.exists, self.created = exists, []

    def get(self, name):
        if not self.exists:
            raise ResourceNotFoundError("nope")

    def create_version(self, agent_name, definition):
        self.created.append((agent_name, definition))


def voice(tel, agents=None, **kw):
    client = NS(beta=NS(voice_agents=NS(telephony=tel)), agents=agents or FakeAgents(True))
    return FoundryVoice(settings(**kw), client=client)


def test_guid_prefix_removed():
    assert normalize_resource_account_id(f"28:orgid:{GUID.upper()}") == GUID
    with pytest.raises(ValueError):
        normalize_resource_account_id("not-a-guid")


def test_sdk_enum_str_is_not_the_wire_value():
    # Regression: str(call.status) is 'TelephonyCallStatus.IN_PROGRESS', which broke status comparisons.
    assert str(real_call("c").status) != "in_progress"
    assert val(real_call("c").status) == "in_progress"


def test_ensure_binding_creates_then_reuses():
    tel = FakeTelephony()
    b = voice(tel).ensure_binding()
    assert (b.incoming_call_url, b.status) == ("https://foundry/incoming", "active")
    assert tel.created[0].resource_account_object_id == GUID
    existing = m.TeamsPhoneExtensibilityTelephonyBindingListItem(
        {"id": "b9", "provider": "teams_phone_extensibility", "connection_name": "acs", "status": "active",
         "incoming_call_url": "u", "resource_account_object_id": GUID})
    tel2 = FakeTelephony(bindings=[existing])
    assert voice(tel2).ensure_binding().id == "b9" and not tel2.created


def test_ensure_agent_creates_voice_definition_only_when_missing():
    agents = FakeAgents(exists=False)
    assert voice(FakeTelephony(), agents).ensure_agent() == "created"
    name, definition = agents.created[0]
    assert name == "agent" and definition.kind == "voice" and definition.model_type == "managed"
    assert voice(FakeTelephony(), FakeAgents(True)).ensure_agent() == "exists"


def test_watch_logs_start_and_end(caplog):
    tel = FakeTelephony(calls=[real_call("c1")])
    v = voice(tel)

    def finish_call(_):
        tel.calls["c1"] = real_call("c1", "success", "caller_hangup")

    with caplog.at_level("INFO"):
        v.watch(iterations=2, sleep=finish_call)
    assert "call started c1" in caplog.text
    assert "call ended c1 status=success" in caplog.text


def test_watch_ends_call_over_limit():
    tel = FakeTelephony(calls=[real_call("c1")])
    v = voice(tel, max_call_seconds=60)
    clock = iter([0, 100, 100])  # start at t=0, then 100s later
    v.watch(iterations=2, sleep=lambda _: None, clock=lambda: next(clock))
    assert tel.ended == ["c1"]


def test_consent_signature_matches_acs_spec():
    key = base64.b64encode(b"secret-key-bytes").decode()
    now = datetime(2026, 1, 2, 3, 4, 5, tzinfo=timezone.utc)
    body = b'{"principalType":"teamsResourceAccount"}'
    h = sign_request("x.communication.azure.com", key, "/p?api-version=1", body, now)
    to_sign = "PUT\n/p?api-version=1\nFri, 02 Jan 2026 03:04:05 GMT;x.communication.azure.com;" + \
              base64.b64encode(hashlib.sha256(body).digest()).decode()
    sig = base64.b64encode(hmac.new(b"secret-key-bytes", to_sign.encode(), hashlib.sha256).digest()).decode()
    assert h["Authorization"].endswith("Signature=" + sig)
    assert h["x-ms-date"] == "Fri, 02 Jan 2026 03:04:05 GMT"


def test_wake_and_sleep_words_in_instructions():
    from teams_voice_meeting.config import build_instructions
    text = build_instructions("BASE", ("hey bob", "ok bob"), ("bob stop",))
    assert text.startswith("BASE") and '"hey bob" or "ok bob"' in text and '"bob stop"' in text
    assert "ASLEEP" in text and "AWAKE" in text
    assert build_instructions("BASE", (), ("x",)) == "BASE"   # no wake words: trigger mode off


def test_settings_wake_words_from_env(monkeypatch):
    for k, v in dict(FOUNDRY_PROJECT_ENDPOINT="https://x", VOICE_AGENT_NAME="a", ACS_CONNECTION_NAME="c",
                     RESOURCE_ACCOUNT_OBJECT_ID=GUID, WAKE_WORDS="hey bob, ok bob", SLEEP_WORDS="bob stop").items():
        monkeypatch.setenv(k, v)
    s = Settings.from_env()
    assert s.wake_words == ("hey bob", "ok bob") and s.sleep_words == ("bob stop",)
    monkeypatch.setenv("WAKE_WORDS", "")
    assert Settings.from_env().wake_words == () and Settings.from_env().agent_instructions() == s.instructions
    monkeypatch.delenv("WAKE_WORDS")
    assert Settings.from_env().wake_words == ("hey assistant",)


def test_ensure_agent_update_publishes_new_version_with_triggers():
    agents = FakeAgents(exists=True)
    v = voice(FakeTelephony(), agents)
    assert v.ensure_agent() == "exists" and not agents.created
    assert v.ensure_agent(update=True) == "updated"
    definition = agents.created[0][1]
    assert "hey assistant" in definition.instructions and not definition.get("greeting")
