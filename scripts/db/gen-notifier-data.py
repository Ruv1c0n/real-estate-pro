#!/usr/bin/env python3
"""Генератор тестовых данных для схемы notifier (анализ запросов, ТЗ 6.1).

Заполняет notifier.users / subscriptions / notifications_log синтетикой
с реалистичным профилем: подписки 0..10 на пользователя (лимит ФТ-3.x),
журнал с распределением статусов и режимов, очередь deferred.

Использование (из корня репо, python из .venv):
    DATABASE_URL=postgresql://app:app@127.0.0.1:5432/realestate \
        .venv/Scripts/python -I scripts/db/gen-notifier-data.py
Параметры: --users N (5000), --subs-per-user (avg 4.8), --log N (150000),
    --seed S, --truncate.

Данные идут напрямую в БД (мимо бота) — инструмент анализа, не для демо.
"""

import argparse
import asyncio
import json
import os
import random
import uuid
from datetime import UTC, datetime, timedelta

import asyncpg

CITIES = [
    ("BY", "Minsk"),
    ("BY", "Brest"),
    ("RU", "Moscow"),
    ("RU", "Saint Petersburg"),
    ("PL", "Warsaw"),
    ("PL", "Krakow"),
    ("KZ", "Astana"),
    ("KZ", "Almaty"),
    ("GE", "Tbilisi"),
    ("GE", "Batumi"),
]
STATUSES = ["sent"] * 75 + ["unknown"] * 5 + ["failed"] * 5 + ["deferred"] * 15


def _event_payload(rng: random.Random, listing_id: int) -> dict:
    country, city = rng.choice(CITIES)
    return {
        "event_id": str(uuid.uuid4()),
        "id": listing_id,
        "source": "mock",
        "deal_type": rng.choice(("rent", "sale")),
        "country": country,
        "city": city,
        "rooms": rng.randint(0, 3),
        "price": rng.randint(150, 1400),
        "currency": "USD",
        "price_usd": rng.randint(150, 1400),
        "address": f"ул. Тестовая, {rng.randint(1, 99)}",
        "url": f"https://example.com/mock/{listing_id}",
        "created_at": "2026-11-17T00:00:00Z",
    }


async def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--users", type=int, default=5000)
    parser.add_argument("--log", type=int, default=150000)
    parser.add_argument("--seed", type=int, default=2026)
    parser.add_argument("--truncate", action="store_true")
    args = parser.parse_args()

    rng = random.Random(args.seed)
    dsn = os.environ["DATABASE_URL"].replace("postgresql+asyncpg://", "postgresql://")
    conn = await asyncpg.connect(dsn)

    if args.truncate:
        await conn.execute(
            "TRUNCATE notifier.notifications_log,"
            " notifier.subscriptions, notifier.users CASCADE"
        )

    # Пользователи: chat_id — реалистичные Telegram-числа
    chat_ids = [10**9 + rng.randint(10**8, 9 * 10**8) for _ in range(args.users)]
    await conn.copy_records_to_table(
        "users",
        schema_name="notifier",
        columns=["chat_id"],
        records=[(c,) for c in chat_ids],
    )

    # Подписки: 90% пользователей подписаны, по 0..10 (лимит 10)
    subs = []
    for chat_id in chat_ids:
        if rng.random() < 0.10:
            continue
        for _ in range(rng.randint(1, 10)):
            country, city = rng.choice(CITIES)
            deal = rng.choice(("rent", "sale", None))
            rooms = rng.choice((0, 1, 2, 3, None))
            min_p = rng.choice((None, rng.randint(100, 900)))
            max_p = rng.choice((None, rng.randint(1000, 5000)))
            # CHECK subscriptions_not_empty_chk: хотя бы один параметр
            if deal is None and rooms is None and min_p is None and max_p is None:
                deal = rng.choice(("rent", "sale"))
            subs.append((chat_id, deal, country, city, rooms, min_p, max_p))
    await conn.copy_records_to_table(
        "subscriptions",
        schema_name="notifier",
        columns=[
            "chat_id",
            "deal_type",
            "country",
            "city",
            "rooms",
            "min_price_usd",
            "max_price_usd",
        ],
        records=subs,
    )

    # Журнал: журнал на 30 дней, статусы/режимы по профилю демо-нагрузки.
    # Пары (chat_id, listing_id) уникальны — UNIQUE(chat_id, listing_id)
    now = datetime.now(UTC)
    seen_pairs: set[tuple[int, int]] = set()
    log_rows = []
    while len(log_rows) < args.log:
        chat_id = rng.choice(chat_ids)
        listing_id = rng.randint(1, 300000)
        if (chat_id, listing_id) in seen_pairs:
            continue
        seen_pairs.add((chat_id, listing_id))
        status = rng.choice(STATUSES)
        mode = rng.choice(("telegram", "log"))
        payload = _event_payload(rng, listing_id)
        created = now - timedelta(minutes=rng.randint(0, 60 * 24 * 30))
        if status in ("sent", "unknown", "failed"):
            sent_at = created + timedelta(seconds=rng.randint(1, 600))
            # NOT NULL: для завершённых — момент обработки
            available_at = sent_at
        else:
            sent_at = None
            # deferred: backoff из профиля retry (1/2/5/15 минут)
            available_at = created + timedelta(minutes=rng.choice((1, 2, 5, 15)))
        log_rows.append(
            (
                chat_id,
                listing_id,
                payload["event_id"],
                status,
                mode,
                json.dumps(payload, ensure_ascii=False),
                created,
                sent_at,
                available_at,
            )
        )
    await conn.copy_records_to_table(
        "notifications_log",
        schema_name="notifier",
        columns=[
            "chat_id",
            "listing_id",
            "event_id",
            "status",
            "mode",
            "payload",
            "created_at",
            "sent_at",
            "available_at",
        ],
        records=log_rows,
    )

    stats = await conn.fetchrow(
        "SELECT (SELECT count(*) FROM notifier.users) AS users,"
        " (SELECT count(*) FROM notifier.subscriptions) AS subs,"
        " (SELECT count(*) FROM notifier.notifications_log) AS log,"
        " (SELECT count(*) FROM notifier.notifications_log"
        "   WHERE status = 'deferred') AS deferred"
    )
    print(
        f"users={stats['users']} subs={stats['subs']} log={stats['log']}"
        f" deferred={stats['deferred']}"
    )
    await conn.close()


if __name__ == "__main__":
    asyncio.run(main())
