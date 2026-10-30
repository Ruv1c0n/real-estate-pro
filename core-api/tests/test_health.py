from types import SimpleNamespace
from unittest.mock import AsyncMock, MagicMock

import pytest
import asyncio

from fastapi import FastAPI
from fastapi.testclient import TestClient
from redis.asyncio import Redis
from redis.exceptions import ConnectionError as RedisConnectionError
from sqlalchemy.ext.asyncio import AsyncEngine

from app.health import router
from app import health

@pytest.fixture
def health_client():
    connection = SimpleNamespace(
        scalar=AsyncMock(return_value=1),
    )

    engine = MagicMock(spec=AsyncEngine)
    engine.connect.return_value.__aenter__.return_value = connection

    redis_client = MagicMock(spec=Redis)
    redis_client.ping = AsyncMock(return_value=True)

    app = FastAPI()
    app.state.db_engine = engine
    app.state.redis = redis_client
    app.include_router(router)

    with TestClient(app) as client:
        yield client, engine, redis_client


def test_live_does_not_access_dependencies(health_client):
    client, engine, redis_client = health_client

    response = client.get("/health/live")

    assert response.status_code == 200
    assert response.json() == {"status": "alive"}
    engine.connect.assert_not_called()
    redis_client.ping.assert_not_awaited()


@pytest.mark.parametrize(
    ("postgres_available", "redis_available"),
    [
        (True, True),
        (False, True),
        (True, False),
        (False, False),
    ],
    ids=[
        "both_available",
        "postgres_unavailable",
        "redis_unavailable",
        "both_unavailable",
    ],
)
def test_ready(health_client, postgres_available, redis_available):
    client, engine, redis_client = health_client

    if not postgres_available:
        engine.connect.return_value.__aenter__.side_effect = OSError(
            "Internal PostgreSQL connection details"
        )

    if not redis_available:
        redis_client.ping.side_effect = RedisConnectionError(
            "Internal Redis connection details"
        )

    response = client.get("/health/ready")

    engine.connect.assert_called_once()
    redis_client.ping.assert_awaited_once()

    if postgres_available and redis_available:
        assert response.status_code == 200
        assert response.json() == {
            "status": "ready",
            "checks": {
                "postgres": "ok",
                "redis": "ok",
            },
        }
    else:
        assert response.status_code == 503
        assert response.json() == {
            "error": "dependency_unavailable",
            "message": "Сервис временно не готов к работе",
        }


@pytest.mark.parametrize("dependency", ["postgres", "redis"])
def test_dependency_check_timeout(monkeypatch, dependency):
    monkeypatch.setattr(health, "CHECK_TIMEOUT", 0.05)

    async def scenario():
        cancelled = False

        async def hanging_operation(*args, **kwargs):
            nonlocal cancelled
            try:
                await asyncio.Event().wait()
            except asyncio.CancelledError:
                cancelled = True
                raise

        if dependency == "postgres":
            connection = SimpleNamespace(
                scalar=AsyncMock(side_effect=hanging_operation),
            )
            engine = MagicMock(spec=AsyncEngine)
            context = engine.connect.return_value
            context.__aenter__.return_value = connection

            probe = health.check_postgres(engine)
        else:
            redis_client = MagicMock(spec=Redis)
            redis_client.ping = AsyncMock(
                side_effect=hanging_operation,
            )

            probe = health.check_redis(redis_client)
        result = await asyncio.wait_for(probe, timeout=2)

        assert result is False
        assert cancelled is True

        if dependency == "postgres":
            context.__aexit__.assert_awaited_once()

    asyncio.run(scenario())