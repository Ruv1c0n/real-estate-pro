"""Детерминированный генератор синтетических объявлений для mock-avito.

Требования ТЗ 3.4: >=500 объектов, >=5 стран, >=10 городов.
Города/валюты синхронизированы со справочником core-api (contracts.md,
раздел 3); при изменении справочника — править здесь и в core-api вместе.
"""

from __future__ import annotations

import random
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone

# Страна: (код ISO alpha-2, валюта, смещение UTC для published_at, города).
# Смещения — зимние; данные синтетические, точность таймзон не требуется.
COUNTRIES: list[dict] = [
    {"code": "BY", "currency": "BYN", "offset": "+03:00",
     "cities": ["Minsk", "Brest", "Grodno"]},
    {"code": "RU", "currency": "RUB", "offset": "+03:00",
     "cities": ["Moscow", "Saint Petersburg", "Kazan"]},
    {"code": "PL", "currency": "PLN", "offset": "+01:00",
     "cities": ["Warsaw", "Krakow", "Gdansk"]},
    {"code": "UA", "currency": "UAH", "offset": "+02:00",
     "cities": ["Kyiv", "Lviv"]},
    {"code": "KZ", "currency": "KZT", "offset": "+05:00",
     "cities": ["Almaty", "Astana"]},
]

# Диапазоны цен (rent, sale) в валюте страны и шаг округления.
PRICE_RANGES: dict[str, tuple[tuple[int, int], tuple[int, int], int]] = {
    "BYN": ((400, 1500), (45_000, 150_000), 10),
    "RUB": ((25_000, 120_000), (3_500_000, 25_000_000), 1000),
    "PLN": ((1800, 4500), (350_000, 900_000), 50),
    "UAH": ((8_000, 30_000), (1_500_000, 6_000_000), 500),
    "KZT": ((150_000, 500_000), (15_000_000, 60_000_000), 50_000),
}

STREETS = [
    "Притыцкого", "Независимости", "Советская", "Пушкина", "Ленина",
    "Гагарина", "Космонавтов", "Мира", "Зелёная", "Садовая",
]

# Локальное написание города для address (city в данных — каноническое латинское).
CITY_LOCAL = {
    "Minsk": "Минск", "Brest": "Брест", "Grodno": "Гродно",
    "Moscow": "Москва", "Saint Petersburg": "Санкт-Петербург", "Kazan": "Казань",
    "Warsaw": "Варшава", "Krakow": "Краков", "Gdansk": "Гданьск",
    "Kyiv": "Киев", "Lviv": "Львов",
    "Almaty": "Алматы", "Astana": "Астана",
}

SELLERS = ["Иван", "Мария", "Алексей", "Ольга", "Дмитрий", "Анна", "Сергей", "Елена"]

DESCRIPTION_PARTS = [
    "Светлая квартира после ремонта",
    "Развитая инфраструктура рядом",
    "Тихий район, зелёный двор",
    "Удобная транспортная развязка",
    "Балкон с видом на парк",
    "Новая сантехника и мебель",
    "Рядом школа и детский сад",
    "Свободна сразу",
]

# Обёртки описания: иногда с HTML-фрагментами — для проверки очистки у адаптера.
HTML_WRAPS = ["<b>{}</b>", "<i>{}</i>", "<span style=\"color:red\">{}</span>", "{}"]


@dataclass(frozen=True)
class Place:
    country: str
    city: str
    currency: str
    offset: str


def _place(rng: random.Random) -> Place:
    c = rng.choice(COUNTRIES)
    return Place(c["code"], rng.choice(c["cities"]), c["currency"], c["offset"])


def _make_item(rng: random.Random, seq: int, now: datetime, prefix: str = "seed",
               fresh: bool = False) -> dict:
    place = _place(rng)
    deal_type = rng.choice(["rent", "sale"])

    rooms = rng.choices([0, 1, 2, 3, 4], weights=[1, 3, 3, 2, 1])[0]
    area = round(rng.uniform(18, 120), 1)
    floor = rng.randint(1, 9)
    floors = rng.randint(floor, 12)

    rent, sale, step = PRICE_RANGES[place.currency]
    low, high = rent if deal_type == "rent" else sale
    price = rng.randrange(low, high, step)

    parts = rng.sample(DESCRIPTION_PARTS, rng.randint(2, 4))
    wrap = rng.choice(HTML_WRAPS)
    description = wrap.format(", ".join(parts))

    # fresh — «только что опубликовано» (stream-режим); иначе разброс за 90 дней.
    published = now if fresh else now - timedelta(seconds=rng.uniform(0, 90 * 24 * 3600))

    return {
        "id": f"{prefix}-{seq:06d}",
        "deal_type": deal_type,
        "title": (
            f"Студия, {area} м², {floor}/{floors} эт."
            if rooms == 0
            else f"{rooms}-к квартира, {area} м², {floor}/{floors} эт."
        ),
        "price": price,
        "currency": place.currency,
        "country": place.country,
        "city": place.city,
        "address": f"{CITY_LOCAL[place.city]}, ул. {rng.choice(STREETS)}, {rng.randint(1, 99)}",
        "rooms": rooms,
        "area": area,
        "floor": floor,
        "description": description,
        "images": [
            f"http://mock-avito:8002/img/{prefix}-{seq:06d}_{k}.jpg"
            for k in range(1, rng.randint(1, 3) + 1)
        ],
        "published_at": published.isoformat(timespec="seconds").replace("+00:00", place.offset),
        "url": f"http://mock-avito:8002/item/{prefix}-{seq:06d}",
        "seller": {"name": rng.choice(SELLERS), "rating": round(rng.uniform(3.5, 5.0), 1)},
    }


def generate_batch(count: int, seed: int) -> list[dict]:
    """Полный набор с постоянными ID (MOCK_PUBLISH_MODE=batch)."""
    rng = random.Random(seed)
    now = datetime.now(timezone.utc)
    return [_make_item(rng, i, now) for i in range(1, count + 1)]


def generate_stream_item(seq: int, rng: random.Random) -> dict:
    """Новое объявление с уникальным ID (MOCK_PUBLISH_MODE=stream).

    published_at — текущий момент: объявление «только что опубликовано».
    """
    return _make_item(rng, seq, datetime.now(timezone.utc), prefix="gen", fresh=True)
