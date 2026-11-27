import hashlib
import json
from collections.abc import Awaitable, Callable
from contextlib import suppress
from decimal import Decimal

from redis.asyncio import Redis
from redis.exceptions import RedisError

from app.listing_filters import ListingFilters

CACHE_VERSION_KEY = "cache:version"


def _decimal_for_key(value: Decimal) -> str:
    if value.is_zero():
        return "0"

    sign, digits, exponent = value.as_tuple()
    digits_text = "".join(str(digit) for digit in digits)
    significant = digits_text.rstrip("0")
    exponent += len(digits_text) - len(significant)
    return f"{'-' if sign else ''}{significant}e{exponent}"


def listing_cache_key(filters: ListingFilters, version: int) -> str:
    values = filters.model_dump(exclude_none=True)
    normalized = {
        name: _decimal_for_key(value) if isinstance(value, Decimal) else value
        for name, value in values.items()
    }
    payload = json.dumps(
        normalized, sort_keys=True, separators=(",", ":"), ensure_ascii=False
    )
    digest = hashlib.sha256(payload.encode("utf-8")).hexdigest()
    return f"cache:listings:v{version}:{digest}"


class ListingPageCache:
    def __init__(
        self,
        redis: Redis,
        ttl_seconds: int,
        has_pending_invalidation: Callable[[], Awaitable[bool]],
    ) -> None:
        self._redis = redis
        self._ttl_seconds = ttl_seconds
        self._has_pending_invalidation = has_pending_invalidation

    async def get_or_load(
        self, filters: ListingFilters, load: Callable[[], Awaitable[str]]
    ) -> str:
        if await self._has_pending_invalidation():
            return await load()
        key = None
        try:
            version = await self._redis.get(CACHE_VERSION_KEY)
            if version is None:
                initialized = await self._redis.set(CACHE_VERSION_KEY, "0", nx=True)
                version = (
                    "0" if initialized else await self._redis.get(CACHE_VERSION_KEY)
                )
            if version is not None:
                key = listing_cache_key(filters, int(version))
                cached = await self._redis.get(key)
                if cached is not None:
                    return cached
        except (RedisError, ValueError):
            key = None

        body = await load()
        if key is not None:
            with suppress(RedisError):
                await self._redis.set(key, body, ex=self._ttl_seconds)
        return body
