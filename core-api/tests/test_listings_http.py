import json
from datetime import UTC, datetime
from decimal import Decimal
from threading import Event
from unittest.mock import AsyncMock, Mock

import pytest
from app.in_memory_listing_store import InMemoryListingStore
from app.listing_cache import listing_cache_key
from app.listing_filters import ListingFilters
from app.listing_store import UnknownListingReferenceError
from app.main import app
from fastapi.testclient import TestClient
from redis.exceptions import ConnectionError as RedisConnectionError

URL = "/api/v1/listings"


@pytest.fixture
def redis_client(monkeypatch):
    redis = AsyncMock()
    redis.get.return_value = None
    redis.set.return_value = False
    monkeypatch.setattr("app.main.create_redis_client", lambda url: redis)
    return redis


@pytest.fixture
def client(redis_client, monkeypatch):
    store = InMemoryListingStore()
    monkeypatch.setattr(
        store,
        "has_pending_cache_invalidation",
        AsyncMock(return_value=False),
        raising=False,
    )
    monkeypatch.setattr(
        store, "invalidate_next", AsyncMock(return_value=False), raising=False
    )
    monkeypatch.setattr("app.main.PostgresListingStore", lambda engine: store)

    with TestClient(app) as test_client:
        yield test_client


@pytest.fixture
def payload():
    return {
        "source": "mock",
        "external_id": "http-123",
        "url": "https://example.com/listing/123",
        "deal_type": "rent",
        "country": "BY",
        "city": "Minsk",
        "price": 380.25,
        "currency": "USD",
        "price_usd": 380,
        "rooms": 0,
    }


def test_create_then_repeat_returns_same_id(client, payload):
    first = client.post(URL, json=payload)

    assert first.status_code == 201, first.text
    listing_id = first.json()["id"]
    assert listing_id > 0
    assert first.json() == {"id": listing_id, "created": True}

    changed = {**payload, "price": 500, "price_usd": 500}
    repeat = client.post(URL, json=changed)

    assert repeat.status_code == 200, repeat.text
    assert repeat.json() == {"id": listing_id, "created": False}


def test_invalid_listing_returns_422_then_corrected_listing_is_created(client, payload):
    invalid = client.post(URL, json={**payload, "rooms": -1})

    assert invalid.status_code == 422, invalid.text
    body = invalid.json()
    assert isinstance(body["error"], str) and body["error"]
    assert isinstance(body["message"], str) and body["message"]

    corrected = client.post(URL, json=payload)

    assert corrected.status_code == 201, corrected.text
    assert corrected.json()["created"] is True


def test_get_listing_by_id(client, payload):
    photos = [
        "https://example.com/second.jpg",
        "https://example.com/first.jpg",
    ]
    payload.update(photos=photos, area_m2=42.75)

    created = client.post(URL, json=payload)
    assert created.status_code == 201, created.text
    listing_id = created.json()["id"]

    response = client.get(f"{URL}/{listing_id}")
    assert response.status_code == 200, response.text

    body = json.loads(response.content, parse_float=Decimal)
    assert body["id"] == listing_id
    assert body["external_id"] == payload["external_id"]
    assert body["price"] == Decimal("380.25")
    assert body["area_m2"] == Decimal("42.75")
    assert body["price_usd"] == 380
    assert body["rooms"] == 0
    assert body["photos"] == photos
    assert body["published_at"] is None
    assert body["created_at"]


def test_get_unknown_listing_returns_404(client):
    response = client.get(f"{URL}/999")

    assert response.status_code == 404, response.text
    body = response.json()
    assert isinstance(body["error"], str) and body["error"]
    assert isinstance(body["message"], str) and body["message"]


def test_list_listings_returns_filtered_page(client, payload):
    for external_id, rooms, price in [
        ("expensive-studio", 0, 500),
        ("cheap-studio", 0, 300),
        ("one-room-flat", 1, 200),
    ]:
        listing = {
            **payload,
            "external_id": external_id,
            "rooms": rooms,
            "price": price,
            "price_usd": price,
        }
        created = client.post(URL, json=listing)
        assert created.status_code == 201, created.text

    params = {"rooms": 0, "sort": "price_asc", "page": 1, "size": 1}
    response = client.get(URL, params=params)

    assert response.status_code == 200, response.text
    body = response.json()
    assert body["total"] == 2
    assert body["page"] == 1
    assert body["size"] == 1
    assert [item["external_id"] for item in body["items"]] == ["cheap-studio"]
    assert body["items"][0]["price"] == 300


def test_list_listings_rejects_city_without_country(client):
    response = client.get(URL, params={"city": "Minsk"})

    assert response.status_code == 422, response.text
    body = response.json()
    assert body["error"]
    assert body["message"]


@pytest.mark.parametrize(
    ("params", "expected"),
    [
        ({}, ["newer", "older"]),
        ({"sort": "date_desc"}, ["newer", "older"]),
        ({"sort": "date_asc"}, ["older", "newer"]),
    ],
)
def test_list_listings_sorts_by_created_at(
    client, payload, monkeypatch, params, expected
):
    clock = Mock()
    clock.now.side_effect = [
        datetime(2026, 1, 2, tzinfo=UTC),
        datetime(2026, 1, 1, tzinfo=UTC),
    ]
    monkeypatch.setattr("app.in_memory_listing_store.datetime", clock)

    for external_id in ("newer", "older"):
        created = client.post(URL, json={**payload, "external_id": external_id})
        assert created.status_code == 201, created.text

    response = client.get(URL, params=params)

    assert response.status_code == 200, response.text
    assert [item["external_id"] for item in response.json()["items"]] == expected


