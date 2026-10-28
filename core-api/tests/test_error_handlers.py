import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient

from app.error_handlers import register_error_handlers


@pytest.fixture
def client():
    app = FastAPI()
    register_error_handlers(app)

    @app.get("/validation/{value}")
    async def validate_value(value: int):
        return {"value": value}

    @app.get("/crash")
    async def crash():
        raise RuntimeError("Internal failure details")

    with TestClient(app, raise_server_exceptions=False) as test_client:
        yield test_client


def test_validation_error(client):
    response = client.get("/validation/abc")

    assert response.status_code == 422
    assert response.json() == {
        "error": "validation_error",
        "message": "Некорректное значение поля: path.value",
    }


def test_internal_error(client, caplog):
    with caplog.at_level("ERROR", logger="app.error_handlers"):
        response = client.get("/crash")

    assert response.status_code == 500
    assert response.json() == {
        "error": "internal_error",
        "message": "Внутренняя ошибка сервера",
    }

    assert "RuntimeError: Internal failure details" in caplog.text