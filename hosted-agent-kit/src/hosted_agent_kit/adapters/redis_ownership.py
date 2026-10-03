"""``OwnershipStore`` on Redis. Each operation is one atomic script."""

from __future__ import annotations

from typing import Any

from hosted_agent_kit.ports.ownership import Claim

_CLAIM = """
local v = redis.call('GET', KEYS[1])
if v then
  local holder, epoch = string.match(v, '^(.*)|(%d+)$')
  if holder == ARGV[1] then
    redis.call('PEXPIRE', KEYS[1], ARGV[2])
    return {1, holder, tonumber(epoch), ''}
  end
  return {0, holder, tonumber(epoch), ''}
end
local last = redis.call('GET', KEYS[3])
local epoch = tonumber(redis.call('GET', KEYS[2]) or '0')
local previous = ''
if last ~= ARGV[1] then
  epoch = redis.call('INCR', KEYS[2])
  if last then previous = last end
end
redis.call('SET', KEYS[1], ARGV[1] .. '|' .. epoch, 'PX', ARGV[2])
redis.call('SET', KEYS[3], ARGV[1])
return {1, ARGV[1], epoch, previous}
"""

_RENEW = """
if redis.call('GET', KEYS[1]) == ARGV[1] .. '|' .. ARGV[2] then
  redis.call('PEXPIRE', KEYS[1], ARGV[3])
  return 1
end
return 0
"""

_RELEASE = """
if redis.call('GET', KEYS[1]) == ARGV[1] .. '|' .. ARGV[2] then
  redis.call('DEL', KEYS[1])
  if redis.call('GET', KEYS[3]) == ARGV[1] then redis.call('DEL', KEYS[3]) end
  return 1
end
return 0
"""


class RedisOwnershipStore:
    """Keys are ``hack:own:{<key>}:lease|epoch|last``; the braces keep them in one cluster slot."""

    def __init__(self, client: Any, *, prefix: str = "hack:own") -> None:
        self._redis = client
        self._prefix = prefix
        self._claim = client.register_script(_CLAIM)
        self._renew = client.register_script(_RENEW)
        self._release = client.register_script(_RELEASE)

    def _keys(self, key: str) -> list[str]:
        base = f"{self._prefix}:{{{key}}}"
        return [f"{base}:lease", f"{base}:epoch", f"{base}:last"]

    async def start(self) -> None:
        await self._redis.ping()

    async def close(self) -> None:
        await self._redis.aclose()

    async def claim(self, key: str, holder: str, ttl_seconds: float) -> Claim:
        _check_holder(holder)
        granted, who, epoch, previous = await self._claim(
            keys=self._keys(key), args=[holder, int(ttl_seconds * 1000)]
        )
        return Claim(bool(granted), who, int(epoch), previous or None)

    async def renew(self, key: str, holder: str, epoch: int, ttl_seconds: float) -> bool:
        _check_holder(holder)
        result = await self._renew(
            keys=self._keys(key), args=[holder, epoch, int(ttl_seconds * 1000)]
        )
        return bool(result)

    async def release(self, key: str, holder: str, epoch: int) -> None:
        _check_holder(holder)
        await self._release(keys=self._keys(key), args=[holder, epoch])

    async def holder(self, key: str) -> str | None:
        value = await self._redis.get(self._keys(key)[0])
        if not value:
            return None
        return str(value).rsplit("|", 1)[0]


def _check_holder(holder: str) -> None:
    if "|" in holder:
        raise ValueError("a lease holder id cannot contain '|'")