@pytest.mark.parametrize(
    ("params", "expected"),
    [
        ({}, {"small", "medium", "large", "unknown"}),
        ({"area_min": "42.75"}, {"medium", "large"}),
        ({"area_max": "42.75"}, {"small", "medium"}),
        ({"area_min": "42.75", "area_max": "42.75"}, {"medium"}),
    ],
)
def test_list_listings_filters_by_area(client, payload, params, expected):
    for external_id, area in [
        ("small", 30),
        ("medium", 42.75),
        ("large", 60),
        ("unknown", None),
    ]:
        listing = {**payload, "external_id": external_id, "area_m2": area}
        created = client.post(URL, json=listing)
        assert created.status_code == 201, created.text

    response = client.get(URL, params=params)

    assert response.status_code == 200, response.text
    body = response.json()
    assert body["total"] == len(expected)
    assert {item["external_id"] for item in body["items"]} == expected


def test_created_listing_is_published_by_worker(client, redis_client, payload):
    published = Event()

    async def xadd(stream, fields):
        published.set()
        return "1-0"

    redis_client.xadd.side_effect = xadd

    response = client.post(URL, json=payload)
    assert response.status_code == 201, response.text
    assert published.wait(timeout=5), "Воркер не опубликовал событие"

    redis_client.xadd.assert_awaited_once()
    stream, fields = redis_client.xadd.await_args.args
    assert stream == "listings.new"

    event = json.loads(fields["data"], parse_float=Decimal)
    assert event["id"] == response.json()["id"]
    assert event["price"] == Decimal("380.25")


def test_create_returns_422_for_unknown_reference(client, payload, monkeypatch):
    message = "Неизвестный источник, город для указанной страны или валюта"
    monkeypatch.setattr(
        app.state.listing_store,
        "create_or_get",
        AsyncMock(side_effect=UnknownListingReferenceError(message)),
    )

    response = client.post(URL, json=payload)

    assert response.status_code == 422, response.text
    body = response.json()
    assert body["error"]
    assert body["message"] == message


def test_post_body_limit_in_bytes(client, payload, monkeypatch):
    body = json.dumps({**payload, "title": "Квартира"}, ensure_ascii=False).encode(
        "utf-8"
    )
    monkeypatch.setattr("app.listing_body_limit.MAX_LISTING_BODY_BYTES", len(body))
    headers = {"Content-Type": "application/json"}

    allowed = client.post(URL, content=body, headers=headers)
    assert allowed.status_code == 201, allowed.text

    rejected = client.post(URL, content=body + b" ", headers=headers)
    assert rejected.status_code == 413, rejected.text
    assert rejected.json()["error"] == "payload_too_large"


def test_post_field_limit_returns_422(client, payload):
    response = client.post(URL, json={**payload, "title": "я" * 257})

    assert response.status_code == 422, response.text
    assert response.json()["error"] == "validation_error"


def test_listings_cache_hit_keeps_exact_json(
    client, redis_client, payload, monkeypatch
):
    search = AsyncMock(wraps=app.state.listing_store.search)
    monkeypatch.setattr(app.state.listing_store, "search", search)

    assert client.post(URL, json=payload).status_code == 201
    redis_client.get.side_effect = ["7", None]
    first = client.get(URL)
    assert first.status_code == 200, first.text

    redis_client.get.side_effect = ["7", first.text]
    second = client.get(URL)
    assert second.status_code == 200, second.text
    assert second.content == first.content
    items = json.loads(second.content, parse_float=Decimal)["items"]
    assert items[0]["price"] == Decimal("380.25")

    search.assert_awaited_once()
    key = redis_client.get.await_args.args[0]
    redis_client.set.assert_awaited_once_with(key, first.text, ex=600)


@pytest.mark.parametrize("operation", ["get", "set"])
def test_listings_fall_back_when_redis_fails(client, redis_client, payload, operation):
    if operation == "set":
        redis_client.get.side_effect = ["7", None]
    getattr(redis_client, operation).side_effect = RedisConnectionError(
        "Redis unavailable"
    )

    assert client.post(URL, json=payload).status_code == 201
    response = client.get(URL)

    assert response.status_code == 200, response.text
    assert response.json()["total"] == 1


def test_listings_skip_cache_until_invalidation_is_processed(
    client, redis_client, payload
):
    store = app.state.listing_store
    redis_client.get.side_effect = ["7", None]
    stale = client.get(URL)
    assert stale.json()["total"] == 0

    assert client.post(URL, json=payload).status_code == 201
    store.has_pending_cache_invalidation.return_value = True
    redis_client.get.reset_mock()
    redis_client.get.side_effect = ["7", stale.text]

    fresh = client.get(URL)
    assert fresh.status_code == 200, fresh.text
    assert fresh.json()["total"] == 1
    redis_client.get.assert_not_awaited()

    store.has_pending_cache_invalidation.return_value = False
    redis_client.get.side_effect = ["8", None]
    updated = client.get(URL)
    assert updated.content == fresh.content
    key = listing_cache_key(ListingFilters(), 8)
    redis_client.set.assert_awaited_with(key, updated.text, ex=600)

    redis_client.get.side_effect = ["8", updated.text]
    assert client.get(URL).content == fresh.content
