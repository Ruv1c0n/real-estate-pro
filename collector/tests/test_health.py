"""Health-эндпоинты (НФТ-8): живость и готовность с деградацией."""

import httpx
import pytest
from conftest import FakeRedis


@pytest.mark.anyio
async def test_live(wired_app, make_settings):
    client = wired_app(make_settings(), lambda r: httpx.Response(200, json={}))
    response = await client.get("/health")

    assert response.status_code == 200
    assert response.json()["service"] == "collector"
    assert response.json()["status"] == "ok"


@pytest.mark.anyio
async def test_ready_ok_dicts_degraded(wired_app, make_settings):
    def handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/health":
            return httpx.Response(200, json={"status": "ok"})
        return httpx.Response(404, json={"error": "not_found", "message": "нет"})

    client = wired_app(make_settings(), handler)
    response = await client.get("/health/ready")

    assert response.status_code == 200
    body = response.json()
    assert body["core_api"] is True
    assert body["redis"] is True
    # ФТ-2.4 ещё не смержен: справочников нет, но готовность не валят —
    # нормализация деградирует (USD-only), а не останавливается.
    assert body["dicts"] is False


@pytest.mark.anyio
async def test_ready_degraded_on_core_outage(wired_app, make_settings):
    def handler(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("connection refused")

    client = wired_app(make_settings(), handler)
    response = await client.get("/health/ready")

    assert response.status_code == 503
    assert response.json()["status"] == "degraded"
    assert response.json()["core_api"] is False


@pytest.mark.anyio
async def test_ready_degraded_on_redis_down(wired_app, make_settings):
    def handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/health":
            return httpx.Response(200, json={"status": "ok"})
        return httpx.Response(404)

    client = wired_app(make_settings(), handler, FakeRedis(fail=True))
    response = await client.get("/health/ready")

    assert response.status_code == 503
    assert response.json()["redis"] is False
