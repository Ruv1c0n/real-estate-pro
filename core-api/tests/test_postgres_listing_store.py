import asyncio
import json
import os
import subprocess
import sys
from contextlib import asynccontextmanager
from datetime import UTC, datetime
from decimal import Decimal
from functools import partial
from pathlib import Path
from unittest.mock import AsyncMock
from uuid import uuid4

import asyncpg
import pytest
from app.database import create_db_engine
from app.listing_filters import ListingFilters
from app.listing_models import ListingCreate
from app.outbox import publish_listing_event
from app.postgres_listing_store import (
    PostgresListingStore,
    UnknownListingReferenceError,
)
from redis.exceptions import ConnectionError as RedisConnectionError
from sqlalchemy import text
from sqlalchemy.engine import make_url
from sqlalchemy.exc import IntegrityError


@pytest.fixture(scope="module")
def anyio_backend():
    return "asyncio"


@asynccontextmanager
async def _temporary_pg_engine():
    admin_dsn = os.environ.get("MIGRATION_TEST_DSN")
    if not admin_dsn:
        pytest.skip("Для тестов PostgreSQL нужен MIGRATION_TEST_DSN")

    database = f"core_store_test_{uuid4().hex}"
    url = make_url(admin_dsn).set(drivername="postgresql+asyncpg", database=database)
    database_url = url.render_as_string(hide_password=False)
    core_api_dir = Path(__file__).resolve().parents[1]

    admin = await asyncpg.connect(admin_dsn)
    try:
        await admin.execute(f'CREATE DATABASE "{database}"')
        try:
            await asyncio.to_thread(
                subprocess.run,
                [sys.executable, "-m", "alembic", "upgrade", "head"],
                cwd=core_api_dir,
                env={**os.environ, "DATABASE_URL": database_url},
                check=True,
                timeout=60,
            )
            engine = create_db_engine(database_url)
            try:
                yield engine
            finally:
                await engine.dispose()
        finally:
            await admin.execute(f'DROP DATABASE "{database}"')
    finally:
        await admin.close()


@pytest.fixture(scope="module")
async def pg_engine(anyio_backend):
    async with _temporary_pg_engine() as engine:
        yield engine


@pytest.fixture
async def outbox_listing(anyio_backend):
    async with _temporary_pg_engine() as engine:
        store = PostgresListingStore(engine)
        listing = ListingCreate(
            source="avito",
            external_id="outbox-test",
            url="https://example.com/outbox-test",
            deal_type="rent",
            country="BY",
            city="Minsk",
            price=Decimal("9007199254740992.25"),
            currency="USD",
            price_usd=9007199254740992,
            rooms=1,
        )
        result = await store.create_or_get(listing)
        yield engine, store, result.listing.id


@pytest.fixture(scope="module")
async def search_store(pg_engine):
    store = PostgresListingStore(pg_engine)
    dates = []

    # external_id, source, price, day, deal_type, country, city, rooms, area_m2
    rows = [
        ("A", "mock", 300, 2, "rent", "BY", "Minsk", 0, None),
        ("B", "mock", 100, 1, "rent", "BY", "Brest", 1, Decimal("45.50")),
        ("C", "mock", 300, 1, "sale", "RU", "Moscow", 2, Decimal("60.00")),
        ("D", "avito", 300, 1, "sale", "RU", "Moscow", 2, Decimal("60.00")),
    ]

    for external_id, source, price, day, deal_type, country, city, rooms, area in rows:
        listing = ListingCreate(
            source=source,
            external_id=external_id,
            url=f"https://example.com/{external_id}",
            deal_type=deal_type,
            country=country,
            city=city,
            price=price,
            currency="USD",
            price_usd=price,
            rooms=rooms,
            area_m2=area,
        )
        result = await store.create_or_get(listing)
        dates.append(
            {
                "id": result.listing.id,
                "created_at": datetime(2026, 1, day, tzinfo=UTC),
            }
        )

    async with pg_engine.begin() as connection:
        await connection.execute(
            text("UPDATE core.listings SET created_at = :created_at WHERE id = :id"),
            dates,
        )

    return store


