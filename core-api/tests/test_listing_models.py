import json
from datetime import UTC, datetime
from decimal import Decimal

import pytest
from app.listing_models import ListingCreate
from pydantic import ValidationError


@pytest.fixture
def payload():
    return {
        "source": "mock",
        "external_id": "seed-000123",
        "url": "https://example.com/listing/123",
        "deal_type": "rent",
        "country": "BY",
        "city": "Minsk",
        "price": 380.25,
        "currency": "USD",
        "price_usd": 380,
        "rooms": 0,
    }


def test_minimal_listing_from_json(payload):
    listing = ListingCreate.model_validate_json(json.dumps(payload))

    assert listing.source == "mock"
    assert listing.external_id == "seed-000123"
    assert listing.price == Decimal("380.25")
    assert listing.price_usd == 380
    assert listing.rooms == 0
    assert listing.photos is None
    assert listing.published_at is None


@pytest.mark.parametrize(
    "field",
    [
        "source",
        "external_id",
        "url",
        "deal_type",
        "country",
        "city",
        "price",
        "currency",
        "price_usd",
        "rooms",
    ],
)
def test_required_field_cannot_be_omitted(payload, field):
    del payload[field]

    with pytest.raises(ValidationError) as exc:
        ListingCreate.model_validate(payload)

    assert exc.value.errors()[0]["loc"] == (field,)
    assert exc.value.errors()[0]["type"] == "missing"


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("source", "   "),
        ("external_id", ""),
        ("city", "\t"),
        ("country", "BLR"),
        ("country", "by"),
        ("currency", "US"),
        ("deal_type", "exchange"),
        ("url", "ftp://example.com/listing/123"),
        ("price", 0),
        ("price", -1),
        ("price", "NaN"),
        ("price", "Infinity"),
        ("price", "380.255"),
        ("price_usd", 0),
        ("price_usd", 380.5),
        ("price_usd", True),
        ("rooms", None),
        ("rooms", -1),
        ("rooms", 1.5),
        ("rooms", True),
        ("rooms", "2"),
        ("area_m2", 0),
        ("area_m2", -5),
        ("floor", 2.5),
        ("photos", ["ftp://example.com/photo.jpg"]),
        ("photos", "https://example.com/photo.jpg"),
        ("published_at", "2026-11-18T10:00:00"),
    ],
)
def test_invalid_value_is_rejected(payload, field, value):
    payload[field] = value

    with pytest.raises(ValidationError) as exc:
        ListingCreate.model_validate_json(json.dumps(payload))

    assert exc.value.errors()[0]["loc"][0] == field


def test_optional_fields_accept_null(payload):
    optional_fields = [
        "title",
        "address",
        "description",
        "area_m2",
        "floor",
        "photos",
        "published_at",
    ]
    payload.update(dict.fromkeys(optional_fields))

    listing = ListingCreate.model_validate(payload)

    for field in optional_fields:
        assert getattr(listing, field) is None


@pytest.mark.parametrize(
    "photos",
    [
        [],
        [
            "https://example.com/second.jpg",
            "http://example.com/first.jpg",
        ],
    ],
)
def test_photos_preserve_order_in_json(payload, photos):
    payload["photos"] = photos

    listing = ListingCreate.model_validate(payload)
    result = json.loads(listing.model_dump_json())

    assert result["photos"] == photos


def test_published_at_is_converted_to_utc(payload):
    payload["published_at"] = "2026-11-18T03:30:00+03:00"

    listing = ListingCreate.model_validate(payload)

    assert listing.published_at == datetime(2026, 11, 18, 0, 30, tzinfo=UTC)
    assert listing.published_at.tzinfo is UTC
