"""mock-avito — имитация внешнего источника объявлений (contracts.md, раздел 4).

Состояние в памяти, без БД; рестарт пересоздаёт набор из фиксированного seed.
Collector в mock-режиме опрашивает этот сервис тем же кодом, что и боевой
источник (AvitoApiAdapter), — меняется только MOCK_SOURCE_URL.
"""

from __future__ import annotations

import os

from fastapi import FastAPI, HTTPException, Query, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from app.generator import generate_batch

MOCK_PUBLISH_MODE = os.getenv("MOCK_PUBLISH_MODE", "batch").lower()
MOCK_SEED = int(os.getenv("MOCK_SEED", "42"))
MOCK_COUNT = int(os.getenv("MOCK_COUNT", "520"))

# MOCK_PUBLISH_MODE=batch на этапе M1; stream добавляется в M2.
if MOCK_PUBLISH_MODE not in ("batch",):
    raise SystemExit(f"unsupported MOCK_PUBLISH_MODE: {MOCK_PUBLISH_MODE}")

items: list[dict] = generate_batch(MOCK_COUNT, MOCK_SEED)
items_by_id: dict[str, dict] = {it["id"]: it for it in items}

app = FastAPI(title="mock-avito", version="0.1.0")


def json_error(status: int, error: str, message: str) -> JSONResponse:
    return JSONResponse(status_code=status, content={"error": error, "message": message})


@app.exception_handler(RequestValidationError)
async def validation_error_handler(request: Request, exc: RequestValidationError) -> JSONResponse:
    # Формат ошибок JSON API — {error, message}, как в contracts.md.
    details = "; ".join(
        f"{'.'.join(map(str, e['loc'][1:]))}: {e['msg']}" for e in exc.errors()[:3]
    )
    return json_error(422, "validation_error", details)


@app.get("/health")
def health():
    return {"status": "ok", "mode": MOCK_PUBLISH_MODE, "total": len(items)}


@app.get("/search/listings")
def search_listings(
    page: int = Query(1, ge=1),
    size: int = Query(20, ge=1, le=100),
    deal_type: str | None = Query(None, pattern="^(rent|sale)$"),
    country: str | None = Query(None, min_length=2, max_length=2),
    city: str | None = None,
    price_min: int | None = Query(None, ge=0),
    price_max: int | None = Query(None, ge=0),
    rooms: int | None = Query(None, ge=0),
):
    found = items
    if deal_type:
        found = [it for it in found if it["deal_type"] == deal_type]
    if country:
        found = [it for it in found if it["country"] == country]
    if city:
        found = [it for it in found if it["city"] == city]
    if price_min is not None:
        found = [it for it in found if it["price"] >= price_min]
    if price_max is not None:
        found = [it for it in found if it["price"] <= price_max]
    if rooms is not None:
        found = [it for it in found if it["rooms"] == rooms]

    # Свежие объявления первыми — как на источниках публикаций.
    found = sorted(found, key=lambda it: it["published_at"], reverse=True)
    start = (page - 1) * size
    return {
        "items": found[start : start + size],
        "total": len(found),
        "page": page,
        "size": size,
    }


@app.get("/item/{item_id}")
def get_item(item_id: str):
    item = items_by_id.get(item_id)
    if item is None:
        raise HTTPException(status_code=404, detail="not found")
    return item
