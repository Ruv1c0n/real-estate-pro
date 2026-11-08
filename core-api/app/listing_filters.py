from decimal import Decimal
from typing import Literal, Self

from pydantic import BaseModel, Field, model_validator
from pydantic_core import PydanticCustomError


class ListingFilters(BaseModel):
    deal_type: Literal["rent", "sale"] | None = None
    country: str | None = Field(
        default=None,
        min_length=2,
        max_length=2,
        pattern=r"^[A-Z]{2}$",
    )
    city: str | None = Field(default=None, pattern=r"\S")

    price_min: Decimal | None = Field(default=None, ge=0)
    price_max: Decimal | None = Field(default=None, ge=0)

    rooms: int | None = Field(default=None, ge=0)

    area_min: Decimal | None = Field(default=None, ge=0)
    area_max: Decimal | None = Field(default=None, ge=0)

    source: str | None = Field(default=None, pattern=r"\S")

    sort: Literal[
        "price_asc",
        "price_desc",
        "date_asc",
        "date_desc",
    ] = "date_desc"
    page: int = Field(default=1, ge=1)
    size: int = Field(default=20, ge=1, le=100)

    @model_validator(mode="after")
    def validate_ranges(self) -> Self:
        if (
            self.price_min is not None
            and self.price_max is not None
            and self.price_min > self.price_max
        ):
            raise PydanticCustomError(
                "price_range_invalid",
                "Минимальная цена не может превышать максимальную",
            )

        if (
            self.area_min is not None
            and self.area_max is not None
            and self.area_min > self.area_max
        ):
            raise PydanticCustomError(
                "area_range_invalid",
                "Минимальная площадь не может превышать максимальную",
            )

        return self

    @model_validator(mode="after")
    def validate_location(self) -> Self:
        if self.city is not None and self.country is None:
            raise PydanticCustomError(
                "city_country_required",
                "При указании города необходимо указать страну",
            )

        return self
