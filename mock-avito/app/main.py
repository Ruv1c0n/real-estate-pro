"""mock-avito — имитация внешнего источника объявлений (contracts.md, раздел 4).

Состояние в памяти, без БД; рестарт пересоздаёт набор из фиксированного seed.
Collector в mock-режиме опрашивает этот сервис тем же кодом, что и боевой
источник (AvitoApiAdapter), — меняется только MOCK_SOURCE_URL.

MOCK_PUBLISH_MODE=batch — полный набор с постоянными ID;
stream — фоновая задача добавляет новые объявления каждые 5–30 с
с уникальными ID (contracts.md, раздел 3).
"""

from __future__ import annotations

import asyncio
import os
import random
from contextlib import asynccontextmanager
from datetime import datetime, timezone

from fastapi import FastAPI, HTTPException, Query, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from app.generator import generate_batch, generate_stream_item

MOCK_PUBLISH_MODE = os.getenv("MOCK_PUBLISH_MODE", "batch").lower()
MOCK_SEED = int(os.getenv("MOCK_SEED", "42"))
MOCK_COUNT = int(os.getenv("MOCK_COUNT", "520"))
# Интервал публикации в stream-режиме; сужается тестами, контракт — 5–30 с.
MOCK_STREAM_MIN_SEC = float(os.getenv("MOCK_STREAM_MIN_SEC", "5"))
MOCK_STREAM_MAX_SEC = float(os.getenv("MOCK_STREAM_MAX_SEC", "30"))
# Поток не безразмерный: после максимума старейшие объявления вытесняются.
MOCK_STREAM_MAX_ITEMS = int(os.getenv("MOCK_STREAM_MAX_ITEMS", "5000"))

if MOCK_PUBLISH_MODE not in ("batch", "stream"):
    raise SystemExit(f"unsupported MOCK_PUBLISH_MODE: {MOCK_PUBLISH_MODE}")

items: list[dict] = generate_batch(MOCK_COUNT, MOCK_SEED)
items_by_id: dict[str, dict] = {it["id"]: it for it in items}

# Хаос-режим (contracts.md, раздел 4): ?fail=429|500&fail_times=N —
# N подряд запросов возвращают указанный код, затем успешный ответ.
FAIL_CODES = (429, 500)
chaos_left: dict[int, int] = {}

if MOCK_PUBLISH_MODE == "stream":
    stream_rng = random.Random(MOCK_SEED + 1)
    stream_seq = 0

    async def _publish_loop() -> None:
        global stream_seq
        while True:
            await asyncio.sleep(stream_rng.uniform(MOCK_STREAM_MIN_SEC, MOCK_STREAM_MAX_SEC))
            stream_seq += 1
            item = generate_stream_item(stream_seq, stream_rng)
            items.append(item)
            items_by_id[item["id"]] = item
            while len(items) > MOCK_STREAM_MAX_ITEMS:
                old = items.pop(0)
                items_by_id.pop(old["id"], None)

    @asynccontextmanager
    async def lifespan(_app: FastAPI):
        task = asyncio.create_task(_publish_loop())
        yield
        task.cancel()

    app = FastAPI(title="mock-avito", version="0.1.0", lifespan=lifespan)
else:
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


def chaos_response(fail: int, fail_times: int):
    """Возвращает код ошибки, если счётчик хаоса ещё активен, иначе None."""
    left = chaos_left.get(fail)
    if left is None:
        chaos_left[fail] = fail_times - 1
        return _fail_response(fail)
    if left > 0:
        chaos_left[fail] = left - 1
        return _fail_response(fail)
    del chaos_left[fail]  # серия исчерпана — дальше успешные ответы
    return None


def _fail_response(fail: int) -> JSONResponse:
    if fail == 429:
        return JSONResponse(
            status_code=429,
            content={"error": "rate_limited", "message": "chaos: too many requests"},
            headers={"Retry-After": "2"},
        )
    return JSONResponse(status_code=500, content={"error": "internal_error", "message": "chaos"})


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
    fail: int | None = None,
    fail_times: int = Query(3, ge=1, le=100),
):
    if fail is not None:
        if fail not in FAIL_CODES:
            return json_error(422, "validation_error", f"fail must be one of {FAIL_CODES}")
        forced = chaos_response(fail, fail_times)
        if forced is not None:
            return forced

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
    # published_at в данных со смещениями (+03:00 и др.): строковая
    # сортировка перепутала бы порядки, сравниваем как datetime.
    from datetime import datetime as _dt

    found = sorted(found, key=lambda it: _dt.fromisoformat(it["published_at"]), reverse=True)
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
