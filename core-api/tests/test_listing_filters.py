from decimal import Decimal

import pytest
from app.listing_filters import ListingFilters
from pydantic import ValidationError


def test_listing_filters_defaults():
    filters = ListingFilters()

    assert filters.page == 1
    assert filters.size == 20
    assert filters.sort == "date_desc"

    assert filters.deal_type is None
    assert filters.country is None
    assert filters.city is None
    assert filters.price_min is None
    assert filters.price_max is None
    assert filters.rooms is None
    assert filters.area_min is None
    assert filters.area_max is None
    assert filters.source is None


@pytest.mark.parametrize(
    ("page", "size"),
    [
        (1, 1),
        (1, 100),
        (2, 20),
        (1000, 20),
    ],
)
def test_listing_filters_accept_valid_pagination(page: int, size: int):
    filters = ListingFilters(page=page, size=size)

    assert filters.page == page
    assert filters.size == size


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("page", 0),
        ("page", -1),
        ("size", 0),
        ("size", -1),
        ("size", 101),
    ],
)
def test_listing_filters_reject_invalid_pagination(field: str, value: int):
    with pytest.raises(ValidationError) as exc_info:
        ListingFilters(**{field: value})

    assert any(error["loc"] == (field,) for error in exc_info.value.errors())


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("deal_type", "rent"),
        ("deal_type", "sale"),
        ("sort", "price_asc"),
        ("sort", "price_desc"),
        ("sort", "date_asc"),
        ("sort", "date_desc"),
    ],
)
def test_listing_filters_accept_valid_choices(field: str, value: str):
    filters = ListingFilters(**{field: value})

    assert getattr(filters, field) == value


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("deal_type", "buy"),
        ("deal_type", "RENT"),
        ("deal_type", ""),
        ("sort", "price"),
        ("sort", "random"),
        ("sort", ""),
    ],
)
def test_listing_filters_reject_invalid_choices(field: str, value: str):
    with pytest.raises(ValidationError) as exc_info:
        ListingFilters(**{field: value})

    assert any(error["loc"] == (field,) for error in exc_info.value.errors())


@pytest.mark.parametrize("rooms", [0, 1, 5])
def test_listing_filters_accept_valid_rooms(rooms: int):
    filters = ListingFilters(rooms=rooms)

    assert filters.rooms == rooms


@pytest.mark.parametrize("rooms", [-1, 1.5, "abc"])
def test_listing_filters_reject_invalid_rooms(rooms: float | str):
    with pytest.raises(ValidationError) as exc_info:
        ListingFilters(rooms=rooms)

    assert any(error["loc"] == ("rooms",) for error in exc_info.value.errors())


@pytest.mark.parametrize(
    "field",
    ["price_min", "price_max", "area_min", "area_max"],
)
@pytest.mark.parametrize("value", ["0", "0.01", "1234.56"])
def test_listing_filters_accept_valid_bounds(field: str, value: str):
    filters = ListingFilters(**{field: value})
    actual = getattr(filters, field)

    assert isinstance(actual, Decimal)
    assert actual == Decimal(value)


@pytest.mark.parametrize(
    "field",
    ["price_min", "price_max", "area_min", "area_max"],
)
@pytest.mark.parametrize(
    "value",
    ["-0.01", "abc", "NaN", "Infinity", "-Infinity"],
)
def test_listing_filters_reject_invalid_bounds(field: str, value: str):
    with pytest.raises(ValidationError) as exc_info:
        ListingFilters(**{field: value})

    assert any(error["loc"] == (field,) for error in exc_info.value.errors())


@pytest.mark.parametrize(
    ("min_field", "max_field"),
    [
        ("price_min", "price_max"),
        ("area_min", "area_max"),
    ],
)
@pytest.mark.parametrize(
    ("minimum", "maximum"),
    [
        (None, None),
        ("100", None),
        (None, "500"),
        ("100", "500"),
        ("100", "100"),
        ("0", "0"),
    ],
)
def test_listing_filters_accept_valid_ranges(
    min_field: str,
    max_field: str,
    minimum: str | None,
    maximum: str | None,
):
    filters = ListingFilters(**{min_field: minimum, max_field: maximum})

    assert getattr(filters, min_field) == (
        Decimal(minimum) if minimum is not None else None
    )
    assert getattr(filters, max_field) == (
        Decimal(maximum) if maximum is not None else None
    )


@pytest.mark.parametrize(
    ("min_field", "max_field"),
    [
        ("price_min", "price_max"),
        ("area_min", "area_max"),
    ],
)
@pytest.mark.parametrize(
    ("minimum", "maximum"),
    [
        ("800", "300"),
        ("1", "0"),
        ("100.50", "100.49"),
    ],
)
def test_listing_filters_reject_reversed_ranges(
    min_field: str,
    max_field: str,
    minimum: str,
    maximum: str,
):
    with pytest.raises(ValidationError):
        ListingFilters(**{min_field: minimum, max_field: maximum})


@pytest.mark.parametrize("country", ["BY", "RU", "PL"])
def test_listing_filters_accept_valid_country(country: str):
    filters = ListingFilters(country=country)

    assert filters.country == country


@pytest.mark.parametrize(
    "country",
    [
        "by",
        "By",
        "B",
        "BLR",
        "12",
        "БY",
        "",
        " BY ",
    ],
)
def test_listing_filters_reject_invalid_country(country: str):
    with pytest.raises(ValidationError) as exc_info:
        ListingFilters(country=country)

    assert any(error["loc"] == ("country",) for error in exc_info.value.errors())


@pytest.mark.parametrize(
    ("field", "value", "country"),
    [
        ("city", "Minsk", "BY"),
        ("city", "New York", "US"),
        ("source", "mock", None),
        ("source", "avito", None),
    ],
)
def test_listing_filters_accept_nonempty_text(
    field: str,
    value: str,
    country: str | None,
):
    filters = ListingFilters(country=country, **{field: value})

    assert getattr(filters, field) == value


@pytest.mark.parametrize("field", ["city", "source"])
@pytest.mark.parametrize("value", ["", "   ", "\t", "\n"])
def test_listing_filters_reject_blank_text(field: str, value: str):
    with pytest.raises(ValidationError) as exc_info:
        ListingFilters(country="BY", **{field: value})

    assert any(error["loc"] == (field,) for error in exc_info.value.errors())


@pytest.mark.parametrize(
    ("country", "city"),
    [
        (None, None),
        ("BY", None),
        ("BY", "Minsk"),
    ],
)
def test_listing_filters_accept_valid_location(
    country: str | None,
    city: str | None,
):
    filters = ListingFilters(country=country, city=city)

    assert filters.country == country
    assert filters.city == city


@pytest.mark.parametrize(
    "location",
    [
        {"city": "Minsk"},
        {"country": None, "city": "Minsk"},
    ],
)
def test_listing_filters_reject_city_without_country(
    location: dict[str, str | None],
):
    with pytest.raises(ValidationError):
        ListingFilters(**location)
