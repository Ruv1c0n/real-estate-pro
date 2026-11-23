"""Оркестрация сбора: запуски с реестром run-ов, пакетное расписание и
stream-генерация (ФТ-1.4, ФТ-1.8).

Параллельные циклы одного источника не допускаются (ФТ-1.4): активный
запуск блокирует start_cycle (409 у POST /collect/run), тик расписания
в этом случае пропускается.
"""

import asyncio
import logging
import random
import uuid
from datetime import UTC, datetime
from typing import Literal

import httpx
from pydantic import BaseModel, Field

from app.adapters.avito import AvitoApiAdapter
from app.adapters.base import AdapterError, ListingAdapter, SourceConfigError
from app.adapters.mock import MockAdapter
from app.config import Settings
from app.dedup import RedisDedup
from app.dicts import DictsService, DictsSnapshot
from app.normalize import NormalizeError, normalize
from app.publisher import CoreApiPublisher, DeliveryResult
from app.schemas import RawListing

logger = logging.getLogger(__name__)

_KEEP_RUNS = 20
_MAX_REASONS = 100

Outcome = Literal["created", "duplicate", "skipped_invalid", "skipped_dedup", "failed"]


class RunInProgressError(RuntimeError):
    """Активный запуск уже идёт — POST /collect/run отвечает 409 (ФТ-1.4)."""


class RunInfo(BaseModel):
    """Статус запуска; выдаётся GET /collect/runs/{run_id} (ФТ-1.4)."""

    run_id: str
    trigger: str  # manual | schedule
    status: Literal["running", "completed", "failed"] = "running"
    source_used: str = ""
    started_at: str = ""
    finished_at: str | None = None
    fetched: int = 0
    created: int = 0
    duplicates: int = 0
    skipped_invalid: int = 0
    skipped_dedup: int = 0
    failed_delivery: int = 0
    reasons: list[str] = Field(default_factory=list)
    error: str | None = None

    def add_reason(self, reason: str) -> None:
        if len(self.reasons) < _MAX_REASONS:
            self.reasons.append(reason)


class StreamStats(BaseModel):
    """Счётчики stream-генератора (ФТ-1.8) между импортами дампа."""

    sent: int = 0
    skipped: int = 0
    failed: int = 0
    last_sent_at: str | None = None


def _now_iso() -> str:
    return datetime.now(UTC).isoformat(timespec="seconds").replace("+00:00", "Z")


