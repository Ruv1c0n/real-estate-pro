"""Доставка объявлений в Core API (ФТ-1.6, contracts.md §1).

Начальная попытка и до N повторов при сетевой ошибке, 429 и 5xx —
экспоненциальная задержка 1–30 с с учётом Retry-After. Остальные 4xx
не повторяются (ошибка полей не исправится повтором). После исчерпания
попыток запись остаётся непомеченной в дедупе — повторный запуск сбора
обработает её снова; внешний ID при повторе не меняется.
"""

import asyncio
import logging
from collections.abc import Awaitable, Callable
from enum import Enum

import httpx

logger = logging.getLogger(__name__)

_RETRY_CAP_SECONDS = 30.0


class DeliveryResult(Enum):
    CREATED = "created"  # 201 — сохранено, outbox-задание создано
    DUPLICATE = "duplicate"  # 200 — дубль по (source, external_id)
    VALIDATION_ERROR = "validation_error"  # прочий 4xx — не повторяется
    EXHAUSTED = "exhausted"  # попытки кончились; доступна для повторного запуска


class CoreApiPublisher:
    def __init__(
        self,
        client: httpx.AsyncClient,
        base_url: str,
        max_retries: int = 5,
        sleep: Callable[[float], Awaitable[None]] = asyncio.sleep,
    ) -> None:
        self._client = client
        self._base_url = base_url.rstrip("/")
        self._max_retries = max_retries
        self._sleep = sleep

    async def send(self, payload: dict) -> tuple[DeliveryResult, str | None]:
        """Отправляет объявление; возвращает (результат, причина ошибки)."""
        for attempt in range(self._max_retries + 1):
            try:
                response = await self._client.post(
                    f"{self._base_url}/api/v1/listings", json=payload
                )
            except httpx.RequestError as exc:
                if attempt == self._max_retries:
                    return DeliveryResult.EXHAUSTED, f"сеть: {exc}"
                await self._sleep(self._backoff(attempt))
                continue

            if response.status_code in (200, 201):
                result = (
                    DeliveryResult.CREATED
                    if response.status_code == 201
                    else DeliveryResult.DUPLICATE
                )
                return result, None

            if response.status_code == 429 or response.status_code >= 500:
                if attempt == self._max_retries:
                    attempts = self._max_retries + 1
                    return (
                        DeliveryResult.EXHAUSTED,
                        f"HTTP {response.status_code} после {attempts} попыток",
                    )
                await self._sleep(self._delay_for(attempt, response))
                continue

            # Прочие 4xx: повтор бессмыслен, хэш не помечается.
            return DeliveryResult.VALIDATION_ERROR, _error_message(response)
        return DeliveryResult.EXHAUSTED, "попытки исчерпаны"  # недостижимо

    def _backoff(self, attempt: int) -> float:
        """1, 2, 4, 8… с с потолком 30 с (ФТ-1.6: задержки 1–30 с)."""
        return min(_RETRY_CAP_SECONDS, 2.0**attempt)

    def _delay_for(self, attempt: int, response: httpx.Response) -> float:
        delay = self._backoff(attempt)
        retry_after = response.headers.get("Retry-After")
        if retry_after is not None:
            try:
                seconds = float(retry_after)
            except ValueError:
                return delay
            # Учитываем Retry-After, но не дольше общего потолка 30 с.
            delay = min(_RETRY_CAP_SECONDS, max(delay, seconds))
        return delay


def _error_message(response: httpx.Response) -> str:
    try:
        body = response.json()
        return (
            f"HTTP {response.status_code}: {body.get('error')}: {body.get('message')}"
        )
    except ValueError:
        return f"HTTP {response.status_code}"
