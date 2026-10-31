"""Генератор data/listings.json (~520 «сырых» объявлений из разных стран).
Запуск: python scripts/gen_mock.py
"""
import json
import random
from datetime import datetime, timedelta
from pathlib import Path

random.seed(42)

# country, city, currency, symbol-format, streets, rent range (в местной валюте), sale range
CITIES = [
    ("BY", "Минск", "BYN", "{} BYN", ["пр. Независимости", "ул. Притыцкого", "ул. Козлова", "ул. Якуба Коласа"], (500, 2500), (150_000, 600_000), "ru"),
    ("RU", "Москва", "RUB", "{} ₽", ["ул. Тверская", "Ленинский пр-т", "ул. Арбат", "Кутузовский пр-т"], (45_000, 220_000), (9_000_000, 45_000_000), "ru"),
    ("RU", "Санкт-Петербург", "RUB", "{} ₽", ["Невский пр-т", "ул. Рубинштейна", "Московский пр-т"], (35_000, 150_000), (6_000_000, 28_000_000), "ru"),
    ("KZ", "Алматы", "KZT", "{} ₸", ["пр. Аль-Фараби", "ул. Достык", "ул. Абая"], (150_000, 700_000), (30_000_000, 140_000_000), "ru"),
    ("DE", "Berlin", "EUR", "{} €", ["Kastanienallee", "Oranienstraße", "Karl-Marx-Allee"], (700, 2500), (200_000, 800_000), "en"),
    ("PL", "Warsaw", "PLN", "{} zł", ["ul. Marszałkowska", "ul. Puławska", "al. Jerozolimskie"], (2500, 7000), (500_000, 1_800_000), "en"),
    ("GE", "Tbilisi", "GEL", "{} ₾", ["Rustaveli Ave", "Chavchavadze Ave", "Aghmashenebeli Ave"], (900, 3500), (120_000, 500_000), "en"),
    ("TR", "Istanbul", "TRY", "{} ₺", ["İstiklal Cd.", "Bağdat Cd.", "Barbaros Blv."], (20_000, 90_000), (3_000_000, 15_000_000), "en"),
    ("AE", "Dubai", "AED", "{} AED", ["Sheikh Zayed Rd", "Al Wasl Rd", "Marina Walk"], (5_000, 18_000), (600_000, 3_500_000), "en"),
]

ROOMS_RU = {0: "Студия", 1: "1-комн.", 2: "2-комн.", 3: "3-комн.", 4: "4-комн."}
SOURCES = ["avito", "cian", "yandex"]
FOREIGN_SOURCES = ["immowelt", "otodom", "mylivn"]


def fmt_price(n: int, pattern: str) -> str:
    return pattern.format(f"{n:,}".replace(",", " "))


def main() -> None:
    items = []
    now = datetime(2026, 10, 1)
    for i in range(1, 521):
        country, city, cur, pat, streets, rent_r, sale_r, lang = random.choice(CITIES)
        deal = random.choices(["rent", "sale"], weights=[6, 4])[0]
        rooms = random.choices([0, 1, 2, 3, 4], weights=[1, 4, 4, 2, 1])[0]
        area = round({0: 26, 1: 38, 2: 56, 3: 78, 4: 105}[rooms] * random.uniform(0.8, 1.3), 1)
        lo, hi = rent_r if deal == "rent" else sale_r
        k = 0.6 + rooms * 0.15
        price = int(random.uniform(lo, hi) * k / 5) * 5
        price = max(price, lo)
        total_floors = random.randint(5, 25)
        floor = random.randint(1, total_floors)
        src = random.choice(SOURCES if lang == "ru" else FOREIGN_SOURCES + SOURCES[:1])
        street = f"{random.choice(streets)}, {random.randint(1, 150)}"
        ext = f"{src}-{i:05d}"

        # намеренно «грязные» форматы: проверяем нормализацию
        if lang == "ru":
            room_raw = ROOMS_RU[rooms] if i % 3 else rooms
            area_raw = f"{str(area).replace('.', ',')} м²"
            deal_raw = "аренда" if deal == "rent" else "продажа"
            title = f"{ROOMS_RU[rooms]} квартира, {area} м², {city}"
            desc = f"<p>Светлая квартира в центре.</p><br><b>{street}</b> &mdash; рядом метро.&nbsp;Без комиссии."
        else:
            room_raw = "studio" if rooms == 0 else f"{rooms} rooms"
            area_raw = f"{area} m2"
            deal_raw = deal
            title = f"{'Studio' if rooms == 0 else str(rooms) + '-room'} apartment, {area} m², {city}"
            desc = f"<div>Bright apartment near the centre.</div><p>{street}. Quiet building, <i>great transport links</i>.</p>"

        items.append({
            "source": src,
            "id": ext,
            "link": f"https://example.com/{src}/flat/{i:05d}?utm_source=mock",
            "title": title,
            "deal": deal_raw,
            "price": fmt_price(price, pat) if i % 2 else price,
            "currency": cur if i % 2 == 0 else None,
            "country": country,
            "city": city,
            "address": street,
            "rooms": room_raw,
            "area": area_raw,
            "floor": f"{floor}/{total_floors}",
            "description": desc,
            "photos": [],
            "published_at": (now - timedelta(hours=random.randint(0, 24 * 30))).isoformat(),
        })

    # несколько нарочно сломанных записей, чтобы показать отсев невалидных
    items.append({"source": "avito", "id": "broken-1", "link": "https://example.com/x", "title": "?", "deal": "аренда",
                  "price": "договорная", "country": "RU", "city": "Москва", "rooms": 2})
    items.append({"source": "cian", "id": "broken-2", "link": "https://example.com/y", "title": "?", "deal": "обмен",
                  "price": 100, "currency": "USD", "country": "RU", "city": "Москва", "rooms": 2})

    out = Path(__file__).resolve().parent.parent / "data" / "listings.json"
    out.write_text(json.dumps(items, ensure_ascii=False, indent=1), encoding="utf-8")
    print(f"записано {len(items)} записей в {out}")


if __name__ == "__main__":
    main()
