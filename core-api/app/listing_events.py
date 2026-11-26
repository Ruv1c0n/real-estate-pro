from dataclasses import dataclass
from datetime import datetime
from typing import Literal
from uuid import UUID, uuid4

from app.listing_store import StoredListing


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
