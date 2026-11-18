import asyncio
from copy import deepcopy
from dataclasses import dataclass
from datetime import UTC, datetime
from operator import attrgetter
from typing import Literal
from uuid import UUID, uuid4

from app.listing_filters import ListingFilters
from app.listing_models import ListingCreate
from app.listing_store import CreateListingResult, ListingSearchResult, StoredListing


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


def _matches_filters(listing: StoredListing, filters: ListingFilters) -> bool:
    if filters.deal_type is not None and listing.deal_type != filters.deal_type:
        return False
    if filters.country is not None and listing.country != filters.country:
        return False
    if filters.city is not None and listing.city != filters.city:
        return False
    if filters.source is not None and listing.source != filters.source:
        return False
    if filters.rooms is not None and listing.rooms != filters.rooms:
        return False

    if filters.price_min is not None and listing.price_usd < filters.price_min:
        return False
    if filters.price_max is not None and listing.price_usd > filters.price_max:
        return False

    area = listing.area_m2
    if area is None:
        return filters.area_min is None and filters.area_max is None

    return (filters.area_min is None or area >= filters.area_min) and (
        filters.area_max is None or area <= filters.area_max
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

    async def search(self, filters: ListingFilters) -> ListingSearchResult:
        async with self._lock:
            matches = [
                listing
                for listing in self._listings.values()
                if _matches_filters(listing, filters)
            ]

            sort_field = (
                "price_usd" if filters.sort.startswith("price_") else "created_at"
            )
            matches.sort(
                key=attrgetter(sort_field, "id"),
                reverse=filters.sort.endswith("_desc"),
            )

            total = len(matches)
            start = (filters.page - 1) * filters.size
            page_items = matches[start : start + filters.size]

            return ListingSearchResult(
                items=[listing.model_copy(deep=True) for listing in page_items],
                total=total,
            )
