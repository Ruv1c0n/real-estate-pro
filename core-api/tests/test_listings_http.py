import pytest
from fastapi.testclient import TestClient

from app.main import app

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
