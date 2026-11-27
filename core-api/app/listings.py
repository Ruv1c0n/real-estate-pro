from decimal import Decimal
from functools import partial
from typing import Annotated

import simplejson
from fastapi import APIRouter, Depends, HTTPException, Query, Request, Response, status
from pydantic import BaseModel, WithJsonSchema

from app.listing_cache import ListingPageCache
from app.listing_filters import ListingFilters
from app.listing_models import ListingCreate
from app.listing_store import ListingStore, StoredListing, UnknownListingReferenceError

router = APIRouter(prefix="/api/v1/listings", tags=["listings"])


class ListingCreateResponse(BaseModel):
    id: int
    created: bool


JsonDecimal = Annotated[
    Decimal,
    WithJsonSchema({"type": "number"}, mode="serialization"),
]


class ListingDetail(StoredListing):
    price: JsonDecimal
    area_m2: JsonDecimal | None = None


class ListingPage(BaseModel):
    items: list[ListingDetail]
    total: int
    page: int
    size: int


def _listing_data(listing: StoredListing) -> dict[str, object]:
    data = listing.model_dump(mode="json", exclude={"price", "area_m2"})
    data["price"] = listing.price
    data["area_m2"] = listing.area_m2
    return data


async def get_listing_store(request: Request) -> ListingStore:
    return request.app.state.listing_store


@router.post(
    "",
    status_code=status.HTTP_201_CREATED,
    responses={
        200: {
            "model": ListingCreateResponse,
            "description": "Объявление с таким source и external_id уже существует",
        },
        413: {"description": "Тело запроса превышает 128 КиБ"},
        422: {
            "description": "Ошибка валидации объявления",
            "content": {
                "application/json": {
                    "schema": {
                        "type": "object",
                        "required": ["error", "message"],
                        "properties": {
                            "error": {"type": "string"},
                            "message": {"type": "string"},
                        },
                    }
                }
            },
        },
    },
)
async def create_listing(
    listing: ListingCreate,
    response: Response,
    store: Annotated[ListingStore, Depends(get_listing_store)],
) -> ListingCreateResponse:
    try:
        result = await store.create_or_get(listing)
    except UnknownListingReferenceError as exc:
        raise HTTPException(
            status_code=status.HTTP_422_UNPROCESSABLE_CONTENT,
            detail=str(exc),
        ) from exc

    if not result.created:
        response.status_code = status.HTTP_200_OK

    return ListingCreateResponse(id=result.listing.id, created=result.created)


@router.get(
    "/{listing_id}",
    response_model=ListingDetail,
    responses={404: {"description": "Объявление не найдено"}},
)
async def get_listing(
    listing_id: int,
    store: Annotated[ListingStore, Depends(get_listing_store)],
) -> Response:
    listing = await store.get(listing_id)
    if listing is None:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail="Объявление не найдено",
        )

    return Response(
        content=simplejson.dumps(
            _listing_data(listing), use_decimal=True, ensure_ascii=False
        ),
        media_type="application/json",
    )


async def _load_listing_page(store: ListingStore, filters: ListingFilters) -> str:
    result = await store.search(filters)
    data = {
        "items": [_listing_data(listing) for listing in result.items],
        "total": result.total,
        "page": filters.page,
        "size": filters.size,
    }
    return simplejson.dumps(data, use_decimal=True, ensure_ascii=False)


@router.get("", response_model=ListingPage)
async def list_listings(
    request: Request,
    filters: Annotated[ListingFilters, Query()],
    store: Annotated[ListingStore, Depends(get_listing_store)],
) -> Response:
    cache: ListingPageCache | None = getattr(
        request.app.state, "listing_page_cache", None
    )
    load = partial(_load_listing_page, store, filters)
    body = await load() if cache is None else await cache.get_or_load(filters, load)
    return Response(content=body, media_type="application/json")