@pytest.mark.anyio
@pytest.mark.parametrize(
    ("sort", "expected"),
    [
        ("price_asc", ["B", "A", "C"]),
        ("price_desc", ["C", "A", "B"]),
        ("date_asc", ["B", "C", "A"]),
        ("date_desc", ["A", "C", "B"]),
    ],
)
async def test_search_order_across_pages(search_store, sort, expected):
    filters = ListingFilters(source="mock", sort=sort, size=1)

    for page, external_id in enumerate(expected, start=1):
        result = await search_store.search(filters.model_copy(update={"page": page}))
        assert result.total == 3
        assert [item.external_id for item in result.items] == [external_id]

    empty = await search_store.search(filters.model_copy(update={"page": 4}))
    assert empty.total == 3
    assert empty.items == []


@pytest.mark.anyio
async def test_concurrent_duplicate_creates_one_listing_and_one_event(pg_engine):
    store = PostgresListingStore(pg_engine)
    common = {
        "source": "avito",
        "external_id": f"concurrent-{uuid4().hex}",
        "url": "https://example.com/concurrent",
        "deal_type": "rent",
        "country": "BY",
        "city": "Minsk",
        "price": 380,
        "currency": "USD",
        "price_usd": 380,
    }
    first = ListingCreate(
        **common,
        rooms=0,
        photos=["https://example.com/z.jpg", "https://example.com/a.jpg"],
    )
    second = ListingCreate(
        **common,
        rooms=2,
        photos=["https://example.com/other.jpg"],
    )

    first_result, second_result = await asyncio.gather(
        store.create_or_get(first),
        store.create_or_get(second),
    )

    assert first_result.created != second_result.created
    assert first_result.listing == second_result.listing

    winner = first if first_result.created else second
    listing_id = first_result.listing.id
    saved = await store.get(listing_id)
    assert saved == first_result.listing
    assert saved.rooms == winner.rooms
    assert saved.photos == winner.photos

    async with pg_engine.connect() as connection:
        listing_ids = await connection.scalars(
            text("""
                SELECT l.id
                FROM core.listings AS l
                JOIN core.sources AS s ON s.id = l.source_id
                WHERE s.code = :source AND l.external_id = :external_id
            """),
            {"source": winner.source, "external_id": winner.external_id},
        )
        assert listing_ids.all() == [listing_id]

        events = await connection.execute(
            text("""
                SELECT event_id, payload ->> 'event_id' AS payload_event_id,
                       payload ->> 'rooms' AS rooms, published_at
                FROM core.outbox_events
                WHERE listing_id = :listing_id AND kind = 'listing_new'
            """),
            {"listing_id": listing_id},
        )
        event = events.mappings().one()

    assert event["payload_event_id"] == str(event["event_id"])
    assert int(event["rooms"]) == winner.rooms
    assert event["published_at"] is None


@pytest.mark.anyio
async def test_outbox_insert_failure_rolls_back_listing_and_photos(
    pg_engine, monkeypatch
):
    store = PostgresListingStore(pg_engine)
    run_id = uuid4().hex
    fixed_event_id = uuid4()
    photo_url = f"https://example.com/{run_id}/photo.jpg"

    first = ListingCreate(
        source="avito",
        external_id=f"{run_id}-first",
        url=f"https://example.com/{run_id}/first",
        deal_type="rent",
        country="BY",
        city="Minsk",
        price=380,
        currency="USD",
        price_usd=380,
        rooms=1,
    )
    second = ListingCreate.model_validate(
        {
            **first.model_dump(),
            "external_id": f"{run_id}-second",
            "url": f"https://example.com/{run_id}/second",
            "photos": [photo_url],
        }
    )

    with monkeypatch.context() as patch:
        patch.setattr("app.listing_events.uuid4", lambda: fixed_event_id)
        original = await store.create_or_get(first)
        assert original.created is True

        with pytest.raises(IntegrityError) as error:
            await store.create_or_get(second)

    assert error.value.orig.sqlstate == "23505"

    async with pg_engine.connect() as connection:
        listing_count = await connection.scalar(
            text("""
                SELECT count(*) FROM core.listings
                WHERE external_id = :external_id
            """),
            {"external_id": second.external_id},
        )
        photo_count = await connection.scalar(
            text("SELECT count(*) FROM core.listing_photos WHERE url = :url"),
            {"url": photo_url},
        )
        event_owner = await connection.scalar(
            text("""
                SELECT listing_id FROM core.outbox_events
                WHERE event_id = :event_id
            """),
            {"event_id": fixed_event_id},
        )

    assert listing_count == 0
    assert photo_count == 0
    assert event_owner == original.listing.id

    retry = await store.create_or_get(second)
    assert retry.created is True
    assert retry.listing.photos == second.photos


