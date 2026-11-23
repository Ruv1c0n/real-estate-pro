"""Справочники Core API (ФТ-2.4): страны, города с алиасами, валюты, источники.

Форматы — contracts.md §3:
    GET /api/v1/countries         → [{code, name}]
    GET /api/v1/cities?country=BY → [{country, name, aliases}]
    GET /api/v1/sources           → [{code, name}]
    GET /api/v1/currencies        → [{code, rate_to_usd}]

На момент реализации ФТ-2.4 в core-api ещё не смержен: 404 и недоступность
трактуются как «справочников нет» — нормализация работает в промежуточном
режиме (см. app/normalize.py). При появлении справочников этот код менять
не нужно: те же эндпоинты и те же форматы.
"""

import logging
import time
from decimal import Decimal

import httpx

logger = logging.getLogger(__name__)

REFRESH_TTL_SECONDS = 300.0


class DictsSnapshot:
    """Неизменяемый снимок справочников."""

    def __init__(
        self,
        countries: list[str],
        cities: list[dict],
        currencies: dict[str, Decimal],
        sources: list[str],
    ) -> None:
        self.countries = frozenset(countries)
        self.sources = frozenset(sources)
        self.currencies = dict(currencies)
        # country → {написание в нижнем регистре → каноническое имя}
        self._cities: dict[str, dict[str, str]] = {}
        for entry in cities:
            country = str(entry.get("country", "")).upper()
            name = str(entry.get("name", ""))
            index = self._cities.setdefault(country, {})
            if name:
                index[name.casefold()] = name
            for alias in entry.get("aliases") or []:
                if alias:
                    index[str(alias).casefold()] = name

    def canonical_city(self, country: str, raw_city: str) -> str | None:
        """Каноническое латинское имя города внутри страны или None."""
        return self._cities.get(country, {}).get(raw_city.strip().casefold())

    @property
    def available(self) -> bool:
        return bool(self.countries and self.currencies and self.sources)


class DictsService:
    """Загружает и кэширует справочники; недоступность не фатальна."""

    def __init__(self, client: httpx.AsyncClient, base_url: str) -> None:
        self._client = client
        self._base_url = base_url.rstrip("/")
        self._snapshot: DictsSnapshot | None = None
        self._fetched_at = 0.0

    @property
    def snapshot(self) -> DictsSnapshot | None:
        return self._snapshot

    async def refresh(self, force: bool = False) -> DictsSnapshot | None:
        """Свежий снимок или последний удачный; None — справочников нет."""
        if (
            not force
            and self._snapshot is not None
            and time.monotonic() - self._fetched_at < REFRESH_TTL_SECONDS
        ):
            return self._snapshot
        snapshot = await self._fetch()
        if snapshot is not None:
            self._snapshot = snapshot
            self._fetched_at = time.monotonic()
        return self._snapshot

    async def _fetch(self) -> DictsSnapshot | None:
        try:
            countries = await self._fetch_codes("/api/v1/countries")
            sources = await self._fetch_codes("/api/v1/sources")
            currencies = await self._fetch_currencies()
            cities = await self._fetch_cities(countries)
        except (httpx.HTTPError, ValueError, KeyError) as exc:
            logger.warning("Справочники Core API недоступны: %s", exc)
            return None
        return DictsSnapshot(countries, cities, currencies, sources)

    async def _fetch_codes(self, path: str) -> list[str]:
        response = await self._client.get(f"{self._base_url}{path}")
        response.raise_for_status()
        items = response.json()
        if not isinstance(items, list):
            raise ValueError(f"{path}: ожидался массив")
        return [str(item["code"]) for item in items]

    async def _fetch_currencies(self) -> dict[str, Decimal]:
        response = await self._client.get(f"{self._base_url}/api/v1/currencies")
        response.raise_for_status()
        items = response.json()
        if not isinstance(items, list):
            raise ValueError("/api/v1/currencies: ожидался массив")
        rates: dict[str, Decimal] = {}
        for item in items:
            rates[str(item["code"])] = Decimal(str(item["rate_to_usd"]))
        return rates

    async def _fetch_cities(self, countries: list[str]) -> list[dict]:
        cities: list[dict] = []
        for country in countries:
            response = await self._client.get(
                f"{self._base_url}/api/v1/cities", params={"country": country}
            )
            response.raise_for_status()
            items = response.json()
            if not isinstance(items, list):
                raise ValueError(f"/api/v1/cities?country={country}: ожидался массив")
            cities.extend(items)
        return cities
