from dataclasses import dataclass
from datetime import datetime
from typing import Protocol

from app.listing_filters import ListingFilters
from app.listing_models import ListingCreate


class StoredListing(ListingCreate):
    """Сохранённое объявление с полями, назначенными Core API."""

    id: int
    created_at: datetime


@dataclass(frozen=True)
class CreateListingResult:
    listing: StoredListing
    created: bool


@dataclass(frozen=True)
class ListingSearchResult:
    items: list[StoredListing]
    total: int


class ListingStore(Protocol):
    async def create_or_get(
        self,
        listing: ListingCreate,
    ) -> CreateListingResult:
        """Создать объявление или вернуть существующее.

        Уникальность определяется парой (source, external_id).

        Для нового объявления сохранение его данных, фотографий
        и события listing_new в outbox должно быть атомарным.

        Для дубля возвращается исходная запись с created=False.
        Данные и фотографии не обновляются, событие не создаётся.

        Одновременные вызовы с одинаковым ключом должны создавать
        только одно объявление и одно событие listing_new.
        """
        ...

    async def get(self, listing_id: int) -> StoredListing | None:
        """Вернуть объявление по ID или None, если оно отсутствует."""
        ...

    async def search(self, filters: ListingFilters) -> ListingSearchResult:
        """Отфильтровать объявления и вернуть запрошенную страницу.

        total считается после фильтрации, до пагинации.
        При равных значениях поля сортировки порядок определяется
        по id в том же направлении.
        """
        ...
