import hashlib
from collections.abc import Awaitable, Callable

import simplejson
from sqlalchemy import text
from sqlalchemy.ext.asyncio import AsyncConnection, AsyncEngine

from app.listing_events import build_listing_event
from app.listing_filters import ListingFilters
from app.listing_models import ListingCreate
from app.listing_store import (
    CreateListingResult,
    ListingSearchResult,
    StoredListing,
    UnknownListingReferenceError,
)

_LISTING_SELECT = """
    SELECT
        l.id,
        s.code AS source,
        l.external_id,
        l.url,
        l.deal_type,
        l.country_code AS country,
        c.name AS city,
        l.price,
        l.currency_code AS currency,
        l.price_usd,
        l.rooms,
        l.floor,
        l.title,
        l.address,
        l.description,
        l.area_m2,
        l.published_at,
        l.created_at,
        (
            SELECT array_agg(p.url ORDER BY p.sort, p.id)
            FROM core.listing_photos AS p
            WHERE p.listing_id = l.id
        ) AS photos
    FROM core.listings AS l
    JOIN core.sources AS s ON s.id = l.source_id
    JOIN core.cities AS c ON c.id = l.city_id
"""

_GET_LISTING = text(_LISTING_SELECT + "\nWHERE l.id = :listing_id")

_INSERT_LISTING = text("""
    INSERT INTO core.listings (
        source_id, external_id, url_hash, url, deal_type, country_code,
        city_id, price, currency_code, price_usd, rooms, floor, title,
        address, description, area_m2, published_at, content_hash
    )
    SELECT
        s.id, :external_id, :url_hash, :url, :deal_type, c.country_code,
        c.id, :price, cur.code, :price_usd, :rooms, :floor, :title,
        :address, :description, :area_m2, :published_at, :content_hash
    FROM core.sources AS s
    CROSS JOIN core.cities AS c
    CROSS JOIN core.currencies AS cur
    WHERE s.code = :source
      AND c.country_code = :country
      AND c.name = :city
      AND cur.code = :currency
    ON CONFLICT (source_id, external_id) DO NOTHING
    RETURNING id
""")

_FIND_EXISTING_ID = text("""
    SELECT l.id
    FROM core.listings AS l
    JOIN core.sources AS s ON s.id = l.source_id
    WHERE s.code = :source AND l.external_id = :external_id
""")

_INSERT_PHOTO = text("""
    INSERT INTO core.listing_photos (listing_id, url, sort)
    VALUES (:listing_id, :url, :sort)
""")

_INSERT_EVENT = text("""
    INSERT INTO core.outbox_events (event_id, kind, listing_id, payload)
    VALUES (:event_id, :kind, :listing_id, CAST(CAST(:payload AS text) AS jsonb))
""")

_NEXT_PENDING_EVENT = text("""
    SELECT id, CAST(payload AS text) AS payload_json
    FROM core.outbox_events
    WHERE kind = 'listing_new' AND published_at IS NULL
    ORDER BY id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
""")

_MARK_EVENT_PUBLISHED = text("""
    UPDATE core.outbox_events
    SET published_at = clock_timestamp()
    WHERE id = :event_id
""")

_SEARCH_FILTERS = {
    "deal_type": "deal_type = :deal_type",
    "country": "country = :country",
    "city": "city = :city",
    "source": "source = :source",
    "rooms": "rooms = :rooms",
    "price_min": "price_usd >= CAST(:price_min AS numeric)",
    "price_max": "price_usd <= CAST(:price_max AS numeric)",
    "area_min": "area_m2 >= :area_min",
    "area_max": "area_m2 <= :area_max",
}

_SEARCH_ORDER = {
    "date_desc": "created_at DESC, id DESC",
    "date_asc": "created_at ASC, id ASC",
    "price_desc": "price_usd DESC, id DESC",
    "price_asc": "price_usd ASC, id ASC",
}


async def _get_listing(
    connection: AsyncConnection,
    listing_id: int,
) -> StoredListing | None:
    result = await connection.execute(_GET_LISTING, {"listing_id": listing_id})
    row = result.mappings().one_or_none()
    if row is None:
        return None

    return StoredListing.model_validate(dict(row))


