import json
from decimal import Decimal

import httpx2
import pytest
from app.database import create_db_engine
from app.main import app
from sqlalchemy import text

BASE = "/api/v1"
pytestmark = pytest.mark.anyio


@pytest.fixture
def anyio_backend():
    return "asyncio"


@pytest.fixture
async def references(temporary_database, run_alembic, monkeypatch):
    async with temporary_database() as url:
        await run_alembic(url, "upgrade", "head")
        dsn = url.set(drivername="postgresql+asyncpg").render_as_string(
            hide_password=False
        )
        engine = create_db_engine(dsn)
        try:
            monkeypatch.setattr(app.state, "db_engine", engine, raising=False)
            async with httpx2.AsyncClient(
                transport=httpx2.ASGITransport(app=app), base_url="http://test"
            ) as client:
                yield client, engine
        finally:
            await engine.dispose()


async def test_reference_data_formats(references):
    client, _ = references
    for path, expected in [
        ("countries", {"code": "BY", "name": "Belarus"}),
        ("sources", {"code": "avito", "name": "Avito"}),
    ]:
        response = await client.get(f"{BASE}/{path}")
        assert response.status_code == 200
        assert expected in response.json()
        assert all(set(item) == {"code", "name"} for item in response.json())

    response = await client.get(f"{BASE}/currencies")
    assert response.status_code == 200
    rates = json.loads(response.content, parse_float=Decimal)
    assert all(set(item) == {"code", "rate_to_usd"} for item in rates)
    assert {"code": "USD", "rate_to_usd": 1} in rates
    assert {"code": "KZT", "rate_to_usd": Decimal("0.0021")} in rates


async def test_city_filter_and_aliases(references):
    client, engine = references
    async with engine.begin() as connection:
        await connection.execute(
            text("INSERT INTO core.cities (country_code, name) VALUES ('BY', 'Grodno')")
        )

    response = await client.get(f"{BASE}/cities", params={"country": "BY"})
    assert response.status_code == 200
    items = response.json()
    assert all(
        set(row) == {"country", "name", "aliases"} and row["country"] == "BY"
        for row in items
    )
    by_name = {row["name"]: row["aliases"] for row in items}
    assert {"минск", "мінск"} <= set(by_name["Minsk"])
    assert by_name["Grodno"] == []

    all_cities = await client.get(f"{BASE}/cities")
    assert all_cities.status_code == 200
    assert {"BY", "PL"} <= {row["country"] for row in all_cities.json()}
    unknown = await client.get(f"{BASE}/cities", params={"country": "ZZ"})
    assert unknown.status_code == 200
    assert unknown.json() == []
    invalid = await client.get(f"{BASE}/cities", params={"country": "BYX"})
    assert invalid.status_code == 422
    assert invalid.json()["error"] == "validation_error"


async def test_empty_reference_data(references):
    client, engine = references
    # Фикстура создаёт отдельную тестовую БД для каждого сценария.
    async with engine.begin() as connection:
        for table in ("cities", "countries", "sources", "currencies"):
            await connection.execute(text(f"DELETE FROM core.{table}"))

    for path in ("countries", "cities", "sources", "currencies"):
        response = await client.get(f"{BASE}/{path}")
        assert response.status_code == 200
        assert response.json() == []
