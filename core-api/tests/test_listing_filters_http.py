from collections.abc import Iterator
from decimal import Decimal
from typing import Annotated

import pytest
from app.error_handlers import register_error_handlers
from app.listing_filters import ListingFilters
from fastapi import FastAPI, Query
from fastapi.testclient import TestClient


@pytest.fixture
def client() -> Iterator[TestClient]:
    app = FastAPI()
    register_error_handlers(app)

    @app.get("/test/listings")
    async def inspect_filters(
        filters: Annotated[ListingFilters, Query()],
    ):
        return filters.model_dump(mode="json")

    with TestClient(app) as test_client:
        yield test_client


def test_http_filters_defaults(client: TestClient):
    response = client.get("/test/listings")

    assert response.status_code == 200
    body = response.json()
    assert body["page"] == 1
    assert body["size"] == 20
    assert body["sort"] == "date_desc"
    assert body["rooms"] is None


def test_http_filters_parse_query_parameters(client: TestClient):
    response = client.get(
        "/test/listings",
        params={
            "deal_type": "rent",
            "country": "BY",
            "city": "Minsk",
            "rooms": "0",
            "price_min": "100.50",
            "price_max": "500",
            "sort": "price_asc",
            "page": "2",
            "size": "10",
        },
    )

    assert response.status_code == 200
    body = response.json()
    assert body["deal_type"] == "rent"
    assert body["country"] == "BY"
    assert body["city"] == "Minsk"
    assert body["rooms"] == 0
    assert Decimal(body["price_min"]) == Decimal("100.50")
    assert Decimal(body["price_max"]) == Decimal(500)
    assert body["sort"] == "price_asc"
    assert body["page"] == 2
    assert body["size"] == 10


@pytest.mark.parametrize(
    "params",
    [
        {"page": "0"},
        {"rooms": "1.5"},
        {"country": "by"},
        {"source": "   "},
        {"sort": "random"},
        {"price_min": "800", "price_max": "300"},
        {"area_min": "50", "area_max": "20"},
        {"city": "Minsk"},
    ],
)
def test_http_filters_return_validation_error(
    client: TestClient,
    params: dict[str, str],
):
    response = client.get("/test/listings", params=params)

    assert response.status_code == 422
    body = response.json()
    assert set(body) == {"error", "message"}
    assert body["error"] == "validation_error"
    assert isinstance(body["message"], str)
    assert body["message"].strip()


@pytest.mark.parametrize(
    ("params", "message"),
    [
        (
            {"price_min": "800", "price_max": "300"},
            "Минимальная цена не может превышать максимальную",
        ),
        (
            {"area_min": "50", "area_max": "20"},
            "Минимальная площадь не может превышать максимальную",
        ),
        (
            {"city": "Minsk"},
            "При указании города необходимо указать страну",
        ),
    ],
)
def test_http_filters_explain_invalid_combinations(
    client: TestClient,
    params: dict[str, str],
    message: str,
):
    response = client.get("/test/listings", params=params)

    assert response.status_code == 422
    assert response.json() == {
        "error": "validation_error",
        "message": message,
    }


def test_http_filters_openapi_query_parameters(client: TestClient):
    response = client.get("/openapi.json")

    assert response.status_code == 200
    operation = response.json()["paths"]["/test/listings"]["get"]

    assert "requestBody" not in operation

    parameters = operation["parameters"]
    assert all(parameter["in"] == "query" for parameter in parameters)
    assert all(parameter["required"] is False for parameter in parameters)

    schemas = {parameter["name"]: parameter["schema"] for parameter in parameters}

    assert set(schemas) == {
        "deal_type",
        "country",
        "city",
        "price_min",
        "price_max",
        "rooms",
        "area_min",
        "area_max",
        "source",
        "sort",
        "page",
        "size",
    }

    assert schemas["page"]["default"] == 1
    assert schemas["page"]["minimum"] == 1

    assert schemas["size"]["default"] == 20
    assert schemas["size"]["minimum"] == 1
    assert schemas["size"]["maximum"] == 100

    assert schemas["sort"]["default"] == "date_desc"
    assert set(schemas["sort"]["enum"]) == {
        "price_asc",
        "price_desc",
        "date_asc",
        "date_desc",
    }
