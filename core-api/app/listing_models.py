from datetime import UTC, datetime
from decimal import Decimal
from typing import Literal

from pydantic import (
    AnyHttpUrl,
    AwareDatetime,
    BaseModel,
    Field,
    field_validator,
)


class ListingCreate(BaseModel):
    source: str = Field(pattern=r"\S")
    external_id: str = Field(pattern=r"\S")
    url: AnyHttpUrl

    deal_type: Literal["rent", "sale"]
    country: str = Field(
        min_length=2,
        max_length=2,
        pattern=r"^[A-Z]{2}$",
    )
    city: str = Field(pattern=r"\S")

    price: Decimal = Field(
        gt=0,
        max_digits=18,
        decimal_places=2,
        allow_inf_nan=False,
    )
    currency: str = Field(
        min_length=3,
        max_length=3,
        pattern=r"^[A-Z]{3}$",
    )
    price_usd: int = Field(
        strict=True,
        gt=0,
        le=9223372036854775807,
    )
    rooms: int = Field(strict=True, ge=0, le=2147483647)

    title: str | None = None
    address: str | None = None
    description: str | None = None
    area_m2: Decimal | None = Field(
        default=None,
        gt=0,
        max_digits=8,
        decimal_places=2,
        allow_inf_nan=False,
    )
    floor: int | None = Field(
        default=None,
        strict=True,
        ge=-2147483648,
        le=2147483647,
    )
    photos: list[AnyHttpUrl] | None = None
    published_at: AwareDatetime | None = None

    @field_validator("published_at")
    @classmethod
    def normalize_published_at(
        cls,
        value: datetime | None,
    ) -> datetime | None:
        if value is None:
            return None
        return value.astimezone(UTC)
