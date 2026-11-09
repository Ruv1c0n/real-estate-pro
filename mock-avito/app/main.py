"""mock-avito — имитация внешнего источника объявлений (contracts.md, раздел 4).

Состояние в памяти, без БД; рестарт пересоздаёт набор из фиксированного seed.
Collector в mock-режиме опрашивает этот сервис тем же кодом, что и боевой
источник (AvitoApiAdapter), — меняется только MOCK_SOURCE_URL.

MOCK_PUBLISH_MODE=batch — полный набор с постоянными ID;
stream — фоновая задача добавляет новые объявления каждые 5–30 с
с уникальными ID (contracts.md, раздел 3).

Управление на лету (режим, хаос, задержка, rate limit, «плохие данные») —
через /admin (консоль: GET /admin/ui), см. app/admin.py.
"""

from __future__ import annotations

import asyncio
import os
import random
import time
from contextlib import asynccontextmanager
from datetime import datetime, timezone
from pathlib import Path

from fastapi import FastAPI, HTTPException, Query, Request
from fastapi.exceptions import RequestValidationError, StarletteHTTPException
from fastapi.responses import JSONResponse
from fastapi.staticfiles import StaticFiles

from .state import state
from .admin import router as admin_router
from .generator import generate_batch, generate_stream_item, make_broken, mix_broken  # noqa: F401 (mix_broken — для app.admin)

MOCK_COUNT = int(os.getenv("MOCK_COUNT", "520"))
# Интервал публикации в stream-режиме; сужается тестами, контракт — 5–30 с.
MOCK_STREAM_MIN_SEC = float(os.getenv("MOCK_STREAM_MIN_SEC", "5"))
MOCK_STREAM_MAX_SEC = float(os.getenv("MOCK_STREAM_MAX_SEC", "30"))
# Поток не безразмерный: после максимума старейшие объявления вытесняются.
MOCK_STREAM_MAX_ITEMS = int(os.getenv("MOCK_STREAM_MAX_ITEMS", "5000"))

if state["mode"] not in ("batch", "stream"):
    raise SystemExit(f"unsupported MOCK_PUBLISH_MODE: {state['mode']}")

items: list[dict] = generate_batch(MOCK_COUNT, state["seed"])
items_by_id: dict[str, dict] = {it["id"]: it for it in items}

stream_rng = random.Random(state["seed"] + 1)
stream_seq = 0


def _publish_one() -> dict:
    global stream_seq
    stream_seq += 1
    item = generate_stream_item(stream_seq, stream_rng)
    bad = state["bad_data"]
    if bad["enabled"] and stream_rng.random() < bad["rate"]:
        item = make_broken(item)
    items.append(item)
    items_by_id[item["id"]] = item
    while len(items) > MOCK_STREAM_MAX_ITEMS:
        old = items.pop(0)
        items_by_id.pop(old["id"], None)
    state["publications"] += 1
    state["feed"].appendleft(item)
    return item


def reset_items(seed: int) -> None:
    """Пересоздаёт набор из seed (POST /admin/reset), подмешивая bad data."""
    global stream_seq
    batch = generate_batch(MOCK_COUNT, seed)
    bad = state["bad_data"]
    if bad["enabled"]:
        batch = mix_broken(batch, bad["rate"])
    items.clear()
    items.extend(batch)
    items_by_id.clear()
    items_by_id.update({it["id"]: it for it in items})
    stream_seq = 0
    state["seed"] = seed
    state["publications"] = 0
    state["feed"].clear()


async def _publish_loop() -> None:
    # Живёт всегда; публикует только в режиме stream.
    while True:
        await asyncio.sleep(stream_rng.uniform(MOCK_STREAM_MIN_SEC, MOCK_STREAM_MAX_SEC))
        if state["mode"] == "stream":
            _publish_one()


@asynccontextmanager
async def lifespan(_app: FastAPI):
    task = asyncio.create_task(_publish_loop())
    yield
    task.cancel()


