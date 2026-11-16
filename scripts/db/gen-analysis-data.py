#!/usr/bin/env python3
"""Генератор тестовых объявлений для анализа запросов (НФТ-10, ТЗ 6.1).

Заполняет core.listings / listing_duplicates / outbox_events синтетикой
со реалистичным разбросом по городам, комнатам, ценам и площадям, плюс
доля «возможных дублей» и незавершённых outbox-заданий. Данные идут
напрямую в БД (в обход API) — это инструмент анализа, не для демо.

Использование (из корня репо, python из .venv):
    DATABASE_URL=postgresql+asyncpg://app:app@127.0.0.1:5433/realestate \
        .venv/Scripts/python -I scripts/db/gen-analysis-data.py
Параметры:
    --count N    объявлений (по умолчанию 520 — порог НФТ-10)
    --dup-share  доля пар-дублей от count (по умолчанию 0.08)
    --seed S     фиксированный seed для воспроизводимости
    --truncate   очистить listings-таблицы перед генерацией

content_hash повторяет формулу из миграции 0001
(sha256(city+country+address+rooms+area+deal_type)); конкретная
сериализация в contracts.md ещё не зафиксирована — для анализа не важно,
важно совпадение хэша у дублей.
"""

import argparse
import asyncio
import hashlib
import json
import os
import random
import uuid
from decimal import ROUND_HALF_UP, Decimal
from urllib.parse import quote

import asyncpg

CITIES = [
    # (country, city, каноническое имя, ценовой множитель, валюты)
    ("BY", "Minsk", 1.0, ("BYN", "USD")),
    ("BY", "Brest", 0.7, ("BYN", "USD")),
    ("RU", "Moscow", 1.6, ("RUB", "USD")),
    ("RU", "Saint Petersburg", 1.3, ("RUB", "USD")),
    ("PL", "Warsaw", 1.4, ("PLN", "EUR")),
    ("PL", "Krakow", 1.1, ("PLN", "EUR")),
    ("KZ", "Astana", 0.9, ("KZT", "USD")),
    ("KZ", "Almaty", 0.8, ("KZT", "USD")),
    ("GE", "Tbilisi", 1.0, ("GEL", "USD")),
    ("GE", "Batumi", 0.85, ("GEL", "USD")),
]
# Демонстрационные курсы из seed (миграция 0001, ТЗ 3.2)
RATES = {
    "USD": Decimal("1"),
    "BYN": Decimal("0.31"),
    "RUB": Decimal("0.011"),
    "PLN": Decimal("0.25"),
    "EUR": Decimal("1.10"),
    "KZT": Decimal("0.0021"),
    "GEL": Decimal("0.37"),
}
STREETS = [
    "ул. Ленина",
    "пр. Независимости",
    "ул. Пушкина",
    "ул. Садовая",
    "ул. Цветочная",
    "пр. Мира",
    "ул. Заводская",
    "ул. Советская",
    "бул. Победы",
    "ул. Строителей",
]
DEAL_TYPES = ("rent", "sale")


def content_hash(
    city: str,
    country: str,
    address: str,
    rooms: int,
    area: Decimal | None,
    deal_type: str,
) -> str:
    parts = [
        city,
        country,
        address,
        str(rooms),
        "" if area is None else str(area),
        deal_type,
    ]
    return hashlib.sha256("|".join(parts).encode()).hexdigest()


