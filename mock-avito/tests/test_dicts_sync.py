"""Интеграционная сверка набора mock-avito со справочником Core API.

Требование ТЗ 3.7: Collector валидирует города/страны по справочникам
Core API — набор mock-avito обязан в него вписываться, иначе часть
synthetic-набора получит 422. Пока справочники в core-api не реализованы
(ФТ-2.4, 404) или core-api недоступен, тест скипается — это норма, а не
ошибка; тест оживёт автоматически, как только Илья добавит миграции
справочников.

Запуск: CORE_API_URL=http://localhost:8000 (дефолт) + поднятый core-api.
"""

from __future__ import annotations

import os

import app.generator as generator_mod
import httpx
import pytest

CORE_API_URL = os.getenv("CORE_API_URL", "http://localhost:8000")


def _core_dicts() -> tuple[set[str], dict[str, set[str]]]:
    try:
        r = httpx.get(f"{CORE_API_URL}/api/v1/countries", timeout=3)
    except httpx.HTTPError:
        pytest.skip("core-api недоступен — сверка со справочником отложена")
    if r.status_code == 404:
        pytest.skip("справочники ещё не реализованы в core-api (ФТ-2.4)")
    r.raise_for_status()
    countries = {c["code"] for c in r.json()}
    cities: dict[str, set[str]] = {}
    for code in sorted(countries):
        rc = httpx.get(
            f"{CORE_API_URL}/api/v1/cities", params={"country": code}, timeout=3
        )
        rc.raise_for_status()
        cities[code] = {c["name"] for c in rc.json()}
    return countries, cities


def test_mock_countries_in_core():
    countries, _ = _core_dicts()
    mock = {c["code"] for c in generator_mod.COUNTRIES}
    extra = mock - countries
    assert not extra, (
        f"страны {sorted(extra)} есть в mock-avito, но нет в справочнике "
        f"core-api — Collector получит 422 на часть набора"
    )


def test_mock_cities_in_core():
    _, cities = _core_dicts()
    for c in generator_mod.COUNTRIES:
        core_names = cities.get(c["code"], set())
        assert core_names, f"справочник не содержит городов страны {c['code']}"
        missing = set(c["cities"]) - core_names
        assert not missing, (
            f"города {sorted(missing)} страны {c['code']} есть в mock-avito, "
            f"но нет в справочнике core-api — Collector получит 422"
        )
