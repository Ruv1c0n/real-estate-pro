"""ВРЕМЕННАЯ заглушка Core API (хранит данные в памяти).
Нужна, чтобы проверить collector и ui, пока нет настоящего API Ильи.
"""
from fastapi import FastAPI, HTTPException, Response

app = FastAPI(title="Fake Core API")
DB: list[dict] = []
URLS: set[str] = set()


@app.post("/api/v1/listings", status_code=201)
def create(item: dict, response: Response):
    if item["url_hash"] in URLS:
        response.status_code = 409
        return {"detail": "duplicate"}
    item["id"] = len(DB) + 1
    DB.append(item)
    URLS.add(item["url_hash"])
    return item


@app.get("/api/v1/listings")
def list_listings(country: str | None = None, city: str | None = None,
                  deal_type: str | None = None, rooms: int | None = None,
                  price_max: float | None = None, page: int = 1, page_size: int = 20):
    res = [
        i for i in DB
        if (not country or i["country"] == country)
        and (not city or city.lower() in i["city"].lower())
        and (not deal_type or i["deal_type"] == deal_type)
        and (rooms is None or i["rooms"] == rooms)
        and (price_max is None or float(i["price_usd"]) <= price_max)
    ]
    start = (max(page, 1) - 1) * page_size
    return {"items": res[start:start + page_size], "total": len(res)}


@app.get("/api/v1/listings/{listing_id}")
def get_listing(listing_id: int):
    if not 1 <= listing_id <= len(DB):
        raise HTTPException(404, "not found")
    return DB[listing_id - 1]
