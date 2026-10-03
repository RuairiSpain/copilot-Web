"""``QuotaLedger`` on Redis. Each operation is one atomic script, and time comes from Redis."""

from __future__ import annotations

from collections.abc import Sequence
from typing import Any

from hosted_agent_kit.ports.quota import RegionalSnapshot

_NOW = "local t = redis.call('TIME'); local now = t[1] * 1000 + math.floor(t[2] / 1000)\n"

_ACQUIRE = (
    _NOW
    + """
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[2]) then return 0 end
redis.call('ZADD', KEYS[1], now + tonumber(ARGV[3]), ARGV[1])
return 1
"""
)

_HEARTBEAT = (
    _NOW
    + """
local expires = now + tonumber(ARGV[3])
redis.call('ZADD', KEYS[2], expires, ARGV[1])
redis.call('HSET', KEYS[3], ARGV[1], ARGV[2])
for i = 4, #ARGV do
  local member = ARGV[1] .. '|' .. ARGV[i]
  if redis.call('ZSCORE', KEYS[1], member) then redis.call('ZADD', KEYS[1], expires, member) end
end
return 1
"""
)

_FORGET = """
redis.call('ZREM', KEYS[2], ARGV[1])
redis.call('HDEL', KEYS[3], ARGV[1])
for _, member in ipairs(redis.call('ZRANGE', KEYS[1], 0, -1)) do
  if string.sub(member, 1, #ARGV[1] + 1) == ARGV[1] .. '|' then
    redis.call('ZREM', KEYS[1], member)
  end
end
return 1
"""

_SNAPSHOT = (
    _NOW
    + """
for _, kit in ipairs(redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', now)) do
  redis.call('ZREM', KEYS[2], kit)
  redis.call('HDEL', KEYS[3], kit)
end
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
local out = {redis.call('ZCARD', KEYS[1])}
for _, item in ipairs(redis.call('HGETALL', KEYS[3])) do out[#out + 1] = item end
return out
"""
)


class RedisLedger:
    """Keys are ``hack:quota:{<scope>}:permits|kits|counts``, one cluster slot per scope."""

    def __init__(
        self, client: Any, *, scope: str, ttl_seconds: float, prefix: str = "hack:quota"
    ) -> None:
        self._redis = client
        self._ttl_ms = int(ttl_seconds * 1000)
        base = f"{prefix}:{{{scope}}}"
        self._keys = [f"{base}:permits", f"{base}:kits", f"{base}:counts"]
        self._acquire = client.register_script(_ACQUIRE)
        self._heartbeat = client.register_script(_HEARTBEAT)
        self._forget = client.register_script(_FORGET)
        self._snapshot = client.register_script(_SNAPSHOT)

    async def start(self) -> None:
        await self._redis.ping()

    async def close(self) -> None:
        await self._redis.aclose()

    async def acquire_spare(self, kit_id: str, token: str, spare_size: int) -> bool:
        granted = await self._acquire(
            keys=self._keys, args=[f"{kit_id}|{token}", spare_size, self._ttl_ms]
        )
        return bool(granted)

    async def release_spare(self, kit_id: str, token: str) -> None:
        await self._redis.zrem(self._keys[0], f"{kit_id}|{token}")

    async def heartbeat(self, kit_id: str, active: int, tokens: Sequence[str]) -> None:
        await self._heartbeat(keys=self._keys, args=[kit_id, active, self._ttl_ms, *tokens])

    async def forget(self, kit_id: str) -> None:
        await self._forget(keys=self._keys, args=[kit_id])

    async def snapshot(self, spare_size: int, region_limit: int | None) -> RegionalSnapshot:
        raw = await self._snapshot(keys=self._keys, args=[])
        used = int(raw[0])
        pairs = raw[1:]
        kits = {str(pairs[i]): int(pairs[i + 1]) for i in range(0, len(pairs), 2)}
        return RegionalSnapshot(
            region_limit=region_limit,
            total_active=sum(kits.values()),
            spare_size=spare_size,
            spare_used=used,
            kits=kits,
        )
