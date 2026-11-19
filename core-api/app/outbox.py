import asyncio
import logging
from collections.abc import Awaitable, Callable
from contextlib import suppress
from functools import partial
from typing import Protocol

import simplejson
from redis.asyncio import Redis
from redis.exceptions import RedisError
from sqlalchemy.exc import DBAPIError
from sqlalchemy.exc import TimeoutError as PoolTimeoutError

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
            logger.warning("Не удалось опубликовать событие в Redis: %s", exc)
        except (OSError, PoolTimeoutError) as exc:
            logger.warning("Outbox: PostgreSQL недоступен (%s)", type(exc).__name__)
        except DBAPIError as exc:
            sqlstate = getattr(exc.orig, "sqlstate", "") or ""
            if not (
                exc.connection_invalidated
                or sqlstate.startswith("08")
                or sqlstate in {"53300", "57P01", "57P02", "57P03"}
            ):
                raise
            logger.warning("Outbox: соединение с PostgreSQL прервано (%s)", sqlstate)

        with suppress(TimeoutError):
            await asyncio.wait_for(stop.wait(), timeout=poll_interval)
