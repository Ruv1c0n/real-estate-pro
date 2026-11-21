import asyncio
from collections.abc import Awaitable, Callable
from copy import deepcopy
from dataclasses import dataclass, replace
from datetime import UTC, datetime
from decimal import Decimal
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


def _in_range(
    value: int | Decimal | None,
    minimum: Decimal | None,
    maximum: Decimal | None,
) -> bool:
    if value is None:
        return minimum is None and maximum is None

    return (minimum is None or value >= minimum) and (
        maximum is None or value <= maximum
    )


def _matches_filters(listing: StoredListing, filters: ListingFilters) -> bool:
    for field in ("deal_type", "country", "city", "source", "rooms"):
        expected = getattr(filters, field)
        if expected is not None and getattr(listing, field) != expected:
            return False

    return _in_range(listing.price_usd, filters.price_min, filters.price_max) and (
        _in_range(listing.area_m2, filters.area_min, filters.area_max)
    )


class InMemoryListingStore:
    def __init__(self) -> None:
        self._lock = asyncio.Lock()
        self._listings: dict[int, StoredListing] = {}
        self._by_external_id: dict[tuple[str, str], int] = {}
        self._outbox: list[OutboxEvent] = []
        self._publish_lock = asyncio.Lock()
        self._next_unpublished = 0
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

    async def publish_next(
        self,
        publish: Callable[[dict[str, object]], Awaitable[None]],
    ) -> bool:
        async with self._publish_lock:
            async with self._lock:
                index = self._next_unpublished
                if index >= len(self._outbox):
                    return False

                event = self._outbox[index]
                payload = deepcopy(event.payload)

            await publish(payload)

            async with self._lock:
                self._outbox[index] = replace(event, published_at=datetime.now(UTC))
                self._next_unpublished += 1

            return True
