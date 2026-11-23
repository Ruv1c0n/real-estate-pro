"""Общие фикстуры и fake-объекты для тестов collector'а (без сети и БД)."""

import asyncio
import json
import random
import time
from pathlib import Path

import httpx
import pytest

from app.adapters.mock import MockAdapter
from app.config import Settings
from app.dedup import RedisDedup
from app.dicts import DictsService
from app.main import app as collector_app
from app.main import wire_state
from app.runner import CollectRunner

CORE_API = "http://core-api.test"


class FakeRedis:
    """Мини-стаб redis.asyncio.Redis: достаточно exists/set/ping."""

    def __init__(self, fail: bool = False) -> None:
        self.storage: dict[str, str] = {}
        self.fail = fail

    async def exists(self, key: str) -> int:
        if self.fail:
            raise ConnectionError("redis down")
        return 1 if key in self.storage else 0

    async def set(self, key: str, value: str) -> None:
        if self.fail:
            raise ConnectionError("redis down")
        self.storage[key] = value

    async def ping(self) -> bool:
        if self.fail:
            raise ConnectionError("redis down")
        return True


def make_raw(external_id: str = "seed-000001", **overrides) -> dict:
    """Корректная запись RawListing для дампов и тестов."""
    record = {
        "source": "mock",
        "external_id": external_id,
        "deal_type": "rent",
        "country": "BY",
        "city": "Minsk",
        "rooms": 2,
        "area_m2": 45.5,
        "price": 450,
        "currency": "USD",
        "url": f"http://mock.example/listing/{external_id}",
        "title": "2-к квартира, 45.5 м²",
        "address": "ул. Притыцкого, 10",
        "published_at": "2026-11-20T10:00:00+03:00",
    }
    record.update(overrides)
    return record


def write_dump(path: Path, records: list[dict]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(records, ensure_ascii=False), encoding="utf-8")


@pytest.fixture
def anyio_backend() -> str:
    return "asyncio"


@pytest.fixture
def fake_redis() -> FakeRedis:
    return FakeRedis()


@pytest.fixture
def make_settings(tmp_path: Path):
    def factory(**overrides) -> Settings:
        values = {
            "MOCK_MODE": "true",
            "ADMIN_KEY": "",
            "MOCK_DUMP_PATH": str(tmp_path / "dump.json"),
            "COLLECT_INTERVAL_SECONDS": "3600",
            "CORE_API_URL": CORE_API,
            "REDIS_URL": "redis://localhost:6379/0",
        }
        values.update(overrides)
        return Settings(**values)

    return factory


@pytest.fixture
async def wired_app():
    """Фабрика HTTP-клиента поверх collector-app с fake-зависимостями."""
    clients: list[httpx.AsyncClient] = []

    def factory(settings: Settings, handler, redis: FakeRedis | None = None):
        core = httpx.AsyncClient(transport=httpx.MockTransport(handler))
        wire_state(collector_app, settings, core, redis or FakeRedis())
        asgi = httpx.AsyncClient(
            transport=httpx.ASGITransport(app=collector_app),
            base_url="http://collector.test",
        )
        clients.extend([core, asgi])
        return asgi

    yield factory

    for client in clients:
        await client.aclose()


@pytest.fixture
def make_runner():
    """Фабрика CollectRunner с подставляемым publisher'ом и дампом."""

    def factory(
        settings: Settings,
        publisher,
        *,
        core_handler=None,
        dedup: RedisDedup | None = None,
        redis: FakeRedis | None = None,
        dump_records: list[dict] | None = None,
        rng_seed: int = 1,
    ) -> tuple[CollectRunner, httpx.AsyncClient]:
        if dump_records is not None:
            write_dump(Path(settings.mock_dump_path), dump_records)
        if core_handler is None:
            core_handler = _created_handler
        core = httpx.AsyncClient(transport=httpx.MockTransport(core_handler))
        runner = CollectRunner(
            settings,
            core,
            DictsService(core, settings.core_api_url),
            dedup or RedisDedup(redis or FakeRedis()),
            publisher,
            MockAdapter(settings.mock_dump_path, rng=random.Random(rng_seed)),
            rng=random.Random(rng_seed + 1),
        )
        return runner, core

    return factory


async def wait_run_done(runner: CollectRunner, run_id: str, timeout: float = 3.0):
    """Ждёт завершения фонового цикла, чтобы тест не висел вечно."""
    deadline = time.monotonic() + timeout
    run = runner.run_info(run_id)
    while run.status == "running" and time.monotonic() < deadline:
        await asyncio.sleep(0.01)
    return run


def _created_handler(request: httpx.Request) -> httpx.Response:
    return httpx.Response(201, json={"id": 1, "created": True})
