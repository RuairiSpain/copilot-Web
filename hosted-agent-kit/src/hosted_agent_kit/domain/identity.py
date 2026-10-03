"""Identifiers the kit accepts for users, conversations and subjects."""

from __future__ import annotations

import re

_USER_ID_PATTERN = re.compile(r"^[A-Za-z0-9._:-]{1,128}$")
_KEY_PATTERN = re.compile(r"^[A-Za-z0-9._:-]{1,128}$")


def is_valid_user_id(value: str) -> bool:
    return _USER_ID_PATTERN.match(value) is not None


def is_valid_conversation_key(value: str) -> bool:
    return _KEY_PATTERN.match(value) is not None
