"""Веб-страницы (ФТ-1.7, НФТ-15): Jinja2 + HTMX.

Две страницы: список с фильтрами и карточка со ссылкой на источник.
Данные ходят только через Core API — прямого доступа к PostgreSQL нет
(ФТ-1.7). Записи с source=mock помечаются как синтетические.
"""

import logging
from pathlib import Path
from urllib.parse import urlencode

import httpx
from fastapi import APIRouter, Request
from fastapi.responses import HTMLResponse, RedirectResponse
from fastapi.templating import Jinja2Templates

logger = logging.getLogger(__name__)

templates = Jinja2Templates(
    directory=str(Path(__file__).resolve().parent.parent / "templates")
)

router = APIRouter(tags=["ui"])

# Имена совпадают с ListingFilters core-api (contracts.md §3).
_FILTER_FIELDS = (
    "deal_type",
    "country",
    "city",
    "rooms",
    "price_min",
    "price_max",
    "area_min",
    "area_max",
    "source",
    "sort",
)
_DEAL_TYPE_LABELS = {"rent": "аренда", "sale": "продажа"}
_SORT_LABELS = {
    "date_desc": "дата, новые сверху",
    "date_asc": "дата, старые сверху",
    "price_asc": "цена, дешевле сверху",
    "price_desc": "цена, дороже сверху",
}
_PAGE_SIZE = 20


@router.get("/", include_in_schema=False)
async def index() -> RedirectResponse:
    return RedirectResponse("/listings")


@router.get("/listings", response_class=HTMLResponse)
async def listings_page(request: Request) -> HTMLResponse:
    filters = {
        field: value
        for field in _FILTER_FIELDS
        if (value := request.query_params.get(field)) not in (None, "")
    }
    page = _as_int(request.query_params.get("page"), default=1, minimum=1)
    size = _as_int(
        request.query_params.get("size"),
        default=_PAGE_SIZE,
        minimum=1,
        maximum=100,
    )
    items, total, error = await _fetch_listings(
        request, {**filters, "page": page, "size": size}
    )
    for item in items:
        item["deal_type_label"] = _DEAL_TYPE_LABELS.get(
            item.get("deal_type"), item.get("deal_type")
        )

    pager_filters = dict(filters)
    if size != _PAGE_SIZE:
        pager_filters["size"] = str(size)
    context = {
        "items": items,
        "total": total,
        "error": error,
        "page": page,
        "size": size,
        "total_pages": max(1, -(-total // size)),
        "filters": filters,
        "deal_type_labels": _DEAL_TYPE_LABELS,
        "sort_labels": _SORT_LABELS,
        "pager_query": urlencode(pager_filters),
    }
    template_name = (
        "_results.html"
        if request.headers.get("HX-Request") == "true"
        else "listings.html"
    )
    return templates.TemplateResponse(request, template_name, context)


@router.get("/listings/{listing_id}", response_class=HTMLResponse)
async def listing_card_page(request: Request, listing_id: int) -> HTMLResponse:
    settings = request.app.state.settings
    item: dict | None = None
    error: str | None = None
    status_code = 200
    try:
        response = await request.app.state.http.get(
            f"{settings.core_api_url}/api/v1/listings/{listing_id}"
        )
        if response.status_code == 404:
            error = (
                f"Объявление {listing_id} не найдено — возможно, ещё не собрано "
                "или удалено."
            )
            status_code = 404
        else:
            response.raise_for_status()
            item = response.json()
    except (httpx.HTTPError, ValueError, TypeError) as exc:
        logger.warning("Карточка %s не получена: %s", listing_id, exc)
        error = "Core API недоступен — карточка не получена. Попробуйте позже."
        status_code = 503

    source_url = None
    if item and str(item.get("url") or "").lower().startswith(("http://", "https://")):
        source_url = item["url"]
    return templates.TemplateResponse(
        request,
        "listing_card.html",
        {
            "item": item,
            "error": error,
            "source_url": source_url,
            "deal_type_label": _DEAL_TYPE_LABELS.get(item.get("deal_type"))
            if item
            else None,
        },
        status_code=status_code,
    )


async def _fetch_listings(
    request: Request, params: dict
) -> tuple[list[dict], int, str | None]:
    settings = request.app.state.settings
    try:
        response = await request.app.state.http.get(
            f"{settings.core_api_url}/api/v1/listings", params=params
        )
        response.raise_for_status()
        data = response.json()
        return data.get("items") or [], int(data.get("total") or 0), None
    except (httpx.HTTPError, ValueError, TypeError) as exc:
        logger.warning("Список объявлений не получен: %s", exc)
        return (
            [],
            0,
            "Core API недоступен — список объявлений не получен. Попробуйте позже.",
        )


def _as_int(
    raw: str | None, default: int, minimum: int, maximum: int | None = None
) -> int:
    if raw is None:
        return default
    try:
        value = int(raw)
    except ValueError:
        return default
    value = max(minimum, value)
    if maximum is not None:
        value = min(maximum, value)
    return value
