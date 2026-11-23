"""Общий интерфейс адаптеров источников (ФТ-1.1): fetch() → list[RawListing].

Адаптер скрывает пагинацию источника и особенности его API; наружу —
только RawListing. Схема ответа внешнего API фиксируется в docstring
конкретного адаптера (contracts.md §3: описывается при реализации).
"""

from abc import ABC, abstractmethod

from app.schemas import RawListing


class AdapterError(RuntimeError):
    """Источник недоступен или вернул неожиданные данные."""


class SourceConfigError(AdapterError):
    """Реквизиты источника не заданы (ФТ-1.2) — повод перейти на mock."""


class ListingAdapter(ABC):
    """Адаптер одного источника объявлений."""

    code: str  # код источника (справочник Core API; для mock — "mock")

    @abstractmethod
    async def fetch(self) -> list[RawListing]:
        """Пакет исходных записей; параллельные циклы не допускаются (ФТ-1.4)."""
