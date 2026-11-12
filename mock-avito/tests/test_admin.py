"""Admin API и консоль (X-Admin-Key, режим, хаос, задержка, rate limit, bad data).

Идёт первым по алфавиту; каждый тест получает чистые настройки через autouse-фикстуру.
"""

import time

import pytest
from app.admin import require_admin
from app.main import app
from app.state import admin_key
from app.state import state as st
from fastapi.testclient import TestClient

client = TestClient(app)
H = {"X-Admin-Key": "k"}


@pytest.fixture(autouse=True)
def _clean_knobs(monkeypatch):
    # Ключ есть в каждом тесте; настройки сброшены в дефолт.
    monkeypatch.setenv("MOCK_ADMIN_KEY", "k")
    st["chaos_code"] = 0
    st["chaos_every"] = 5
    st["delay_ms"] = 0
    st["rate_per_minute"] = 0
    st["bad_data"] = {"enabled": False, "rate": 0.2}
    st["rate_window"].clear()
    st["chaos_left"].clear()
    yield
    monkeypatch.delenv("MOCK_ADMIN_KEY", raising=False)


def test_admin_auth():
    monkeypatch_free = admin_key()  # ключ задан фикстурой
    assert monkeypatch_free == "k"
    r = client.get("/admin/state", headers={"X-Admin-Key": "wrong"})
    assert r.status_code == 403 and r.json()["error"] == "forbidden"
    r = client.get("/admin/state", headers=H)
    assert r.status_code == 200


def test_admin_auth_non_ascii_header():
    # h11 пропускает не-ASCII latin-1 байты в заголовках; раньше compare_digest
    # падал на таких строках TypeError и ручка отвечала 500 вместо 403.
    from types import SimpleNamespace

    from fastapi import HTTPException
    from starlette.datastructures import Headers

    request = SimpleNamespace(
        headers=Headers(raw=[(b"x-admin-key", "klùm".encode("latin-1"))])
    )
    with pytest.raises(HTTPException) as exc:
        require_admin(request)
    assert exc.value.status_code == 403


def test_admin_disabled_without_key(monkeypatch):
    monkeypatch.delenv("MOCK_ADMIN_KEY", raising=False)
    r = client.get("/admin/state", headers=H)
    assert r.status_code == 404  # пустой ключ выключает управление


def test_state_shape():
    body = client.get("/admin/state", headers=H).json()
    for field in (
        "mode",
        "total",
        "publications",
        "seed",
        "uptime_s",
        "requests",
        "chaos",
        "delay_ms",
        "rate_per_minute",
        "bad_data",
    ):
        assert field in body
    assert body["mode"] in ("batch", "stream") and body["total"] >= 500


def test_mode_switch():
    r = client.post("/admin/mode", json={"mode": "stream"}, headers=H)
    assert r.status_code == 200 and r.json()["mode"] == "stream"
    assert client.get("/admin/state", headers=H).json()["mode"] == "stream"
    r = client.post("/admin/mode", json={"mode": "weird"}, headers=H)
    assert r.status_code == 422
    client.post("/admin/mode", json={"mode": "batch"}, headers=H)


def test_global_chaos_every_nth():
    r = client.post("/admin/chaos", json={"code": 500, "every": 2}, headers=H)
    assert r.status_code == 200
    # только /search и /item под хаосом
    assert client.get("/search/listings?size=1").status_code == 200  # 1-й
    assert client.get("/search/listings?size=1").status_code == 500  # 2-й
    assert client.get("/health").status_code == 200  # health вне хаоса
    client.post("/admin/chaos", json={"code": 0}, headers=H)
    assert client.get("/search/listings?size=1").status_code == 200


def test_global_chaos_invalid_code():
    r = client.post("/admin/chaos", json={"code": 404}, headers=H)
    assert r.status_code == 422 and r.json()["error"] == "validation_error"


