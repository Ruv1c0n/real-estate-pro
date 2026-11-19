import asyncio
import json
from decimal import Decimal
from unittest.mock import AsyncMock

import pytest
from app.outbox import publish_listing_event, run_outbox_worker
from redis.exceptions import ConnectionError as RedisConnectionError


@pytest.fixture
def anyio_backend():
    return "asyncio"


@pytest.fixture
def payload():
    return {
        "event_id": "550e8400-e29b-41d4-a716-446655440000",
        "id": 123,
        "source": "mock",
        "deal_type": "rent",
        "country": "BY",
        "city": "Minsk",
        "rooms": 1,
        "price": Decimal("9007199254740991.25"),
        "currency": "USD",
        "price_usd": 9007199254740991,
        "address": "ул. Примерная, 1",
        "url": "https://example.com/listing/123",
        "created_at": "2026-11-19T10:00:00Z",
    }


@pytest.mark.anyio
async def test_publish_listing_event_sends_payload_to_stream(payload):
    redis = AsyncMock()

    await publish_listing_event(redis, payload)

    redis.xadd.assert_awaited_once()
    stream, fields = redis.xadd.await_args.args
    assert stream == "listings.new"
    assert set(fields) == {"data"}
    assert isinstance(fields["data"], str)
    assert json.loads(fields["data"], parse_float=Decimal) == payload


@pytest.mark.anyio
async def test_publish_listing_event_propagates_error_and_allows_retry(payload):
    redis = AsyncMock()
    redis.xadd.side_effect = [RedisConnectionError("Redis недоступен"), "1-0"]

    with pytest.raises(RedisConnectionError):
        await publish_listing_event(redis, payload)

    await publish_listing_event(redis, payload)

    first, retry = redis.xadd.await_args_list
    assert retry == first
    stream, fields = first.args
    assert stream == "listings.new"
    assert json.loads(fields["data"])["event_id"] == payload["event_id"]


@pytest.mark.anyio
async def test_worker_retries_after_database_connection_refused():
    stop = asyncio.Event()
    store = AsyncMock()
    redis = AsyncMock()

    async def publish_next(publish):
        if store.publish_next.await_count == 1:
            raise ConnectionRefusedError("PostgreSQL временно недоступен")
        stop.set()
        return False

    store.publish_next.side_effect = publish_next

    await asyncio.wait_for(
        run_outbox_worker(store, redis, stop, poll_interval=0.01),
        timeout=2,
    )

    assert store.publish_next.await_count == 2
    redis.xadd.assert_not_awaited()
