import asyncio
import logging
from collections.abc import Awaitable, Callable
from contextlib import suppress
from functools import partial
from typing import Protocol

import simplejson
from redis.asyncio import Redis
from redis.exceptions import RedisError
from sqlalchemy.exc import TimeoutError as PoolTimeoutError

from app.listing_cache import CACHE_VERSION_KEY

logger = logging.getLogger(__name__)


class OutboxStore(Protocol):
    async def publish_next(
        self,
        publish: Callable[[dict[str, object]], Awaitable[None]],
    ) -> bool: ...


async def publish_listing_event(redis: Redis, payload: dict[str, object]) -> None:
    data = simplejson.dumps(payload, use_decimal=True, ensure_ascii=False)
    await redis.xadd("listings.new", {"data": data})


async def invalidate_listings_cache(redis: Redis) -> None:
    await redis.incr(CACHE_VERSION_KEY)


async def run_outbox_worker(
    store: OutboxStore,
    redis: Redis,
    stop: asyncio.Event,
    *,
    poll_interval: float = 1.0,
    invalidate_next: Callable[[Callable[[], Awaitable[None]]], Awaitable[bool]]
    | None = None,
) -> None:
    publish = partial(publish_listing_event, redis)
    operations: list[Callable[[], Awaitable[bool]]] = [
        partial(store.publish_next, publish),
    ]

    if invalidate_next is not None:
        invalidate = partial(invalidate_listings_cache, redis)
        operations.append(partial(invalidate_next, invalidate))

    while not stop.is_set():
        processed_any = False
        had_error = False

        for process_next in operations:
            try:
                processed = await process_next()
            except RedisError as exc:
                had_error = True
                logger.warning("Outbox Redis operation failed: %s", exc)
            except (OSError, PoolTimeoutError) as exc:
                had_error = True
                logger.warning("Outbox database connection unavailable: %s", exc)
            except Exception:
                had_error = True
                logger.exception("Outbox operation failed; will retry")
            else:
                processed_any = processed_any or processed

        if processed_any and not had_error:
            continue

        with suppress(TimeoutError):
            await asyncio.wait_for(stop.wait(), timeout=poll_interval)
