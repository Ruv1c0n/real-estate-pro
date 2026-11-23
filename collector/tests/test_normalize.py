"""Нормализация (ФТ-1.3): цена/валюта, комнаты, площадь, HTML, справочники."""

from decimal import Decimal

import pytest

from app.dicts import DictsSnapshot
from app.normalize import NormalizeError, normalize, strip_html
from app.schemas import RawListing


def dicts_snapshot() -> DictsSnapshot:
    return DictsSnapshot(
        countries=["BY", "RU", "PL"],
        cities=[
            {"country": "BY", "name": "Minsk", "aliases": ["Минск"]},
            {"country": "RU", "name": "Moscow", "aliases": ["Москва"]},
            {"country": "PL", "name": "Warsaw", "aliases": []},
        ],
        currencies={"USD": Decimal(1), "BYN": Decimal("0.3035")},
        sources=["mock", "avito"],
    )


def raw(**overrides) -> RawListing:
    base = {
        "source": "mock",
        "external_id": "seed-000001",
        "deal_type": "rent",
        "country": "BY",
        "city": "Minsk",
        "rooms": 2,
        "area_m2": 45.5,
        "price": 450,
        "currency": "USD",
        "url": "http://mock.example/listing/1",
    }
    base.update(overrides)
    return RawListing.model_validate(base)


def test_studio_rooms_to_zero():
    assert normalize(raw(rooms="студия"), None).rooms == 0
    assert normalize(raw(rooms="Studio"), None).rooms == 0


def test_rooms_must_be_non_negative_int():
    with pytest.raises(NormalizeError, match="не целое"):
        normalize(raw(rooms="две"), None)
    with pytest.raises(NormalizeError, match="отрицательное"):
        normalize(raw(rooms=-1), None)


def test_strip_html_cleans_title_and_description():
    payload = normalize(
        raw(
            title="<b>2-к квартира</b>, 45.5 м²",
            description="<i>Тихий район</i>, зелёный двор",
        ),
        None,
    )
    assert payload.title == "2-к квартира, 45.5 м²"
    assert payload.description == "Тихий район, зелёный двор"


def test_strip_html_broken_markup_falls_back():
    assert strip_html("Текст <b незакрытый хвост") is not None


def test_price_usd_round_half_up_with_rate():
    payload = normalize(raw(price=1000, currency="BYN"), dicts_snapshot())
    # 1000 × 0.3035 = 303.5 → ROUND_HALF_UP → 304 (ТЗ раздел 3)
    assert payload.price_usd == 304


def test_price_usd_without_dicts_usd_only():
    payload = normalize(raw(price=100.5), None)
    assert payload.price_usd == 101
    assert payload.currency == "USD"


def test_non_usd_without_dicts_is_skipped():
    with pytest.raises(NormalizeError, match="курсы недоступны"):
        normalize(raw(currency="BYN"), None)


def test_unknown_currency_with_dicts():
    with pytest.raises(NormalizeError, match="нет в справочнике"):
        normalize(raw(currency="XYZ"), dicts_snapshot())


def test_city_alias_canonicalized():
    assert normalize(raw(city="Минск"), dicts_snapshot()).city == "Minsk"


def test_unknown_city_rejected_with_dicts():
    with pytest.raises(NormalizeError, match="нет в справочнике"):
        normalize(raw(city="Atlantis"), dicts_snapshot())


def test_unknown_country_rejected_with_dicts():
    with pytest.raises(NormalizeError, match="нет в справочнике"):
        normalize(raw(country="ZZ"), dicts_snapshot())


def test_unknown_source_rejected_with_dicts():
    with pytest.raises(NormalizeError, match="source"):
        normalize(raw(source=" craigslist "), dicts_snapshot())


def test_without_dicts_codes_pass_by_format():
    payload = normalize(raw(country="by", city="Gomel"), None)
    assert payload.country == "BY"
    assert payload.city == "Gomel"


def test_bad_deal_type_rejected():
    with pytest.raises(NormalizeError, match="deal_type"):
        normalize(raw(deal_type="exchange"), None)


def test_bad_url_rejected():
    with pytest.raises(NormalizeError, match="url"):
        normalize(raw(url="javascript:alert(1)"), None)


def test_published_at_converted_to_utc():
    payload = normalize(raw(published_at="2026-11-20T10:00:00+03:00"), None)
    assert payload.published_at == "2026-11-20T07:00:00Z"


def test_naive_published_at_treated_as_utc():
    payload = normalize(raw(published_at="2026-11-20T10:00:00"), None)
    assert payload.published_at == "2026-11-20T10:00:00Z"


def test_invalid_published_at_rejected():
    with pytest.raises(NormalizeError, match="ISO 8601"):
        normalize(raw(published_at="вчера"), None)


def test_area_optional_and_quantized():
    assert normalize(raw(area_m2=None), None).area_m2 is None
    assert normalize(raw(area_m2="45.455"), None).area_m2 == Decimal("45.46")
    with pytest.raises(NormalizeError, match="area_m2"):
        normalize(raw(area_m2=0), None)


def test_floor_as_string():
    assert normalize(raw(floor="7"), None).floor == 7
    with pytest.raises(NormalizeError, match="floor"):
        normalize(raw(floor="7-й"), None)


def test_photos_filtered_to_http():
    payload = normalize(
        raw(photos=["https://img.example/1.jpg", "javascript:alert(1)"]), None
    )
    assert payload.photos == ["https://img.example/1.jpg"]


def test_payload_to_dict_uses_numbers():
    data = normalize(raw(price=450, area_m2=45.5), None).to_dict()
    assert isinstance(data["price"], float)
    assert isinstance(data["area_m2"], float)
