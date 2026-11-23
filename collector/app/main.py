"""Точка входа Collector: сборка приложения, фоновые циклы (расписание или
stream-генерация), health, админ-эндпоинты и UI. Фоновые задачи переживают
разовые сбои (НФТ-8), повторы доставки — в app.publisher (НФТ-9)."""

import asyncio
import logging
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from pathlib import Path

import httpx
import redis.asyncio as redis
from fastapi import FastAPI
from fastapi.staticfiles import StaticFiles

from app.adapters.mock import MockAdapter
from app.admin import router as admin_router
from app.config import Settings, settings
from app.dedup import RedisDedup
from app.dicts import DictsService
from app.error_handlers import register_error_handlers
from app.health import router as health_router
from app.logging_setup import setup_logging
from app.publisher import CoreApiPublisher
from app.runner import CollectRunner
from app.ui import router as ui_router

logger = logging.getLogger(__name__)


def wire_state(
    application: FastAPI,
    config: Settings,
    http: httpx.AsyncClient,
    redis_client,
) -> None:
    """Создаёт общие компоненты на app.state. Вынесено отдельно, чтобы
    тесты могли подставить fake-клиенты перед сборкой runner."""
    dicts = DictsService(http, config.core_api_url)
    application.state.settings = config
    application.state.http = http
    application.state.redis = redis_client
    application.state.dicts = dicts
    application.state.runner = CollectRunner(
        config,
        http,
        dicts,
        RedisDedup(redis_client),
        CoreApiPublisher(http, config.core_api_url, config.core_api_max_retries),
        MockAdapter(config.mock_dump_path),
    )


def _start_background(
    runner: CollectRunner, config: Settings, stop: asyncio.Event
) -> asyncio.Task:
    if config.mock_publish_mode == "stream":
        # ФТ-1.8: планировщик пакетного сбора отключён; ручной импорт дампа
        # через POST /collect/run остаётся доступным.
        return asyncio.create_task(runner.stream_loop(stop))
    return asyncio.create_task(runner.schedule_loop(stop))


@asynccontextmanager
async def lifespan(application: FastAPI) -> AsyncIterator[None]:
    setup_logging(settings.log_level)
    if not settings.admin_key:
        logger.warning(
            "ADMIN_KEY пуст — /collect/run и статусы доступны без ключа (dev-режим, НФТ-3)"
        )
    if settings.mock_mode:
        logger.info("MOCK_MODE=true — данные из локального mock-источника")
    else:
        logger.info("MOCK_MODE=false — источник %s", settings.source_api_url)

    async with httpx.AsyncClient(timeout=settings.core_api_timeout_seconds) as http:
        redis_client = redis.from_url(settings.redis_url)
        try:
            wire_state(application, settings, http, redis_client)
            # Прогрев справочников; недоступность — не фатальна.
            await application.state.dicts.refresh()
            stop = asyncio.Event()
            task = _start_background(application.state.runner, settings, stop)
            logger.info("Collector запущен")
            try:
                yield
            finally:
                stop.set()
                await task
                logger.info("Collector остановлен")
        finally:
            await redis_client.aclose()


app = FastAPI(
    title=settings.app_name,
    version=settings.app_version,
    lifespan=lifespan,
)
register_error_handlers(app)
app.include_router(health_router)
app.include_router(admin_router)
app.include_router(ui_router)
app.mount(
    "/static",
    StaticFiles(directory=str(Path(__file__).resolve().parent / "static")),
    name="static",
)
