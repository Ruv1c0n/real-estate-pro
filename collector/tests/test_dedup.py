"""Redis-дедуп (ФТ-1.5, НФТ-7): пометка после доставки, деградация без Redis."""

import pytest
from conftest import FakeRedis

from app.dedup import RedisDedup


@pytest.mark.anyio
async def test_mark_and_processed():
    dedup = RedisDedup(FakeRedis())
    assert await dedup.processed("mock", "seed-1") is False
    await dedup.mark_processed("mock", "seed-1")
    assert await dedup.processed("mock", "seed-1") is True


@pytest.mark.anyio
async def test_redis_down_degrades_to_send():
    dedup = RedisDedup(FakeRedis(fail=True))
    # проверка отвечает «не видели» — отправка продолжается (НФТ-7)
    assert await dedup.processed("mock", "seed-1") is False
    # пометка молча пропускается, без падения
    await dedup.mark_processed("mock", "seed-1")


@pytest.mark.anyio
async def test_key_scoped_by_source():
    dedup = RedisDedup(FakeRedis())
    await dedup.mark_processed("mock", "seed-1")
    assert await dedup.processed("avito", "seed-1") is False