class CollectRunner:
    def __init__(
        self,
        config: Settings,
        client: httpx.AsyncClient,
        dicts: DictsService,
        dedup: RedisDedup,
        publisher: CoreApiPublisher,
        mock_adapter: MockAdapter,
        rng: random.Random | None = None,
    ) -> None:
        self._config = config
        self._client = client
        self._dicts = dicts
        self._dedup = dedup
        self._publisher = publisher
        self._mock = mock_adapter
        self._rng = rng or random.Random()
        self._active = False
        self._runs: dict[str, RunInfo] = {}
        self._tasks: set[asyncio.Task] = set()
        self.stream_stats = StreamStats()

    def run_info(self, run_id: str) -> RunInfo | None:
        return self._runs.get(run_id)

    def build_adapter(self) -> ListingAdapter:
        """Источник по конфигурации; без реквизитов — mock (ФТ-1.2)."""
        if self._config.mock_mode:
            return self._mock
        try:
            return AvitoApiAdapter(
                base_url=self._config.source_api_url or "",
                token=self._config.source_api_token or "",
                client=self._client,
            )
        except SourceConfigError as exc:
            logger.warning(
                "Реквизиты источника не заданы (%s) — подмена на mock (ФТ-1.2)", exc
            )
            return self._mock

    async def start_cycle(self, trigger: str = "manual") -> str:
        """Ставит пакетный цикл в фон и возвращает run_id (ФТ-1.4)."""
        if self._active:
            raise RunInProgressError("Сбор уже выполняется")
        run = RunInfo(run_id=uuid.uuid4().hex, trigger=trigger, started_at=_now_iso())
        self._runs[run.run_id] = run
        while len(self._runs) > _KEEP_RUNS:
            self._runs.pop(next(iter(self._runs)))
        self._active = True
        try:
            task = asyncio.create_task(self._run_cycle(run))
        except Exception:
            self._active = False
            raise
        self._tasks.add(task)
        task.add_done_callback(self._tasks.discard)
        return run.run_id

    async def _run_cycle(self, run: RunInfo) -> None:
        try:
            await self._collect(run)
        except Exception as exc:  # неожиданная ошибка не должна молчать
            run.status = "failed"
            run.error = f"внутренняя ошибка: {exc}"
            logger.exception("Цикл %s упал", run.run_id)
        finally:
            run.finished_at = _now_iso()
            self._active = False

    async def _collect(self, run: RunInfo) -> None:
        adapter, raws = await self._fetch_with_fallback(run)
        if run.status == "failed":
            return
        run.source_used = adapter.code
        run.fetched = len(raws)
        snapshot = await self._dicts.refresh()
        if snapshot is None:
            logger.warning(
                "Справочники недоступны — промежуточный режим нормализации: "
                "USD-записи проходят, не-USD пропускаются (ФТ-2.4 в работе)"
            )
        for raw in raws:
            outcome, reason = await self._deliver(raw, snapshot)
            if outcome == "created":
                run.created += 1
            elif outcome == "duplicate":
                run.duplicates += 1
            elif outcome == "skipped_dedup":
                run.skipped_dedup += 1
            elif outcome == "failed":
                run.failed_delivery += 1
                run.add_reason(f"{raw.external_id}: {reason}")
            else:  # skipped_invalid
                run.skipped_invalid += 1
                run.add_reason(f"{raw.external_id}: {reason}")
        run.status = "completed"
        logger.info(
            "Цикл %s завершён: источник=%s, получено=%d, создано=%d, дублей=%d, "
            "некорректные=%d, дедуп=%d, не доставлено=%d",
            run.run_id,
            adapter.code,
            run.fetched,
            run.created,
            run.duplicates,
            run.skipped_invalid,
            run.skipped_dedup,
            run.failed_delivery,
        )

    async def _fetch_with_fallback(
        self, run: RunInfo
    ) -> tuple[ListingAdapter, list[RawListing]]:
        adapter = self.build_adapter()
        try:
            return adapter, await adapter.fetch()
        except AdapterError as exc:
            logger.warning(
                "Источник «%s» недоступен (%s) — подмена на mock (ФТ-1.2)",
                adapter.code,
                exc,
            )
        try:
            return self._mock, await self._mock.fetch()
        except AdapterError as exc:
            run.status = "failed"
            run.error = f"источник и mock недоступны: {exc}"
            return self._mock, []

    async def _deliver(
        self, raw: RawListing, snapshot: DictsSnapshot | None
    ) -> tuple[Outcome, str | None]:
        try:
            payload = normalize(raw, snapshot)
        except NormalizeError as exc:
            logger.info("Пропуск %s: %s", raw.external_id, exc)
            return "skipped_invalid", str(exc)
        if await self._dedup.processed(payload.source, payload.external_id):
            logger.debug(
                "Дедуп: %s/%s уже обработано", payload.source, payload.external_id
            )
            return "skipped_dedup", None
        result, message = await self._publisher.send(payload.to_dict())
        if result in (DeliveryResult.CREATED, DeliveryResult.DUPLICATE):
            # Хэш помечается обработанным только после 200/201 (ФТ-1.5).
            await self._dedup.mark_processed(payload.source, payload.external_id)
        if result is DeliveryResult.CREATED:
            return "created", None
        if result is DeliveryResult.DUPLICATE:
            return "duplicate", None
        return "failed", message

    async def schedule_loop(self, stop: asyncio.Event) -> None:
        """Пакетный сбор по расписанию (ФТ-1.4; интервал — конфигурация)."""
        logger.info(
            "Расписание пакетного сбора: каждые %d с",
            self._config.collect_interval_seconds,
        )
        while not stop.is_set():
            try:
                await self.start_cycle("schedule")
            except RunInProgressError:
                logger.info("Тик расписания пропущен: предыдущий цикл ещё выполняется")
            except Exception:
                logger.exception("Цикл расписания завершился ошибкой")
            try:
                await asyncio.wait_for(
                    stop.wait(), timeout=self._config.collect_interval_seconds
                )
                break  # стоп установлен — выходим
            except TimeoutError:
                pass  # интервал прошёл — следующий тик

    async def stream_loop(self, stop: asyncio.Event) -> None:
        """Stream-режим mock (ФТ-1.8): одна новая запись раз в 5–30 с."""
        low = self._config.mock_stream_min_seconds
        high = self._config.mock_stream_max_seconds
        logger.info("Stream-генератор: новая запись раз в %d–%d с", low, high)
        while not stop.is_set():
            try:
                raw = await self._mock.fetch_stream_one()
                snapshot = await self._dicts.refresh()
                outcome, reason = await self._deliver(raw, snapshot)
                if outcome in ("created", "duplicate"):
                    self.stream_stats.sent += 1
                    self.stream_stats.last_sent_at = _now_iso()
                elif outcome == "skipped_invalid":
                    self.stream_stats.skipped += 1
                    logger.info("Stream-пропуск %s: %s", raw.external_id, reason)
                else:
                    self.stream_stats.failed += 1
                    logger.warning("Stream-недоставка %s: %s", raw.external_id, reason)
            except asyncio.CancelledError:
                raise
            except Exception:
                logger.exception("Stream-итерация завершилась ошибкой")
            interval = self._rng.uniform(low, high)
            try:
                await asyncio.wait_for(stop.wait(), timeout=interval)
                break  # стоп установлен — выходим
            except TimeoutError:
                pass  # интервал прошёл — следующая запись
