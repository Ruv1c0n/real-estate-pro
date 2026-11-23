"""Runner: подсчёты цикла, дедуп, fallback источника, 409, stream
(ФТ-1.2/1.4/1.5/1.8)."""

import asyncio

import pytest
from conftest import FakeRedis, make_raw, wait_run_done

from app.publisher import DeliveryResult
from app.runner import RunInProgressError


class StubPublisher:
    """Publisher-стаб: возвращает заготовленные результаты по очереди."""

    def __init__(self, *results: tuple[DeliveryResult, str | None]) -> None:
        self.queue = list(results)
        self.sent: list[dict] = []

    async def send(self, payload: dict):
        self.sent.append(payload)
        if len(self.queue) > 1:
            return self.queue.pop(0)
        return self.queue[0]


@pytest.mark.anyio
async def test_cycle_counts_and_marks_dedup(make_settings, make_runner):
    settings = make_settings()
    dump = [
        make_raw("seed-1"),
        make_raw("seed-2"),
        make_raw("bad", price=0),  # некорректная запись
    ]
    publisher = StubPublisher((DeliveryResult.CREATED, None))
    redis = FakeRedis()
    runner, core = make_runner(settings, publisher, dump_records=dump, redis=redis)

    run_id = await runner.start_cycle()
    run = await wait_run_done(runner, run_id)
    await core.aclose()

    assert run.status == "completed"
    assert run.source_used == "mock"
    assert run.created == 2
    assert run.skipped_invalid == 1
    assert run.reasons and "price" in run.reasons[0]
    # дедуп помечен только доставленными записями (ФТ-1.5)
    assert len(redis.storage) == 2


@pytest.mark.anyio
async def test_second_cycle_skips_dedup(make_settings, make_runner):
    settings = make_settings()
    publisher = StubPublisher((DeliveryResult.CREATED, None))
    redis = FakeRedis()
    runner, core = make_runner(
        settings, publisher, dump_records=[make_raw("seed-1")], redis=redis
    )

    first = await wait_run_done(runner, await runner.start_cycle())
    second = await wait_run_done(runner, await runner.start_cycle())
    await core.aclose()

    assert first.created == 1
    assert second.created == 0
    assert second.skipped_dedup == 1
    assert len(publisher.sent) == 1  # дубль повторно не отправлялся


@pytest.mark.anyio
async def test_fallback_to_mock_without_credentials(make_settings, make_runner):
    # MOCK_MODE=false без SOURCE_API_URL/TOKEN — подмена на mock (ФТ-1.2)
    settings = make_settings(MOCK_MODE="false")
    publisher = StubPublisher((DeliveryResult.CREATED, None))
    runner, core = make_runner(settings, publisher, dump_records=[make_raw("seed-1")])

    assert runner.build_adapter().code == "mock"
    run = await wait_run_done(runner, await runner.start_cycle())
    await core.aclose()

    assert run.source_used == "mock"
    assert run.created == 1


@pytest.mark.anyio
async def test_exhausted_delivery_not_marked(make_settings, make_runner):
    settings = make_settings()
    publisher = StubPublisher((DeliveryResult.EXHAUSTED, "HTTP 500 после 6 попыток"))
    redis = FakeRedis()
    runner, core = make_runner(
        settings, publisher, dump_records=[make_raw("seed-1")], redis=redis
    )

    run = await wait_run_done(runner, await runner.start_cycle())
    await core.aclose()

    assert run.failed_delivery == 1
    assert redis.storage == {}  # непомечена — повторный запуск обработает снова


@pytest.mark.anyio
async def test_second_start_while_active_conflicts(make_settings, make_runner):
    settings = make_settings()
    release = asyncio.Event()

    class SlowPublisher:
        async def send(self, payload: dict):
            await release.wait()
            return (DeliveryResult.CREATED, None)

    runner, core = make_runner(
        settings,
        SlowPublisher(),
        dump_records=[make_raw("seed-1"), make_raw("seed-2")],
    )

    run_id = await runner.start_cycle()
    with pytest.raises(RunInProgressError):
        await runner.start_cycle()  # активный запуск — 409 у API (ФТ-1.4)
    release.set()
    run = await wait_run_done(runner, run_id)
    await core.aclose()

    assert run.created == 2


@pytest.mark.anyio
async def test_stream_loop_delivers_one_record(make_settings, make_runner):
    settings = make_settings()
    stop = asyncio.Event()

    class OneShotPublisher:
        def __init__(self) -> None:
            self.sent: list[dict] = []

        async def send(self, payload: dict):
            self.sent.append(payload)
            stop.set()
            return (DeliveryResult.CREATED, None)

    publisher = OneShotPublisher()
    runner, core = make_runner(settings, publisher)  # дамп stream'у не нужен

    await runner.stream_loop(stop)
    await core.aclose()

    assert len(publisher.sent) == 1
    assert runner.stream_stats.sent == 1
