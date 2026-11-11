"""Environment-based settings."""

from __future__ import annotations

import os
import uuid
from dataclasses import dataclass

ORGID_PREFIX = "28:orgid:"


def normalize_resource_account_id(value: str) -> str:
    """Return the resource account object ID as a bare lowercase GUID (Foundry rejects the MRI prefix)."""
    raw = value.strip()
    if raw.lower().startswith(ORGID_PREFIX):
        raw = raw[len(ORGID_PREFIX):]
    try:
        return str(uuid.UUID(raw))
    except ValueError as exc:
        raise ValueError(f"RESOURCE_ACCOUNT_OBJECT_ID is not a GUID: {value!r}") from exc


def _required(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"Environment variable {name} is required")
    return value


BASE_INSTRUCTIONS = "You are a helpful voice assistant in a Microsoft Teams call. Keep answers short."
DEFAULT_WAKE_WORDS = ("hey assistant",)
DEFAULT_SLEEP_WORDS = ("go quiet",)


def split_words(value: str) -> tuple[str, ...]:
    return tuple(w.strip() for w in value.split(",") if w.strip())


def build_instructions(base: str, wake_words: tuple[str, ...], sleep_words: tuple[str, ...]) -> str:
    """Add wake and sleep word behaviour to the agent instructions.

    This is a prompt, not a gate: the managed realtime model decides whether to speak, so it can
    occasionally answer when it should not. See the README for the deterministic alternative.
    """
    if not wake_words:
        return base
    wake = " or ".join(f'"{w}"' for w in wake_words)
    sleep = " or ".join(f'"{w}"' for w in sleep_words) or "none"
    return f"""{base}

You are on a call with several people, so you only speak when addressed. You have two states.

ASLEEP (the state at the start of every call):
- Do not speak, do not acknowledge anything, and produce no audio.
- Listen only for a start phrase: {wake}. Ignore everything else, including questions, your own name
  and requests that do not include a start phrase.

AWAKE (after you hear a start phrase):
- If the start phrase came with a request, answer it. If not, say only "Yes?".
- Answer what people ask you, briefly. If people are clearly talking to each other, stay quiet.
- When you hear a stop phrase ({sleep}), say only "Okay." once, then become ASLEEP and stay silent until the next start phrase.

Speech transcription can be imperfect. Accept a close phonetic match of a start or stop phrase, but not unrelated words.""".strip()


@dataclass(frozen=True)
class Settings:
    project_endpoint: str
    agent_name: str
    connection_name: str
    resource_account_object_id: str
    phone_number: str | None = None
    model: str = "gpt-realtime"
    instructions: str = BASE_INSTRUCTIONS
    wake_words: tuple[str, ...] = DEFAULT_WAKE_WORDS
    sleep_words: tuple[str, ...] = DEFAULT_SLEEP_WORDS
    max_call_seconds: int = 7200

    def agent_instructions(self) -> str:
        return build_instructions(self.instructions, self.wake_words, self.sleep_words)

    @classmethod
    def from_env(cls) -> "Settings":
        d = cls.__dataclass_fields__
        return cls(
            project_endpoint=_required("FOUNDRY_PROJECT_ENDPOINT"),
            agent_name=_required("VOICE_AGENT_NAME"),
            connection_name=_required("ACS_CONNECTION_NAME"),
            resource_account_object_id=normalize_resource_account_id(_required("RESOURCE_ACCOUNT_OBJECT_ID")),
            phone_number=os.environ.get("TEAMS_PHONE_NUMBER", "").strip() or None,
            model=os.environ.get("VOICE_MODEL") or d["model"].default,
            instructions=os.environ.get("VOICE_INSTRUCTIONS") or BASE_INSTRUCTIONS,
            wake_words=split_words(os.environ["WAKE_WORDS"]) if "WAKE_WORDS" in os.environ else DEFAULT_WAKE_WORDS,
            sleep_words=split_words(os.environ["SLEEP_WORDS"]) if "SLEEP_WORDS" in os.environ else DEFAULT_SLEEP_WORDS,
            max_call_seconds=int(os.environ.get("MAX_CALL_SECONDS", "7200")),
        )
