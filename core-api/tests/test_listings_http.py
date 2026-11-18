import json
from decimal import Decimal

import pytest
from app.main import app
from fastapi.testclient import TestClient

URL = "/api/v1/listings"


@pytest.fixture
def client():
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
