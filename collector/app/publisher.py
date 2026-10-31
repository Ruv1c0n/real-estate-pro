from __future__ import annotations

import logging
import time

import httpx

from .schemas import ListingCreate

log = logging.getLogger(__name__)


class CoreApiPublisher:
    def __init__(self, base_url: str, timeout: float = 10.0):
        self.url = f"{base_url.rstrip('/')}/api/v1/listings"
        self.client = httpx.Client(timeout=timeout)

    def wait_until_ready(self, attempts: int = 30, delay: float = 2.0) -> None:
        """Core API может стартовать позже воркера, ждём, а не падаем."""
        health = self.url.rsplit("/api/v1/", 1)[0] + "/docs"
        for i in range(1, attempts + 1):
            try:
                self.client.get(health)
                log.info("core-api доступен")
                return
            except httpx.HTTPError:
                log.info("ждём core-api (%d/%d)...", i, attempts)
                time.sleep(delay)
        raise RuntimeError("core-api так и не поднялся")

    def send(self, listing: ListingCreate) -> str:
        """Возвращает 'created' | 'duplicate' | 'error'."""
        payload = listing.model_dump(mode="json")
        for attempt in range(3):
            try:
                r = self.client.post(self.url, json=payload)
            except httpx.HTTPError as e:
                log.warning("POST не удался (%s), попытка %d/3", e, attempt + 1)
                time.sleep(2 ** attempt)
                continue
            if r.status_code in (200, 201):
                return "created"
            if r.status_code == 409:
                return "duplicate"
            log.error("core-api ответил %s: %s", r.status_code, r.text[:300])
            return "error"
        return "error"

    def close(self) -> None:
        self.client.close()
