"""Тесты mock-avito (M1: batch-режим)."""

import re

from fastapi.testclient import TestClient

from app.main import app

client = TestClient(app)

ISO_OFFSET = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2}$")


def test_health():
    r = client.get("/health")
    assert r.status_code == 200
    body = r.json()
    assert body["status"] == "ok" and body["mode"] == "batch"
    assert body["total"] >= 500  # ТЗ 3.4


def test_dataset_requirements():
    data = client.get("/search/listings", params={"size": 100}).json()
    countries, cities = set(), set()
    for it in data["items"]:
        countries.add(it["country"])
        cities.add(it["city"])
    # полный набор >500, страны/города проверяем по всем страницам
    page = 1
    while True:
        r = client.get("/search/listings", params={"page": page, "size": 100}).json()
        for it in r["items"]:
            countries.add(it["country"])
            cities.add(it["city"])
        if page * 100 >= r["total"]:
            break
        page += 1
    assert len(countries) >= 5 and len(cities) >= 10  # ТЗ 3.4


def test_pagination_shape_and_defaults():
    r = client.get("/search/listings")
    assert r.status_code == 200
    body = r.json()
    assert set(body) == {"items", "total", "page", "size"}
    assert body["page"] == 1 and body["size"] == 20 and len(body["items"]) == 20
    # пустая страница — 200
    r = client.get("/search/listings", params={"page": 1000})
    assert r.status_code == 200 and r.json()["items"] == []


def test_pagination_beyond_limits():
    assert client.get("/search/listings", params={"page": 0}).status_code == 422
    assert client.get("/search/listings", params={"size": 101}).status_code == 422
    # формат ошибки — {error, message}
    body = client.get("/search/listings", params={"size": 101}).json()
    assert body["error"] == "validation_error" and body["message"]


def test_sorted_by_published_desc():
    items = client.get("/search/listings", params={"size": 100}).json()["items"]
    dates = [it["published_at"] for it in items]
    assert dates == sorted(dates, reverse=True)
    assert all(ISO_OFFSET.match(d) for d in dates)


def test_filters():
    r = client.get("/search/listings", params={"country": "BY", "size": 100})
    assert all(it["country"] == "BY" for it in r.json()["items"])

    r = client.get("/search/listings", params={"city": "Minsk", "size": 100})
    body = r.json()
    assert body["total"] > 0 and all(it["city"] == "Minsk" for it in body["items"])

    r = client.get("/search/listings", params={"deal_type": "sale", "size": 100})
    assert all(it["deal_type"] == "sale" for it in r.json()["items"])

    r = client.get("/search/listings", params={"deal_type": "buy"})
    assert r.status_code == 422

    r = client.get("/search/listings", params={"rooms": 0, "size": 100})
    assert all(it["rooms"] == 0 for it in r.json()["items"])

    # диапазон цен в валюте объявления
    r = client.get("/search/listings", params={"country": "BY", "price_min": 500, "size": 100})
    assert all(it["price"] >= 500 and it["currency"] == "BYN" for it in r.json()["items"])


def test_get_item():
    first = client.get("/search/listings", params={"size": 1}).json()["items"][0]
    r = client.get(f"/item/{first['id']}")
    assert r.status_code == 200
    item = r.json()
    # все поля, нужные адаптеру (contracts.md, раздел 4)
    for key in ("id", "deal_type", "title", "price", "currency", "country", "city",
                "address", "rooms", "area", "floor", "description", "images",
                "published_at", "url", "seller"):
        assert key in item
    assert item["price"] > 0 and item["rooms"] >= 0

    r = client.get("/item/unknown-id")
    assert r.status_code == 404


def test_cyrillic_and_html_in_data():
    item = client.get("/item/seed-000001").json()
    assert re.search(r"[а-яёА-ЯЁ]", item["title"])
    assert re.search(r"[а-яёА-ЯЁ]", item["address"])
    assert re.search(r"[а-яёА-ЯЁ]", item["description"])
    # в описании части набора встречается HTML — для проверки очистки у адаптера
    found_html = any("<" in client.get(f"/item/{i['id']}").json()["description"]
                     for i in client.get("/search/listings", params={"size": 100}).json()["items"])
    assert found_html
