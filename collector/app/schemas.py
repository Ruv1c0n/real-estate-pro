"""Схемы Collector: RawListing (contracts.md §3) и payload доставки (§1)."""

from decimal import Decimal
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict


class RawListing(BaseModel):
    """Внутренний формат исходных полей до нормализации (ФТ-1.1).

    Типы намеренно широкие: адаптер получает данные внешнего API как есть,
    разбор и валидацию выполняет normalize (неполные записи отбрасываются
    с причиной, ФТ-1.3). Неизвестные поля источника игнорируются.
    """

    model_config = ConfigDict(extra="ignore")

    source: str | None = None
    external_id: str
    deal_type: str | None = None
    country: str | None = None
    city: str | None = None
    rooms: int | str | None = None
    area_m2: Decimal | float | int | str | None = None
    price: Decimal | float | int | str | None = None
    currency: str | None = None
    url: str | None = None
    title: str | None = None
    address: str | None = None
    description: str | None = None
    floor: int | str | None = None
    photos: list[str] | None = None
    published_at: str | None = None


class ListingPayload(BaseModel):
    """Тело POST /api/v1/listings (contracts.md §1); собирает normalize."""

    source: str
    external_id: str
    deal_type: Literal["rent", "sale"]
    country: str
    city: str
    rooms: int
    price: Decimal
    currency: str
    price_usd: int
    url: str
    title: str | None = None
    address: str | None = None
    description: str | None = None
    area_m2: Decimal | None = None
    floor: int | None = None
    photos: list[str] | None = None
    published_at: str | None = None

    def to_dict(self) -> dict[str, Any]:
        """JSON-тело: Decimal передаётся числом (contracts.md: JSON — числа)."""
        data = self.model_dump()
        data["price"] = float(self.price)
        if data["area_m2"] is not None:
            data["area_m2"] = float(data["area_m2"])
        return data
