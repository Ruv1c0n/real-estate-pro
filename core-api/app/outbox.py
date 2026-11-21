import asyncio
import logging
from collections.abc import Awaitable, Callable
from contextlib import suppress
from functools import partial
from typing import Protocol

import simplejson
from redis.asyncio import Redis
from redis.exceptions import RedisError

logger = logging.getLogger(__name__)


class OutboxStore(Protocol):
    async def publish_next(
        self,
        publish: Callable[[dict[str, object]], Awaitable[None]],
    ) -> bool: ...


async def publish_listing_event(redis: Redis, payload: dict[str, object]) -> None:
    data = simplejson.dumps(payload, use_decimal=True, ensure_ascii=False)
    await redis.xadd("listings.new", {"data": data})


async def run_outbox_worker(
    store: OutboxStore,
    redis: Redis,
    stop: asyncio.Event,
    *,
    poll_interval: float = 1.0,
) -> None:
    publish = partial(publish_listing_event, redis)

    while not stop.is_set():
        try:
            if await store.publish_next(publish):
                continue
        except RedisError as exc:
            logger.warning("Failed to publish outbox event to Redis: %s", exc)

        with suppress(TimeoutError):
            await asyncio.wait_for(stop.wait(), timeout=poll_interval)