app = FastAPI(title="mock-avito", version="0.2.0", lifespan=lifespan)

app.mount("/static", StaticFiles(directory=Path(__file__).parent / "static"), name="static")
app.include_router(admin_router)

LIMITED_PATHS = {"/search/listings", "/item"}


def _fail_response(fail: int) -> JSONResponse:
    if fail == 429:
        return JSONResponse(
            status_code=429,
            content={"error": "rate_limited", "message": "mock-avito: too many requests"},
            headers={"Retry-After": "2"},
        )
    return JSONResponse(status_code=500, content={"error": "internal_error", "message": "mock-avito chaos"})


@app.middleware("http")
async def ops_middleware(request: Request, call_next):
    # Метрики запросов.
    if request.url.path.startswith("/search/listings"):
        state["requests"]["search"] += 1
    elif request.url.path.startswith("/item"):
        state["requests"]["item"] += 1
    elif request.url.path.startswith("/admin"):
        state["requests"]["admin"] += 1

    limited = any(request.url.path.startswith(p) for p in LIMITED_PATHS)
    if limited:
        # Искусственная задержка (тест таймаутов collector'а).
        delay_s = state["delay_ms"] / 1000
        if delay_s > 0:
            await asyncio.sleep(delay_s)
        # Глобальный rate limit: скользящее окно в 60 с.
        limit = state["rate_per_minute"]
        now = time.time()
        while state["rate_window"] and now - state["rate_window"][0] > 60:
            state["rate_window"].popleft()
        if limit and len(state["rate_window"]) >= limit:
            state["limited_responses"] += 1
            retry = max(1, int(60 - (now - state["rate_window"][0])) + 1)
            return JSONResponse(
                status_code=429,
                content={"error": "rate_limited", "message": "mock-avito: rate limit exceeded"},
                headers={"Retry-After": str(retry)},
            )
        state["rate_window"].append(now)
        # Глобальный хаос: каждый N-й запрос возвращает ошибку.
        if state["chaos_code"]:
            state["chaos_counter"] += 1
            if state["chaos_counter"] % state["chaos_every"] == 0:
                return _fail_response(state["chaos_code"])

    response = await call_next(request)
    return response


def json_error(status: int, error: str, message: str) -> JSONResponse:
    return JSONResponse(status_code=status, content={"error": error, "message": message})


@app.exception_handler(RequestValidationError)
async def validation_error_handler(request: Request, exc: RequestValidationError) -> JSONResponse:
    # Формат ошибок JSON API — {error, message}, как в contracts.md.
    details = "; ".join(
        f"{'.'.join(map(str, e['loc'][1:]))}: {e['msg']}" for e in exc.errors()[:3]
    )
    return json_error(422, "validation_error", details)


@app.exception_handler(StarletteHTTPException)
async def http_error_handler(request: Request, exc: StarletteHTTPException) -> JSONResponse:
    names = {404: "not_found", 403: "forbidden", 401: "unauthorized", 503: "unavailable"}
    return json_error(exc.status_code, names.get(exc.status_code, "error"), str(exc.detail))


@app.get("/health")
def health():
    return {
        "status": "ok",
        "mode": state["mode"],
        "total": len(items),
        "publications": state["publications"],
        "uptime_s": int(time.time() - state["started_at"]),
    }


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
        if fail not in (429, 500):
            return json_error(422, "validation_error", "fail must be one of (429, 500)")
        left = state["chaos_left"].get(fail)
        if left is None:
            state["chaos_left"][fail] = fail_times - 1
            return _fail_response(fail)
        if left > 0:
            state["chaos_left"][fail] = left - 1
            return _fail_response(fail)
        del state["chaos_left"][fail]  # серия исчерпана — дальше успешные ответы

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
    found = sorted(found, key=lambda it: datetime.fromisoformat(it["published_at"]), reverse=True)
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
