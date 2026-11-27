import asyncio
import json
from contextlib import asynccontextmanager
from datetime import UTC, datetime
from decimal import Decimal
from functools import partial
from unittest.mock import AsyncMock
from uuid import uuid4

import httpx2
import pytest
import simplejson
from app.database import create_db_engine
from app.listing_filters import ListingFilters
from app.listing_models import ListingCreate
from app.listing_store import UnknownListingReferenceError
from app.main import app
from app.outbox import publish_listing_event
from app.postgres_listing_store import PostgresListingStore
from redis.exceptions import ConnectionError as RedisConnectionError
from sqlalchemy import text
from sqlalchemy.exc import IntegrityError


@pytest.fixture(scope="module")
def anyio_backend():
    return "asyncio"


@asynccontextmanager
async def _temporary_pg_engine(temporary_database, run_alembic):
    async with temporary_database() as url:
        await run_alembic(url, "upgrade", "head")
        app_url = url.set(drivername="postgresql+asyncpg")
        engine = create_db_engine(app_url.render_as_string(hide_password=False))
        try:
            yield engine
        finally:
            await engine.dispose()


@pytest.fixture(scope="module")
async def pg_engine(anyio_backend, temporary_database, run_alembic):
    async with _temporary_pg_engine(temporary_database, run_alembic) as engine:
        yield engine


@pytest.fixture
async def outbox_listing(anyio_backend, temporary_database, run_alembic):
    async with _temporary_pg_engine(temporary_database, run_alembic) as engine:
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


@pytest.fixture
def listing_data():
    external_id = uuid4().hex
    return {
        "source": "avito",
        "external_id": external_id,
        "url": f"https://example.com/{external_id}",
        "deal_type": "rent",
        "country": "BY",
        "city": "Minsk",
        "price": 380,
        "currency": "USD",
        "price_usd": 380,
        "rooms": 1,
    }


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
async def test_concurrent_duplicate_creates_one_listing_and_one_event(
    pg_engine, listing_data
):
    store = PostgresListingStore(pg_engine)
    first = ListingCreate.model_validate(
        {
            **listing_data,
            "rooms": 0,
            "photos": ["https://example.com/z.jpg", "https://example.com/a.jpg"],
        }
    )
    second = ListingCreate.model_validate(
        {
            **listing_data,
            "rooms": 2,
            "photos": ["https://example.com/other.jpg"],
        }
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
    pg_engine, monkeypatch, listing_data
):
    store = PostgresListingStore(pg_engine)
    run_id = listing_data["external_id"]
    fixed_event_id = uuid4()
    photo_url = f"https://example.com/{run_id}/photo.jpg"

    first = ListingCreate.model_validate(listing_data)
    second = ListingCreate.model_validate(
        {
            **listing_data,
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
async def test_create_rejects_unknown_reference(pg_engine, changes, listing_data):
    store = PostgresListingStore(pg_engine)
    listing = ListingCreate.model_validate({**listing_data, **changes})

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


@pytest.mark.anyio
@pytest.mark.parametrize("listing_id", [0, -(2**63) - 1, 2**63])
async def test_get_out_of_range_listing_id(pg_engine, listing_id):
    store = PostgresListingStore(pg_engine)
    assert await store.get(listing_id) is None


@pytest.fixture
async def price_client(pg_engine, monkeypatch):
    monkeypatch.setattr(
        app.state, "listing_store", PostgresListingStore(pg_engine), raising=False
    )
    async with httpx2.AsyncClient(
        transport=httpx2.ASGITransport(app=app), base_url="http://test"
    ) as client:
        yield client


@pytest.mark.anyio
@pytest.mark.parametrize(
    ("currency", "price", "expected_usd"),
    [("USD", 10.49, 10), ("USD", 10.50, 11), ("PLN", 42, 11)],
)
async def test_price_usd_validation(
    price_client, listing_data, currency, price, expected_usd
):
    url = "/api/v1/listings"
    payload = {**listing_data, "currency": currency, "price": price}

    invalid = await price_client.post(
        url, json={**payload, "price_usd": expected_usd - 1}
    )
    assert invalid.status_code == 422, invalid.text
    assert invalid.json()["error"] == "validation_error"
    assert "price_usd" in invalid.json()["message"]

    valid = await price_client.post(url, json={**payload, "price_usd": expected_usd})
    assert valid.status_code == 201, valid.text


@pytest.mark.anyio
async def test_price_survives_http_json(price_client, listing_data):
    url = "/api/v1/listings"
    price = Decimal("9007199254740992.25")
    data = {**listing_data, "price": price, "price_usd": 9007199254740992}

    created = await price_client.post(
        url,
        content=simplejson.dumps(data, use_decimal=True),
        headers={"Content-Type": "application/json"},
    )
    assert created.status_code == 201, created.text

    detail = await price_client.get(f"{url}/{created.json()['id']}")
    assert detail.status_code == 200
    assert json.loads(detail.content, parse_float=Decimal)["price"] == price


@pytest.mark.anyio
async def test_duplicate_after_rate_change(price_client, pg_engine, listing_data):
    url = "/api/v1/listings"
    async with pg_engine.begin() as connection:
        await connection.execute(
            text("INSERT INTO core.currencies (code, rate_to_usd) VALUES ('TST', 0.25)")
        )

    payload = {**listing_data, "currency": "TST", "price": 40, "price_usd": 10}
    created = await price_client.post(url, json=payload)
    assert created.status_code == 201, created.text
    listing_id = created.json()["id"]

    async with pg_engine.begin() as connection:
        await connection.execute(
            text("UPDATE core.currencies SET rate_to_usd = 0.50 WHERE code = 'TST'")
        )

    repeat = await price_client.post(url, json=payload)
    assert repeat.status_code == 200, repeat.text
    assert repeat.json() == {"id": listing_id, "created": False}

    detail = await price_client.get(f"{url}/{listing_id}")
    assert detail.status_code == 200
    assert detail.json()["price_usd"] == 10

    async with pg_engine.connect() as connection:
        event_count = await connection.scalar(
            text("""
                SELECT count(*) FROM core.outbox_events
                WHERE listing_id = :id AND kind = 'listing_new'
            """),
            {"id": listing_id},
        )
    assert event_count == 1

    new_listing = {**payload, "external_id": f"{payload['external_id']}-new"}
    invalid = await price_client.post(url, json=new_listing)
    assert invalid.status_code == 422, invalid.text
    assert "price_usd" in invalid.json()["message"]


@pytest.mark.anyio
async def test_unknown_currency_returns_422(price_client, listing_data):
    response = await price_client.post(
        "/api/v1/listings", json={**listing_data, "currency": "ZZZ"}
    )
    assert response.status_code == 422, response.text
    assert response.json()["error"] == "validation_error"
    assert "валют" in response.json()["message"]
