import asyncio
from decimal import Decimal
from unittest.mock import AsyncMock

import app.in_memory_listing_store as store_module
import pytest
from app.in_memory_listing_store import InMemoryListingStore
from app.listing_filters import ListingFilters
from app.listing_models import ListingCreate
from app.outbox import run_outbox_worker
from redis.exceptions import ConnectionError as RedisConnectionError


@pytest.fixture
def anyio_backend():
    return "asyncio"


@pytest.fixture
def listing():
    return ListingCreate(
        source="mock",
        external_id="seed-123",
        url="https://example.com/listing/123",
        deal_type="rent",
        country="BY",
        city="Minsk",
        price=Decimal("380.25"),
        currency="USD",
        price_usd=380,
        rooms=0,
        photos=[
            "https://example.com/second.jpg",
            "https://example.com/first.jpg",
        ],
    )


@pytest.mark.anyio
async def test_create_saves_listing_and_event(listing):
    store = InMemoryListingStore()

    result = await store.create_or_get(listing)
    saved = await store.get(result.listing.id)
    events = await store.outbox_snapshot()

    assert result.created is True
    assert saved == result.listing
    assert saved.id > 0
    assert saved.created_at.utcoffset().total_seconds() == 0
    assert saved.photos == listing.photos

    assert len(events) == 1
    event = events[0]
    assert event.kind == "listing_new"
    assert event.listing_id == saved.id
    assert event.published_at is None
    assert event.payload == {
        "event_id": str(event.event_id),
        "id": saved.id,
        "source": "mock",
        "deal_type": "rent",
        "country": "BY",
        "city": "Minsk",
        "rooms": 0,
        "price": Decimal("380.25"),
        "currency": "USD",
        "price_usd": 380,
        "address": None,
        "url": "https://example.com/listing/123",
        "created_at": saved.created_at.isoformat(),
    }


@pytest.mark.anyio
async def test_duplicate_preserves_original_listing_and_event(listing):
    store = InMemoryListingStore()
    first = await store.create_or_get(listing)
    original_events = await store.outbox_snapshot()

    changed = listing.model_copy(
        update={
            "price": Decimal("999.00"),
            "price_usd": 999,
            "photos": [],
        }
    )
    duplicate = await store.create_or_get(changed)

    assert duplicate.created is False
    assert duplicate.listing == first.listing
    assert await store.get(first.listing.id) == first.listing
    assert await store.outbox_snapshot() == original_events


@pytest.mark.anyio
async def test_same_external_id_from_different_sources_is_not_duplicate(listing):
    store = InMemoryListingStore()

    first = await store.create_or_get(listing)
    second = await store.create_or_get(listing.model_copy(update={"source": "another"}))

    assert first.created is True
    assert second.created is True
    assert first.listing.id != second.listing.id
    assert len(await store.outbox_snapshot()) == 2


@pytest.mark.anyio
async def test_concurrent_duplicates_create_one_listing_and_event(listing):
    store = InMemoryListingStore()

    results = await asyncio.gather(*(store.create_or_get(listing) for _ in range(20)))

    assert sum(result.created for result in results) == 1
    assert len({result.listing.id for result in results}) == 1
    assert len(await store.outbox_snapshot()) == 1


@pytest.mark.anyio
async def test_mutating_input_and_results_does_not_change_storage(listing):
    store = InMemoryListingStore()
    original = listing.model_copy(deep=True)

    result = await store.create_or_get(listing)
    listing.photos.clear()
    result.listing.photos.clear()

    saved = await store.get(result.listing.id)
    assert saved.photos == original.photos

    saved.photos.clear()
    reread = await store.get(result.listing.id)
    assert reread.photos == original.photos

    events = await store.outbox_snapshot()
    events[0].payload["city"] = "Changed"

    reread_events = await store.outbox_snapshot()
    assert reread_events[0].payload["city"] == "Minsk"


@pytest.mark.anyio
async def test_event_build_failure_does_not_save_listing(listing, monkeypatch):
    store = InMemoryListingStore()

    def fail_to_build_event(_listing):
        raise RuntimeError("Не удалось подготовить событие")

    with monkeypatch.context() as patch:
        patch.setattr(store_module, "build_listing_event", fail_to_build_event)

        with pytest.raises(RuntimeError, match="подготовить событие"):
            await store.create_or_get(listing)

    assert await store.outbox_snapshot() == []

    # Повтор должен создать запись, а не обнаружить частично сохранённый дубль.
    retry = await store.create_or_get(listing)
    assert retry.created is True
    assert retry.listing.id == 1
    assert len(await store.outbox_snapshot()) == 1


@pytest.mark.anyio
async def test_get_unknown_listing_returns_none():
    store = InMemoryListingStore()

    assert await store.get(999) is None


@pytest.mark.anyio
async def test_search_filters_then_sorts_and_paginates(listing):
    store = InMemoryListingStore()

    first = await store.create_or_get(listing)
    second = await store.create_or_get(
        listing.model_copy(update={"external_id": "second-studio"})
    )
    await store.create_or_get(
        listing.model_copy(update={"external_id": "one-room", "rooms": 1})
    )

    filters = ListingFilters(rooms=0, price_max=380, sort="price_desc", size=1)

    page_one = await store.search(filters)
    page_two = await store.search(filters.model_copy(update={"page": 2}))
    page_three = await store.search(filters.model_copy(update={"page": 3}))

    assert page_one.total == page_two.total == page_three.total == 2
    assert [item.id for item in page_one.items] == [second.listing.id]
    assert [item.id for item in page_two.items] == [first.listing.id]
    assert page_three.items == []


@pytest.mark.anyio
async def test_failed_publication_stays_pending_until_successful_retry(listing):
    store = InMemoryListingStore()
    await store.create_or_get(listing)
    [original] = await store.outbox_snapshot()

    publish = AsyncMock(side_effect=[RedisConnectionError("Redis недоступен"), None])

    with pytest.raises(RedisConnectionError):
        await store.publish_next(publish)

    [pending] = await store.outbox_snapshot()
    assert pending == original
    assert pending.published_at is None

    assert await store.publish_next(publish) is True

    [published] = await store.outbox_snapshot()
    assert published.event_id == original.event_id
    assert published.published_at is not None

    assert await store.publish_next(publish) is False
    assert publish.await_count == 2
    first, retry = publish.await_args_list
    assert first == retry
    assert first.args == (original.payload,)


@pytest.mark.anyio
async def test_worker_retries_redis_failure_and_marks_event_published(listing):
    store = InMemoryListingStore()
    await store.create_or_get(listing)
    [original] = await store.outbox_snapshot()

    stop = asyncio.Event()
    redis = AsyncMock()

    async def xadd(stream, fields):
        if redis.xadd.await_count == 1:
            raise RedisConnectionError("Redis временно недоступен")

        stop.set()
        return "1-0"

    redis.xadd.side_effect = xadd

    await asyncio.wait_for(
        run_outbox_worker(store, redis, stop, poll_interval=0.01),
        timeout=2,
    )

    first, retry = redis.xadd.await_args_list
    assert retry == first

    [published] = await store.outbox_snapshot()
    assert published.event_id == original.event_id
    assert published.published_at is not None
