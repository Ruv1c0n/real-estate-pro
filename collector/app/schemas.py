from __future__ import annotations

import hashlib
import html
import re
from datetime import datetime
from decimal import Decimal
from enum import Enum

from pydantic import BaseModel, Field, field_validator, model_validator

TAG_RE = re.compile(r"<[^>]+>")
WS_RE = re.compile(r"\s+")


class DealType(str, Enum):
    rent = "rent"
    sale = "sale"


class ListingCreate(BaseModel):
    """Нормализованное объявление. Это тело POST /api/v1/listings."""

    # источник
    source: str = Field(description="avito | cian | yandex | mock | ...")
    external_id: str
    url: str

    # сделка и цена
    deal_type: DealType
    price: Decimal = Field(gt=0, description="цена в исходной валюте")
    currency: str = Field(min_length=3, max_length=3, description="ISO 4217, исходная")
    price_usd: Decimal = Field(gt=0, description="базовая валюта для фильтров")

    # локация
    country: str = Field(min_length=2, max_length=2, description="ISO 3166-1 alpha-2")
    city: str
    address: str | None = None

    # параметры квартиры
    rooms: int = Field(ge=0, le=20, description="0 = студия")
    area_m2: float | None = Field(default=None, gt=0)
    floor: int | None = None
    floors_total: int | None = None

    # контент
    title: str
    description: str = ""
    photos: list[str] = []
    published_at: datetime | None = None

    # дедупликация (считаются автоматически)
    url_hash: str = ""
    content_hash: str = ""

    @field_validator("title", "description", "address", "city", mode="before")
    @classmethod
    def clean_text(cls, v):
        if v is None:
            return v
        v = html.unescape(TAG_RE.sub(" ", str(v)))
        return WS_RE.sub(" ", v).strip()

    @field_validator("currency", "country", mode="before")
    @classmethod
    def upper(cls, v):
        return str(v).strip().upper()

    @model_validator(mode="after")
    def compute_hashes(self):
        self.url_hash = hashlib.sha256(self.url.split("?")[0].encode()).hexdigest()
        raw = (
            f"{self.country}|{self.city}|{self.address}|{self.rooms}|"
            f"{self.area_m2}|{self.price_usd}|{self.deal_type.value}"
        )
        self.content_hash = hashlib.sha256(raw.lower().encode()).hexdigest()
        return self
