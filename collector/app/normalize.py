"""Приведение «сырых» объявлений из разных источников к ListingCreate."""
from __future__ import annotations

import re
from datetime import datetime
from decimal import ROUND_HALF_UP, Decimal
from typing import Any

from .config import Settings
from .schemas import DealType, ListingCreate

CURRENCY_SYMBOLS = {
    "$": "USD", "€": "EUR", "₽": "RUB", "руб": "RUB", "р.": "RUB",
    "₸": "KZT", "zł": "PLN", "₾": "GEL", "₺": "TRY", "br": "BYN", "byn": "BYN",
}
DEAL_WORDS = {
    "rent": DealType.rent, "аренда": DealType.rent, "снять": DealType.rent, "сдам": DealType.rent,
    "sale": DealType.sale, "buy": DealType.sale, "продажа": DealType.sale, "купить": DealType.sale,
}


class NormalizeError(ValueError):
    pass


def parse_number(value: Any) -> float:
    if isinstance(value, (int, float, Decimal)):
        return float(value)
    s = str(value).replace("\xa0", " ").replace(" ", "")
    s = s.replace(",", ".")
    m = re.search(r"\d+(?:\.\d+)?", s)
    if not m:
        raise NormalizeError(f"не число: {value!r}")
    return float(m.group())


def parse_currency(raw_currency: Any, raw_price: Any = None) -> str:
    for src in (raw_currency, raw_price):
        if not src:
            continue
        s = str(src).strip().lower()
        if re.fullmatch(r"[a-z]{3}", s):
            return s.upper()
        for sym, code in CURRENCY_SYMBOLS.items():
            if sym in s:
                return code
    raise NormalizeError(f"не удалось определить валюту: {raw_currency!r}/{raw_price!r}")


def parse_rooms(value: Any) -> int:
    if isinstance(value, int):
        return value
    s = str(value).lower()
    if "студ" in s or "studio" in s:
        return 0
    m = re.search(r"\d+", s)
    if not m:
        raise NormalizeError(f"не комнаты: {value!r}")
    return int(m.group())


def parse_floor(value: Any) -> tuple[int | None, int | None]:
    if value in (None, ""):
        return None, None
    nums = re.findall(r"\d+", str(value))
    if not nums:
        return None, None
    return int(nums[0]), int(nums[1]) if len(nums) > 1 else None


def parse_deal(value: Any) -> DealType:
    key = str(value).strip().lower()
    if key in DEAL_WORDS:
        return DEAL_WORDS[key]
    raise NormalizeError(f"неизвестный тип сделки: {value!r}")


def to_usd(amount: float, currency: str, rates: dict[str, float]) -> Decimal:
    if currency not in rates:
        raise NormalizeError(f"нет курса для {currency}")
    return (Decimal(str(amount)) * Decimal(str(rates[currency]))).quantize(
        Decimal("0.01"), rounding=ROUND_HALF_UP
    )


def normalize(raw: dict[str, Any], settings: Settings) -> ListingCreate:
    """raw → ListingCreate. Бросает NormalizeError, если запись непригодна."""
    try:
        price = parse_number(raw["price"])
        currency = parse_currency(raw.get("currency"), raw.get("price"))
        floor, floors_total = parse_floor(raw.get("floor"))
        area = parse_number(raw["area"]) if raw.get("area") else None
        published = raw.get("published_at")
        return ListingCreate(
            source=str(raw["source"]),
            external_id=str(raw["id"]),
            url=str(raw["link"]),
            deal_type=parse_deal(raw["deal"]),
            price=Decimal(str(price)),
            currency=currency,
            price_usd=to_usd(price, currency, settings.fx_rates),
            country=str(raw["country"]),
            city=str(raw["city"]),
            address=raw.get("address"),
            rooms=parse_rooms(raw["rooms"]),
            area_m2=area,
            floor=floor,
            floors_total=floors_total,
            title=str(raw["title"]),
            description=raw.get("description") or "",
            photos=list(raw.get("photos") or []),
            published_at=datetime.fromisoformat(published) if published else None,
        )
    except NormalizeError:
        raise
    except (KeyError, ValueError, TypeError) as e:
        raise NormalizeError(f"{type(e).__name__}: {e}") from e
