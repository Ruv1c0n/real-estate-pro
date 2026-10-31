from __future__ import annotations

from abc import ABC, abstractmethod
from typing import Any


class BlockedError(RuntimeError):
    """Источник заблокировал нас (403/429/капча). Сигнал к переключению на мок."""


class SourceAdapter(ABC):
    name: str

    @abstractmethod
    def fetch(self) -> list[dict[str, Any]]:
        """Вернуть «сырые» объявления (формат см. data/listings.json)."""