@pytest.mark.anyio
@pytest.mark.parametrize(
    "changes",
    [
        {"source": "unknown-source"},
        {"city": "Unknown City"},
        {"country": "RU", "city": "Minsk"},
        {"currency": "ZZZ"},
    ],
)
async def test_create_rejects_unknown_reference(pg_engine, changes):
    store = PostgresListingStore(pg_engine)
    data = {
        "source": "avito",
        "external_id": uuid4().hex,
        "url": "https://example.com/reference-test",
        "deal_type": "rent",
        "country": "BY",
        "city": "Minsk",
        "price": 380,
        "currency": "USD",
        "price_usd": 380,
        "rooms": 1,
    }
    listing = ListingCreate.model_validate({**data, **changes})

    with pytest.raises(UnknownListingReferenceError):
        await store.create_or_get(listing)


async def _read_outbox_event(engine, listing_id):
    async with engine.connect() as connection:
        result = await connection.execute(
            text("""
                SELECT event_id, published_at
                FROM core.outbox_events
                WHERE listing_id = :listing_id AND kind = 'listing_new'
            """),
            {"listing_id": listing_id},
        )
        return result.mappings().one()


@pytest.mark.anyio
async def test_publish_next_retries_same_event_after_redis_failure(outbox_listing):
    engine, store, listing_id = outbox_listing
    original = await _read_outbox_event(engine, listing_id)
    redis = AsyncMock()
    redis.xadd.side_effect = [RedisConnectionError("Redis unavailable"), "1-0"]
    publish = partial(publish_listing_event, redis)

    with pytest.raises(RedisConnectionError):
        await store.publish_next(publish)

    pending = await _read_outbox_event(engine, listing_id)
    assert pending["published_at"] is None
    assert pending["event_id"] == original["event_id"]

    assert await store.publish_next(publish) is True
    first, retry = redis.xadd.await_args_list
    assert retry == first
    stream, fields = retry.args
    assert stream == "listings.new"
    assert json.loads(fields["data"])["event_id"] == str(original["event_id"])


@pytest.mark.anyio
async def test_publish_next_marks_event_published_once(outbox_listing):
    engine, store, listing_id = outbox_listing
    publish = AsyncMock()

    assert await store.publish_next(publish) is True

    event = await _read_outbox_event(engine, listing_id)
    assert event["published_at"] is not None
    assert await store.publish_next(publish) is False
    publish.assert_awaited_once()


@pytest.mark.anyio
async def test_publish_next_preserves_decimal_price_in_stream(outbox_listing):
    _, store, listing_id = outbox_listing
    redis = AsyncMock()

    assert await store.publish_next(partial(publish_listing_event, redis)) is True

    redis.xadd.assert_awaited_once()
    stream, fields = redis.xadd.await_args.args
    assert stream == "listings.new"
    payload = json.loads(fields["data"], parse_float=Decimal)
    assert payload["id"] == listing_id
    assert payload["price"] == Decimal("9007199254740992.25")


@pytest.mark.anyio
@pytest.mark.parametrize(
    ("criteria", "expected"),
    [
        ({"price_min": 300}, ["A", "C"]),
        ({"price_max": 100}, ["B"]),
        ({"area_min": "60.00"}, ["C"]),
        ({"area_max": "45.50"}, ["B"]),
        ({"rooms": 0}, ["A"]),
        ({"rooms": 2_147_483_648}, []),
        ({"deal_type": "sale"}, ["C"]),
        ({"country": "BY"}, ["B", "A"]),
        ({"country": "BY", "city": "Brest"}, ["B"]),
    ],
)
async def test_search_applies_sql_filters(search_store, criteria, expected):
    filters = ListingFilters(source="mock", sort="price_asc", **criteria)

    result = await search_store.search(filters)

    assert [item.external_id for item in result.items] == expected
    assert result.total == len(expected)