def test_delay_applies_to_limited_paths():
    client.post("/admin/delay", json={"ms": 150}, headers=H)
    t0 = time.monotonic()
    r = client.get("/search/listings?size=1")
    assert r.status_code == 200 and time.monotonic() - t0 >= 0.15
    # health без задержки
    t0 = time.monotonic()
    assert client.get("/health").status_code == 200
    assert time.monotonic() - t0 < 0.15
    client.post("/admin/delay", json={"ms": 0}, headers=H)


def test_rate_limit():
    client.post("/admin/rate-limit", json={"per_minute": 2}, headers=H)
    assert client.get("/search/listings?size=1").status_code == 200
    assert client.get("/search/listings?size=1").status_code == 200
    r = client.get("/search/listings?size=1")
    assert r.status_code == 429 and int(r.headers["retry-after"]) >= 1
    assert client.get("/admin/state", headers=H).json()["limited_responses"] >= 1
    client.post("/admin/rate-limit", json={"per_minute": 0}, headers=H)
    assert client.get("/search/listings?size=1").status_code == 200


def test_bad_data_via_reset():
    client.post("/admin/bad-data", json={"enabled": True, "rate": 1.0}, headers=H)
    try:
        client.post("/admin/reset", json={"seed": 7}, headers=H)
        items = client.get("/search/listings", params={"size": 100}).json()["items"]
        broken = [
            it
            for it in items
            if it["price"] == 0
            or it["city"] == "Atlantis"
            or it["deal_type"] == "exchange"
            or it["rooms"] < 0
        ]
        assert len(broken) == 100  # доля 1.0 — всё сломано
    finally:
        client.post("/admin/bad-data", json={"enabled": False}, headers=H)
        client.post("/admin/reset", json={"seed": 42}, headers=H)


def test_bad_data_validation():
    r = client.post("/admin/bad-data", json={"enabled": True, "rate": 1.5}, headers=H)
    assert r.status_code == 422
    r = client.post("/admin/reset", json={"seed": 42}, headers=H)
    assert r.json()["total"] >= 500  # пересоздание без bad data даёт полный набор


def test_bad_data_reset_deterministic():
    # «Рестарт пересоздаёт набор из фиксированного seed» — и для bad data тоже:
    # два reset с одним seed дают один и тот же набор ломаных объявлений.
    client.post("/admin/bad-data", json={"enabled": True, "rate": 0.5}, headers=H)
    try:

        def broken_ids() -> set[str]:
            client.post("/admin/reset", json={"seed": 7}, headers=H)
            found: set[str] = set()
            page = 1
            while True:
                body = client.get(
                    "/search/listings", params={"page": page, "size": 100}
                ).json()
                for it in body["items"]:
                    if (
                        it["price"] == 0
                        or it["city"] == "Atlantis"
                        or it["deal_type"] == "exchange"
                        or it["rooms"] < 0
                    ):
                        found.add(it["id"])
                if page * 100 >= body["total"]:
                    return found
                page += 1

        first = broken_ids()
        second = broken_ids()
        assert first and first == second
    finally:
        client.post("/admin/bad-data", json={"enabled": False}, headers=H)
        client.post("/admin/reset", json={"seed": 42}, headers=H)


def test_metrics_counted():
    before = client.get("/admin/state", headers=H).json()["requests"]["search"]
    client.get("/search/listings?size=1")
    client.get("/search/listings?size=1")
    after = client.get("/admin/state", headers=H).json()["requests"]["search"]
    assert after - before == 2


def test_ui_fragments():
    r = client.get("/admin/ui/state")
    assert r.status_code == 200 and 'id="status"' in r.text
    r = client.get("/admin/ui/feed")
    assert r.status_code == 200 and 'id="feed"' in r.text


def test_ui_page():
    r = client.get("/admin/ui")
    assert r.status_code == 200
    assert "/static/htmx.min.js" in r.text and "mock-avito console" in r.text
    # с ключом — блок управления доступен
    assert "hx-post" in r.text
