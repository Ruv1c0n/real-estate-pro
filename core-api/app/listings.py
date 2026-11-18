from typing import Annotated

from fastapi import APIRouter, Depends, Request, Response, status
from pydantic import BaseModel

from app.listing_models import ListingCreate
from app.listing_store import ListingStore

router = APIRouter(prefix="/api/v1/listings", tags=["listings"])


class ListingCreateResponse(BaseModel):
    id: int
    created: bool


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
    result = await store.create_or_get(listing)

    if not result.created:
        response.status_code = status.HTTP_200_OK

    return ListingCreateResponse(id=result.listing.id, created=result.created)
