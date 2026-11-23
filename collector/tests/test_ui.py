"""UI-страницы: список с фильтрами, карточка, деградация (ФТ-1.7, НФТ-15)."""

import httpx
import pytest

LISTINGS_BODY = {
    "items": [
        {
            "id": 1,
            "source": "mock",
            "deal_type": "rent",
            "country": "BY",
            "city": "Minsk",
            "rooms": 2,
            "area_m2": 45.5,
            "price": 450,
            "currency": "USD",
            "price_usd": 450,
            "title": "2-к квартира, 45.5 м²",
            "address": "ул. Притыцкого, 10",
            "url": "http://mock.example/listing/1",
        },
        {
            "id": 2,
            "source": "avito",
            "deal_type": "sale",
            "country": "RU",
            "city": "Moscow",
            "rooms": 3,
            "area_m2": 70,
            "price": 95000,
            "currency": "RUB",
            "price_usd": 950,
            "title": "3-к квартира, 70 м²",
            "address": None,
            "url": "http://avito.example/item/2",
        },
    ],
    "total": 2,
    "page": 1,
    "size": 20,
}


@pytest.mark.anyio
async def test_list_page_renders_and_marks_mock(wired_app, make_settings):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json=LISTINGS_BODY)

    client = wired_app(make_settings(), handler)
    response = await client.get("/listings")

    assert response.status_code == 200
    assert "2-к квартира" in response.text
    assert "синтетические данные" in response.text  # отметка mock (ФТ-1.7)
    assert "≈ $450" in response.text


@pytest.mark.anyio
async def test_filters_forwarded_to_core_api(wired_app, make_settings):
    seen: dict[str, str] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen.update(request.url.params)
        return httpx.Response(200, json={**LISTINGS_BODY, "items": [], "total": 0})

    client = wired_app(make_settings(), handler)
    response = await client.get(
        "/listings",
        params={
            "deal_type": "rent",
            "country": "BY",
            "city": "Minsk",
            "price_max": "500",
            "rooms": "2",
            "sort": "price_asc",
            "page": "2",
            "size": "5",
        },
    )

    assert response.status_code == 200
    assert seen["deal_type"] == "rent"
    assert seen["country"] == "BY"
    assert seen["city"] == "Minsk"
    assert seen["price_max"] == "500"
    assert seen["rooms"] == "2"
    assert seen["sort"] == "price_asc"
    assert seen["page"] == "2"
    assert seen["size"] == "5"


@pytest.mark.anyio
async def test_htmx_request_returns_partial(wired_app, make_settings):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json=LISTINGS_BODY)

    client = wired_app(make_settings(), handler)
    response = await client.get("/listings", headers={"HX-Request": "true"})

    assert "<html" not in response.text  # частичный шаблон, не вся страница
    assert 'id="results"' in response.text


@pytest.mark.anyio
async def test_empty_results_message(wired_app, make_settings):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200, json={"items": [], "total": 0, "page": 1, "size": 20}
        )

    client = wired_app(make_settings(), handler)
    response = await client.get("/listings")

    assert "Ничего не найдено" in response.text


@pytest.mark.anyio
async def test_core_api_outage_shown_friendly(wired_app, make_settings):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(500, json={"error": "internal_error"})

    client = wired_app(make_settings(), handler)
    response = await client.get("/listings")

    assert response.status_code == 200
    assert "Core API недоступен" in response.text


@pytest.mark.anyio
async def test_index_redirects_to_list(wired_app, make_settings):
    client = wired_app(make_settings(), lambda r: httpx.Response(200, json={}))
    response = await client.get("/", follow_redirects=False)

    assert response.status_code in (301, 302, 307, 308)
    assert response.headers["location"] == "/listings"


@pytest.mark.anyio
async def test_card_page_renders_source_link(wired_app, make_settings):
    card = LISTINGS_BODY["items"][0] | {"description": "<i>Тихий район</i>"}

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/api/v1/listings/1"
        return httpx.Response(200, json=card)

    client = wired_app(make_settings(), handler)
    response = await client.get("/listings/1")

    assert response.status_code == 200
    assert "Открыть источник" in response.text
    assert "http://mock.example/listing/1" in response.text
    assert "синтетические данные" in response.text


@pytest.mark.anyio
async def test_card_404_message(wired_app, make_settings):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            404, json={"error": "not_found", "message": "Объявление не найдено"}
        )

    client = wired_app(make_settings(), handler)
    response = await client.get("/listings/999")

    assert response.status_code == 404
    assert "не найдено" in response.text
