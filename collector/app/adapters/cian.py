"""Каркас живого адаптера. Селекторы нужно подогнать под актуальную вёрстку.

Главное для зачёта: при блокировке бросаем BlockedError, а main.py
бесшовно подменяет источник на MockAdapter.
"""
from __future__ import annotations

import logging
from typing import Any

import httpx
from bs4 import BeautifulSoup

from .base import BlockedError, SourceAdapter

log = logging.getLogger(__name__)

SEARCH_URL = "https://www.cian.ru/cat.php?deal_type=rent&engine_version=2&offer_type=flat&region=1"
HEADERS = {"User-Agent": "Mozilla/5.0 (compatible; course-project-bot/0.1)"}


class CianAdapter(SourceAdapter):
    name = "cian"

    def __init__(self, timeout: float = 10.0):
        self.timeout = timeout

    def fetch(self) -> list[dict[str, Any]]:
        try:
            r = httpx.get(SEARCH_URL, headers=HEADERS, timeout=self.timeout, follow_redirects=True)
        except httpx.HTTPError as e:
            raise BlockedError(f"cian недоступен: {e}") from e

        if r.status_code in (403, 429) or "captcha" in r.text.lower():
            raise BlockedError(f"cian заблокировал запрос (HTTP {r.status_code})")
        r.raise_for_status()

        soup = BeautifulSoup(r.text, "lxml")
        items: list[dict[str, Any]] = []
        for card in soup.select("article"):  # TODO: подогнать селектор под актуальную вёрстку
            link = card.select_one("a[href*='/rent/flat/']")
            if not link:
                continue
            # TODO: достать title/price/rooms/area/address из карточки и
            # собрать dict в формате data/listings.json
            log.debug("найдена карточка %s", link.get("href"))
        return items
