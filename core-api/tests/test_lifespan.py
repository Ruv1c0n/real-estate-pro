from types import SimpleNamespace
from unittest.mock import AsyncMock, MagicMock

import pytest
from app import main
from fastapi.testclient import TestClient


@pytest.fixture
def clients(monkeypatch):
    engine = SimpleNamespace(dispose=AsyncMock())
    redis_client = SimpleNamespace(aclose=AsyncMock())

    db_factory = MagicMock(return_value=engine)
    redis_factory = MagicMock(return_value=redis_client)

    monkeypatch.setattr(main, "create_db_engine", db_factory)
    monkeypatch.setattr(main, "create_redis_client", redis_factory)

    monkeypatch.setattr(main, "setup_logging", lambda level: None)

    return engine, redis_client, db_factory, redis_factory


def test_clients_lifecycle(clients):
    engine, redis_client, db_factory, redis_factory = clients

    with TestClient(main.app) as client:
        db_factory.assert_called_once_with(main.settings.database_url)
        redis_factory.assert_called_once_with(main.settings.redis_url)

        assert main.app.state.db_engine is engine
        assert main.app.state.redis is redis_client

        engine.dispose.assert_not_awaited()
        redis_client.aclose.assert_not_awaited()

        response = client.get("/health/live")
        assert response.status_code == 200
        assert response.json() == {"status": "alive"}

    engine.dispose.assert_awaited_once()
    redis_client.aclose.assert_awaited_once()


def test_db_closed_when_redis_initialization_fails(clients):
    engine, redis_client, db_factory, redis_factory = clients
    redis_factory.side_effect = RuntimeError("Redis initialization failed")

    with (
        pytest.raises(RuntimeError, match="Redis initialization failed"),
        TestClient(main.app),
    ):
        pytest.fail("Application must not start")

    db_factory.assert_called_once()
    redis_factory.assert_called_once()
    engine.dispose.assert_awaited_once()
    redis_client.aclose.assert_not_awaited()


def test_db_closed_when_redis_cleanup_fails(clients):
    engine, redis_client, _, _ = clients
    redis_client.aclose.side_effect = RuntimeError("Redis cleanup failed")

    with (
        pytest.raises(RuntimeError, match="Redis cleanup failed"),
        TestClient(main.app),
    ):
        engine.dispose.assert_not_awaited()

    redis_client.aclose.assert_awaited_once()
    engine.dispose.assert_awaited_once()