async def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--count", type=int, default=520)
    parser.add_argument("--dup-share", type=float, default=0.08)
    parser.add_argument("--seed", type=int, default=2026)
    parser.add_argument("--truncate", action="store_true")
    args = parser.parse_args()

    rng = random.Random(args.seed)

    dsn = os.environ.get("DATABASE_URL")
    if dsn:
        # DATABASE_URL в стиле SQLAlchemy (postgresql+asyncpg://) — asyncpg
        # ждёт postgresql:// ; конвертируем при необходимости
        dsn = dsn.replace("postgresql+asyncpg://", "postgresql://")
    else:
        user = os.environ.get("POSTGRES_USER", "app")
        password = quote(os.environ.get("POSTGRES_PASSWORD", "app"), safe="")
        host = os.environ.get("POSTGRES_HOST", "127.0.0.1")
        port = os.environ.get("POSTGRES_PORT", "5432")
        db = os.environ.get("POSTGRES_DB", "realestate")
        dsn = f"postgresql+asyncpg://{user}:{password}@{host}:{port}/{db}"

    conn = await asyncpg.connect(dsn)

    if args.truncate:
        await conn.execute(
            "TRUNCATE core.listing_duplicates, core.listing_photos,"
            " core.outbox_events, core.listings RESTART IDENTITY CASCADE"
        )

    sources = dict(await conn.fetch("SELECT code, id FROM core.sources"))
    cities = await conn.fetch(
        "SELECT id, country_code, name FROM core.cities ORDER BY id"
    )

    rows = []  # записи для listings
    dup_pairs = []  # (listing_a_external, listing_b_external, hash)

    n_base = args.count
    n_dup = int(n_base * args.dup_share)

    for i in range(n_base):
        _cc, city, mult, currencies = rng.choice(CITIES)
        country, city_id, city_name = next(
            (r["country_code"], r["id"], r["name"]) for r in cities if r["name"] == city
        )
        deal_type = rng.choice(DEAL_TYPES)
        rooms = rng.choice([0, 0, 1, 1, 1, 2, 2, 2, 3, 3])
        area = Decimal(str(rng.uniform(18.0, 120.0))).quantize(Decimal("0.01"))
        if rng.random() < 0.05:
            area = None  # площадь иногда неизвестна (ТЗ 3.3: без связывания)
        base_price_usd = int(rng.uniform(180, 1400) * mult * (1 + rooms * 0.25))
        if deal_type == "rent":
            base_price_usd = max(150, base_price_usd // 3)
        currency = rng.choice(currencies)
        price = (Decimal(base_price_usd) / RATES[currency]).quantize(
            Decimal("1"), rounding=ROUND_HALF_UP
        )
        price_usd = (price * RATES[currency]).quantize(
            Decimal("1"), rounding=ROUND_HALF_UP
        )
        floor = rng.randint(1, 16) if rng.random() > 0.1 else None
        address = (
            f"{rng.choice(STREETS)}, {rng.randint(1, 120)}"
            if rng.random() > 0.05
            else None
        )
        title = (
            (
                f"{rooms}-к, {area} м²"
                if rooms
                else f"студия, {area} м²"
                if area
                else None
            )
            if rng.random() > 0.08
            else None
        )
        source = rng.choice(("mock", "avito"))
        ext_id = f"gen-{args.seed}-{i:06d}"
        rows.append(
            {
                "source_id": sources[source],
                "external_id": ext_id,
                "url_hash": hashlib.sha256(f"{source}|{ext_id}".encode()).hexdigest(),
                "url": f"https://example.com/{source}/{i}",
                "deal_type": deal_type,
                "country_code": country,
                "city_id": city_id,
                "price": price,
                "currency_code": currency,
                "price_usd": int(price_usd),
                "rooms": rooms,
                "floor": floor,
                "title": title,
                "address": address,
                "description": "Синтетическое объявление для анализа запросов."
                if rng.random() < 0.5
                else None,
                "area_m2": area,
                "published_at": None,
                "content_hash": content_hash(
                    city_name, country, address or "", rooms, area, deal_type
                ),
            }
        )

    # Дубли: копия существующего объявления под другим source/external_id
    for j in range(n_dup):
        src = dict(rng.choice(rows))
        src["external_id"] = f"gen-{args.seed}-dup{j:05d}"
        src["source_id"] = sources["avito" if rng.random() < 0.5 else "mock"]
        src["url_hash"] = hashlib.sha256(
            f"{src['source_id']}|{src['external_id']}".encode()
        ).hexdigest()
        src["url"] = f"https://example.com/dup/{j}"
        dup_pairs.append(
            (rng.choice(rows)["external_id"], src["external_id"], src["content_hash"])
        )
        rows.append(src)

    cols = (
        "source_id, external_id, url_hash, url, deal_type, country_code,"
        " city_id, price, currency_code, price_usd, rooms, floor, title,"
        " address, description, area_m2, published_at, content_hash"
    )
    records = [tuple(r[c] for c in cols.replace(" ", "").split(",")) for r in rows]
    await conn.copy_records_to_table(
        "listings",
        schema_name="core",
        columns=cols.replace(" ", "").split(","),
        records=records,
    )

    # published_at: не storage-поле с DEFAULT — ставим частично (12% NULL)
    await conn.execute(
        "UPDATE core.listings SET published_at = now() - (random() * interval '30 days')"
        " WHERE published_at IS NULL AND random() > 0.12"
    )

    id_by_ext = dict(await conn.fetch("SELECT external_id, id FROM core.listings"))
    dup_records = sorted(
        (id_by_ext[a], id_by_ext[b], h)
        for a, b, h in dup_pairs
        if id_by_ext[a] != id_by_ext[b]
    )
    await conn.copy_records_to_table(
        "listing_duplicates",
        schema_name="core",
        columns=["listing_a_id", "listing_b_id", "content_hash"],
        records=dup_records,
    )

    # outbox: объявление + задание в одной транзакции (ТЗ 4.3); 30% заданий
    # оставляем unpublished — имитация очереди фоновой публикации
    outbox = []
    for r in rng.sample(rows, k=max(1, int(len(rows) * 0.6))):
        payload = {
            "event_id": str(uuid.uuid4()),
            "id": id_by_ext[r["external_id"]],
            "source": "mock",
            "deal_type": r["deal_type"],
            "country": r["country_code"],
            "city": next(c["name"] for c in cities if c["id"] == r["city_id"]),
            "rooms": r["rooms"],
            "price": float(r["price"]),
            "currency": r["currency_code"],
            "price_usd": r["price_usd"],
            "address": r["address"],
            "url": r["url"],
            "created_at": "2026-11-17T00:00:00Z",
        }
        outbox.append(
            (
                uuid.uuid4(),
                "listing_new",
                json.dumps(payload, ensure_ascii=False),
                id_by_ext[r["external_id"]],
                None,
            )
        )
    await conn.copy_records_to_table(
        "outbox_events",
        schema_name="core",
        columns=["event_id", "kind", "payload", "listing_id", "published_at"],
        records=outbox,
    )
    await conn.execute(
        "UPDATE core.outbox_events SET published_at = now() - (random() * interval '2 hours')"
        " WHERE published_at IS NULL AND random() > 0.30"
    )

    stats = await conn.fetchrow(
        "SELECT (SELECT count(*) FROM core.listings) AS listings,"
        " (SELECT count(*) FROM core.listing_duplicates) AS dups,"
        " (SELECT count(*) FROM core.outbox_events) AS outbox,"
        " (SELECT count(*) FROM core.outbox_events WHERE published_at IS NULL) AS pending"
    )
    print(
        f"listings={stats['listings']} dups={stats['dups']}"
        f" outbox={stats['outbox']} pending={stats['pending']}"
    )
    await conn.close()


if __name__ == "__main__":
    asyncio.run(main())
