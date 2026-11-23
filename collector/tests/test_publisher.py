"""Доставка (ФТ-1.6): повторы на 5xx/429/сети, Retry-After, 4xx без повтора."""

import httpx
import pytest

from app.publisher import CoreApiPublisher, DeliveryResult


def make_publisher(handler, delays: list[float] | None = None, max_retries: int = 5):
    async def instant_sleep(delay: float) -> None:
        if delays is not None:
            delays.append(delay)

    client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
    publisher = CoreApiPublisher(
        client, "http://core-api.test", max_retries=max_retries, sleep=instant_sleep
    )
    return publisher, client


@pytest.mark.anyio
async def test_retries_on_500_then_created():
    calls = []

    def handler(request: httpx.Request) -> httpx.Response:
        calls.append(1)
        if len(calls) < 3:
            return httpx.Response(
                500, json={"error": "internal_error", "message": "boom"}
            )
        return httpx.Response(201, json={"id": 1, "created": True})

    delays: list[float] = []
    publisher, client = make_publisher(handler, delays)
    result, message = await publisher.send({"external_id": "x"})
    await client.aclose()

    assert result is DeliveryResult.CREATED
    assert message is None
    assert len(calls) == 3
    assert delays == [1.0, 2.0]  # экспоненциальная задержка 1–30 с


@pytest.mark.anyio
async def test_422_not_retried():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            422,
            json={"error": "validation_error", "message": "rooms должен быть целым"},
        )

    publisher, client = make_publisher(handler)
    result, message = await publisher.send({})
    await client.aclose()

    assert result is DeliveryResult.VALIDATION_ERROR
    assert "validation_error" in message


@pytest.mark.anyio
async def test_duplicate_200():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"id": 5, "created": False})

    publisher, client = make_publisher(handler)
    result, _ = await publisher.send({})
    await client.aclose()

    assert result is DeliveryResult.DUPLICATE


@pytest.mark.anyio
async def test_retry_after_honored():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            429, headers={"Retry-After": "7"}, json={"error": "too_many_requests"}
        )

    delays: list[float] = []
    publisher, client = make_publisher(handler, delays, max_retries=1)
    result, _ = await publisher.send({})
    await client.aclose()

    assert result is DeliveryResult.EXHAUSTED
    assert delays == [7.0]  # Retry-After учтён вместо базовой задержки 1 с


@pytest.mark.anyio
async def test_exhausted_after_all_attempts():
    calls = []

    def handler(request: httpx.Request) -> httpx.Response:
        calls.append(1)
        return httpx.Response(500, json={"error": "internal_error"})

    delays: list[float] = []
    publisher, client = make_publisher(handler, delays, max_retries=5)
    result, message = await publisher.send({})
    await client.aclose()

    assert result is DeliveryResult.EXHAUSTED
    assert len(calls) == 6  # начальная + 5 повторов (ФТ-1.6)
    assert delays == [1.0, 2.0, 4.0, 8.0, 16.0]
    assert "500" in message


@pytest.mark.anyio
async def test_network_error_retried():
    calls = []

    def handler(request: httpx.Request) -> httpx.Response:
        calls.append(1)
        if len(calls) == 1:
            raise httpx.ConnectError("connection refused")
        return httpx.Response(201, json={"id": 1, "created": True})

    delays: list[float] = []
    publisher, client = make_publisher(handler, delays)
    result, _ = await publisher.send({})
    await client.aclose()

    assert result is DeliveryResult.CREATED
    assert len(calls) == 2
    assert delays == [1.0]
