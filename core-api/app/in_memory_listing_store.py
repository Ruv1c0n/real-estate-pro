import asyncio
from copy import deepcopy
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Literal
from uuid import UUID, uuid4

from app.listing_models import ListingCreate
from app.listing_store import CreateListingResult, StoredListing


@dataclass(frozen=True)
class OutboxEvent:
    event_id: UUID
    listing_id: int
    kind: Literal["listing_new"]
    payload: dict[str, object]
    created_at: datetime
    published_at: datetime | None = None


def build_listing_event(listing: StoredListing) -> OutboxEvent:
    event_id = uuid4()

    return OutboxEvent(
        event_id=event_id,
        listing_id=listing.id,
        kind="listing_new",
        payload={
            "event_id": str(event_id),
            "id": listing.id,
            "source": listing.source,
            "deal_type": listing.deal_type,
            "country": listing.country,
            "city": listing.city,
            "rooms": listing.rooms,
            "price": listing.price,
            "currency": listing.currency,
            "price_usd": listing.price_usd,
            "address": listing.address,
            "url": str(listing.url),
            "created_at": listing.created_at.isoformat(),
        },
        created_at=listing.created_at,
    )


class InMemoryListingStore:
    def __init__(self) -> None:
        self._lock = asyncio.Lock()
        self._listings: dict[int, StoredListing] = {}
        self._by_external_id: dict[tuple[str, str], int] = {}
        self._outbox: list[OutboxEvent] = []
        self._next_id = 1

    async def create_or_get(
        self,
        listing: ListingCreate,
    ) -> CreateListingResult:
        async with self._lock:
            key = (listing.source, listing.external_id)
            existing_id = self._by_external_id.get(key)

            if existing_id is not None:
                return CreateListingResult(
                    listing=self._listings[existing_id].model_copy(deep=True),
                    created=False,
                )

            stored = StoredListing(
                **listing.model_dump(),
                id=self._next_id,
                created_at=datetime.now(UTC),
            )
            event = build_listing_event(stored)

            result = CreateListingResult(
                listing=stored.model_copy(deep=True),
                created=True,
            )

            self._listings[stored.id] = stored
            self._by_external_id[key] = stored.id
            self._outbox.append(event)
            self._next_id += 1

            return result

    async def get(self, listing_id: int) -> StoredListing | None:
        async with self._lock:
            listing = self._listings.get(listing_id)
            if listing is None:
                return None
            return listing.model_copy(deep=True)

    async def outbox_snapshot(self) -> list[OutboxEvent]:
        """Вернуть независимую копию outbox для проверок."""
        async with self._lock:
            return deepcopy(self._outbox)