class PostgresListingStore:
    def __init__(self, engine: AsyncEngine) -> None:
        self._engine = engine

    async def get(self, listing_id: int) -> StoredListing | None:
        if not 1 <= listing_id < 2 ** 63:
            return None

        async with self._engine.connect() as connection:
            return await _get_listing(connection, listing_id)

    async def create_or_get(self, listing: ListingCreate) -> CreateListingResult:
        params = listing.model_dump(exclude={"photos"})
        params["url"] = str(listing.url)

        # Историческое имя url_hash: SHA-256 от пары source|external_id.
        identity = f"{listing.source}|{listing.external_id}"

        # Для поиска возможных дублей: SHA-256 от city|country|address|rooms|area|deal_type.
        # Пустой адрес и отсутствующая площадь дают пустые поля, площадь имеет два знака.
        area = "" if listing.area_m2 is None else format(listing.area_m2, ".2f")
        content = "|".join(
            (
                listing.city,
                listing.country,
                listing.address or "",
                str(listing.rooms),
                area,
                listing.deal_type,
            )
        )
        params["url_hash"] = hashlib.sha256(identity.encode("utf-8")).hexdigest()
        params["content_hash"] = hashlib.sha256(content.encode("utf-8")).hexdigest()

        async with self._engine.begin() as connection:
            inserted = await connection.execute(_INSERT_LISTING, params)
            listing_id = inserted.scalar_one_or_none()
            created = listing_id is not None

            if not created:
                existing = await connection.execute(
                    _FIND_EXISTING_ID,
                    {"source": listing.source, "external_id": listing.external_id},
                )
                listing_id = existing.scalar_one_or_none()
                if listing_id is None:
                    raise UnknownListingReferenceError(
                        "Неизвестный источник, город для указанной страны или валюта"
                    )

            if created and listing.photos:
                await connection.execute(
                    _INSERT_PHOTO,
                    [
                        {"listing_id": listing_id, "url": str(url), "sort": position}
                        for position, url in enumerate(listing.photos)
                    ],
                )

            stored = await _get_listing(connection, listing_id)
            if stored is None:
                raise RuntimeError("Не удалось прочитать объявление внутри транзакции")

            if created:
                event = build_listing_event(stored)
                await connection.execute(
                    _INSERT_EVENT,
                    {
                        "event_id": event.event_id,
                        "kind": event.kind,
                        "listing_id": listing_id,
                        "payload": simplejson.dumps(event.payload, use_decimal=True),
                    },
                )

            return CreateListingResult(listing=stored, created=created)

    async def publish_next(
        self,
        publish: Callable[[dict[str, object]], Awaitable[None]],
    ) -> bool:
        async with self._engine.begin() as connection:
            result = await connection.execute(_NEXT_PENDING_EVENT)
            row = result.mappings().one_or_none()
            if row is None:
                return False

            payload = simplejson.loads(row["payload_json"], use_decimal=True)
            await publish(payload)

            await connection.execute(_MARK_EVENT_PUBLISHED, {"event_id": row["id"]})
            return True

    async def search(self, filters: ListingFilters) -> ListingSearchResult:
        # В PostgreSQL rooms хранится как integer.
        if filters.rooms is not None and filters.rooms > 2_147_483_647:
            return ListingSearchResult(items=[], total=0)
        params = filters.model_dump(include=set(_SEARCH_FILTERS), exclude_none=True)
        conditions = [_SEARCH_FILTERS[name] for name in params]

        source_sql = f" FROM ({_LISTING_SELECT}) AS listing"
        if conditions:
            source_sql += " WHERE " + " AND ".join(conditions)

        count_query = text("SELECT count(*)" + source_sql)
        page_query = text(
            "SELECT listing.*"
            + source_sql
            + " ORDER BY "
            + _SEARCH_ORDER[filters.sort]
            + " LIMIT :page_size OFFSET :page_offset"
        )

        offset = (filters.page - 1) * filters.size

        async with self._engine.connect() as connection:
            await connection.execution_options(isolation_level="REPEATABLE READ")

            total = (await connection.execute(count_query, params)).scalar_one()
            if offset >= total:
                return ListingSearchResult(items=[], total=total)

            result = await connection.execute(
                page_query,
                {**params, "page_size": filters.size, "page_offset": offset},
            )
            items = [
                StoredListing.model_validate(dict(row)) for row in result.mappings()
            ]
            return ListingSearchResult(items=items, total=total)
