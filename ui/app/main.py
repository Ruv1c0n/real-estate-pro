import os
from urllib.parse import urlencode

import httpx
from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import HTMLResponse
from fastapi.staticfiles import StaticFiles
from fastapi.templating import Jinja2Templates

CORE_API_URL = os.getenv("CORE_API_URL", "http://core-api:8000")
PAGE_SIZE = 20

app = FastAPI(title="Listings UI")
app.mount("/static", StaticFiles(directory="app/static"), name="static")
templates = Jinja2Templates(directory="app/templates")

CURRENCY_SIGNS = {"USD": "$", "EUR": "€", "RUB": "₽", "KZT": "₸", "PLN": "zł",
                  "GEL": "₾", "TRY": "₺", "AED": "AED", "BYN": "BYN"}


def money(value, currency: str = "USD") -> str:
    try:
        n = f"{float(value):,.0f}".replace(",", "\u202f")
    except (TypeError, ValueError):
        return "—"
    sign = CURRENCY_SIGNS.get(currency, currency)
    return f"{sign}\u202f{n}" if currency == "USD" else f"{n}\u202f{sign}"


def rooms_label(rooms) -> str:
    return "Студия" if rooms == 0 else f"{rooms}-к"


templates.env.filters["money"] = money
templates.env.filters["rooms"] = rooms_label


@app.get("/", response_class=HTMLResponse)
async def index(
    request: Request,
    country: str | None = None,
    city: str | None = None,
    deal_type: str | None = None,
    rooms: int | None = None,
    price_max: float | None = None,
    page: int = 1,
):
    filters = {k: v for k, v in {
        "country": country, "city": city, "deal_type": deal_type,
        "rooms": rooms, "price_max": price_max,
    }.items() if v not in (None, "")}
    params = {**filters, "page": max(page, 1), "page_size": PAGE_SIZE}

    error = None
    data = {"items": [], "total": 0}
    try:
        async with httpx.AsyncClient(timeout=5) as client:
            r = await client.get(f"{CORE_API_URL}/api/v1/listings", params=params)
            r.raise_for_status()
            data = r.json()
    except httpx.HTTPError as e:
        error = f"Core API недоступен ({e.__class__.__name__}). Проверьте, что сервис core-api запущен."

    total = data.get("total", 0)
    pages = max((total + PAGE_SIZE - 1) // PAGE_SIZE, 1)
    return templates.TemplateResponse(request, "index.html", {
        "items": data.get("items", []),
        "total": total,
        "page": max(page, 1),
        "pages": pages,
        "filters": filters,
        "base_qs": urlencode(filters),
        "error": error,
    })


@app.get("/listing/{listing_id}", response_class=HTMLResponse)
async def listing(request: Request, listing_id: int):
    try:
        async with httpx.AsyncClient(timeout=5) as client:
            r = await client.get(f"{CORE_API_URL}/api/v1/listings/{listing_id}")
    except httpx.HTTPError:
        raise HTTPException(502, "Core API недоступен")
    if r.status_code == 404:
        raise HTTPException(404, "Объявление не найдено")
    r.raise_for_status()
    return templates.TemplateResponse(request, "listing.html", {"item": r.json()})


@app.get("/health")
async def health():
    return {"status": "ok"}
