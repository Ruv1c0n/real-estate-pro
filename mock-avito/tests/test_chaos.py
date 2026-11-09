"""Хаос-режим для проверки ретраев collector'а (contracts.md, раздел 4)."""

import pytest
from fastapi.testclient import TestClient

import app.main as main_mod
from app.main import app

client = TestClient(app)


@pytest.fixture(autouse=True)
def _reset_chaos():
    # Серии хаоса не должны перетекать между тестами.
    main_mod.chaos_left.clear()
    yield
    main_mod.chaos_left.clear()


def test_chaos_429_then_success():
    for _ in range(2):  # fail_times=2: два 429 с Retry-After
        r = client.get("/search/listings", params={"fail": 429, "fail_times": 2})
        assert r.status_code == 429
        assert r.headers["retry-after"] == "2"
        assert r.json()["error"] == "rate_limited"
    # серия исчерпана — успешные ответы
    assert client.get("/search/listings").status_code == 200
    assert client.get("/search/listings").status_code == 200


def test_chaos_500_then_success():
    r = client.get("/search/listings", params={"fail": 500, "fail_times": 1})
    assert r.status_code == 500
    assert r.json()["error"] == "internal_error"
    assert client.get("/search/listings").status_code == 200


def test_chaos_invalid_code():
    r = client.get("/search/listings", params={"fail": 404})
    assert r.status_code == 422
    assert r.json()["error"] == "validation_error"


def test_chaos_independent_per_code():
    # серии по 429 и 500 идут параллельно и не мешают друг другу
    r1 = client.get("/search/listings", params={"fail": 429, "fail_times": 2})
    r2 = client.get("/search/listings", params={"fail": 500, "fail_times": 2})
    assert r1.status_code == 429 and r2.status_code == 500
    # у каждой серии остался один сбой
    assert client.get("/search/listings", params={"fail": 500, "fail_times": 2}).status_code == 500
    assert client.get("/search/listings", params={"fail": 429, "fail_times": 2}).status_code == 429
    # после исчерпания обе серии — успехи
    assert client.get("/search/listings").status_code == 200
